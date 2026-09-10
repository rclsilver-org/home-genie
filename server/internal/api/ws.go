package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/coder/websocket"

	"github.com/rclsilver-org/home-notifications/server/internal/store"
)

const (
	// heartbeatInterval is how often the server proves the socket is alive.
	//
	// It also has to stay well under the reverse proxy's read timeout: each
	// heartbeat rearms that timer, and spacing them beyond it to save
	// battery would have the proxy cut the socket instead. The two values
	// are coupled and nothing in the proxy configuration says so.
	//
	// It is an application-level frame, not a protocol ping, because the app
	// has to *see* it: a socket the system has silently wedged stays open
	// as far as the OS is concerned, and only a missing heartbeat reveals
	// it. The diagnostic screen shows the last one received.
	heartbeatInterval = 30 * time.Second

	// writeTimeout bounds a single frame. A phone on a bad 4G link must not
	// hold a goroutine forever.
	writeTimeout = 10 * time.Second

	// replayPageSize bounds one catch-up page.
	replayPageSize = 500
)

// frame is what travels over the socket. Persisted events carry a Seq;
// transport frames such as the heartbeat do not.
type frame struct {
	Kind    string          `json:"kind"`
	Seq     int64           `json:"seq,omitempty"`
	At      time.Time       `json:"at"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

const (
	// frameReady is sent once the replay is done, so the client knows it is
	// caught up and can stop showing a catching-up state.
	frameReady = "ready"
	// frameHeartbeat proves liveness to the application, not just to the OS.
	frameHeartbeat = "heartbeat"
)

// handleWS serves the live feed.
//
// The contract that makes an aggressive power manager survivable: on
// connect the server replays
// everything after since_seq, then switches to live. A socket the system
// kills therefore costs a reconnection and a delay, never a lost event.
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	device, _ := DeviceFrom(r.Context())

	sinceSeq := int64(0)
	if raw := r.URL.Query().Get("since_seq"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 0 {
			s.writeError(w, http.StatusBadRequest, "since_seq must be a positive integer")
			return
		}
		sinceSeq = parsed
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// The clients are native applications, not browsers: there is no
		// origin to check, and no browser to protect from cross-site use.
		InsecureSkipVerify: true,
	})
	if err != nil {
		s.logger.Warn("websocket handshake failed", "error", err, "device_id", device.ID)
		return
	}
	defer conn.CloseNow()

	// Subscribe before replaying, so an event landing during the catch-up is
	// queued rather than lost in the gap between the two.
	subscription := s.hub.Subscribe(user.ID, device.ID)

	if err := s.store.MarkDeviceConnected(device.ID); err != nil {
		s.logger.Warn("marking the device connected", "error", err, "device_id", device.ID)
	}

	// One defer, in this order, and not two: a client reconnects before the
	// server notices the old socket died, so the dying one would otherwise
	// clear the marker its replacement had just set. The device is offline
	// only once the hub holds no socket for it.
	defer func() {
		subscription.Close()
		if s.hub.DeviceSubscribers(device.ID) > 0 {
			s.logger.Debug("socket replaced, device still connected", "device_id", device.ID)
			return
		}
		if err := s.store.MarkDeviceDisconnected(device.ID); err != nil {
			s.logger.Warn("marking the device disconnected", "error", err, "device_id", device.ID)
		}
	}()

	s.logger.Info("socket opened", "user", user.Username, "device_id", device.ID,
		"since_seq", sinceSeq)
	defer s.logger.Info("socket closed", "user", user.Username, "device_id", device.ID)

	ctx := r.Context()

	lastSeq, err := s.replay(ctx, conn, user.ID, device.ID, sinceSeq)
	if err != nil {
		s.logger.Warn("replay failed", "error", err, "device_id", device.ID)
		return
	}

	if err := writeFrame(ctx, conn, frame{Kind: frameReady, Seq: lastSeq, At: time.Now().UTC()}); err != nil {
		return
	}

	// Reading surfaces a client-side close, drives the pings the library
	// answers on our behalf, and carries the client's acknowledgements.
	go func() {
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				subscription.Close()
				return
			}
			s.handleClientFrame(user.ID, device.ID, data)
		}
	}()

	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case event, ok := <-subscription.Events:
			if !ok {
				// Dropped: either the peer closed, or the socket fell behind.
				// Closing with a normal status tells the client to reconnect
				// and replay rather than treat it as an error.
				conn.Close(websocket.StatusNormalClosure, "reconnect and replay")
				return
			}
			// An event already replayed must not be sent twice.
			if event.Seq <= lastSeq {
				continue
			}
			if err := writeFrame(ctx, conn, frame{
				Kind: event.Kind, Seq: event.Seq, At: event.CreatedAt, Payload: event.Payload,
			}); err != nil {
				return
			}
			s.recordSent(event, user.ID, device.ID)
			lastSeq = event.Seq

		case <-ticker.C:
			if err := writeFrame(ctx, conn, frame{
				Kind: frameHeartbeat, At: time.Now().UTC(),
			}); err != nil {
				return
			}
		}
	}
}

// replay sends everything the client missed, page by page, and returns the
// last Seq it now holds.
func (s *Server) replay(ctx context.Context, conn *websocket.Conn, userID, deviceID, sinceSeq int64) (int64, error) {
	cursor := sinceSeq
	for {
		events, err := s.store.EventsSince(userID, cursor, replayPageSize)
		if err != nil {
			return cursor, err
		}
		for _, event := range events {
			if err := writeFrame(ctx, conn, frame{
				Kind: event.Kind, Seq: event.Seq, At: event.CreatedAt, Payload: event.Payload,
			}); err != nil {
				return cursor, err
			}
			s.recordSent(event, userID, deviceID)
			cursor = event.Seq
		}
		if len(events) < replayPageSize {
			return cursor, nil
		}
	}
}

func writeFrame(ctx context.Context, conn *websocket.Conn, f frame) error {
	encoded, err := json.Marshal(f)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()

	if err := conn.Write(ctx, websocket.MessageText, encoded); err != nil {
		return err
	}
	return nil
}

// publishToChannel records an event for every member of a channel and fans
// it out. Recording first is deliberate: a socket that misses the fanout
// replays it, whereas an event only sent live would be gone for good.
func (s *Server) publishToChannel(channelID int64, kind string, payload any) {
	members, err := s.store.MemberUserIDs(channelID)
	if err != nil {
		s.logger.Error("listing the members", "error", err, "channel_id", channelID)
		return
	}
	s.publishToUsers(members, kind, payload)
}

func (s *Server) publishToUsers(userIDs []int64, kind string, payload any) {
	events, err := s.store.AppendEventTo(userIDs, kind, payload)
	if err != nil {
		s.logger.Error("recording the event", "error", err, "kind", kind)
		// Fan out what was recorded: a partial delivery beats none.
	}
	s.hub.PublishAll(events)
}

// clientFrame is the only thing clients send.
type clientFrame struct {
	Kind string `json:"kind"`
	Seq  int64  `json:"seq"`
}

const (
	frameAck  = "ack"
	framePong = "pong"
)

// handleClientFrame interprets an acknowledgement. It is what turns a
// "sent" into a "delivered" and therefore what makes a miss detectable: a
// message written to a socket that the device never confirms is exactly the
// silent failure this whole design is built around.
func (s *Server) handleClientFrame(userID, deviceID int64, data []byte) {
	// Anything coming from the client proves the socket is alive in the
	// direction that matters. The server's own heartbeat proves nothing:
	// writing into a wedged socket succeeds for a long time.
	if err := s.store.TouchDevice(deviceID); err != nil {
		s.logger.Warn("updating the device", "error", err, "device_id", deviceID)
	}

	var incoming clientFrame
	if err := json.Unmarshal(data, &incoming); err != nil {
		s.logger.Warn("unreadable client frame", "device_id", deviceID)
		return
	}
	// A pong carries no delivery, only liveness — already recorded above.
	if incoming.Kind != frameAck || incoming.Seq <= 0 {
		return
	}

	count, err := s.store.RecordDelivered(userID, deviceID, incoming.Seq)
	if err != nil {
		s.logger.Warn("recording the acknowledgement", "error", err, "device_id", deviceID)
		return
	}
	if count > 0 {
		s.logger.Debug("delivery acknowledged", "device_id", deviceID,
			"seq", incoming.Seq, "messages", count)
	}
}

// recordSent notes that a message reached a device's socket. Only message
// events carry a message; the others have nothing to record against.
func (s *Server) recordSent(event store.Event, userID, deviceID int64) {
	messageID := messageIDOf(event)
	if messageID == 0 {
		return
	}
	if err := s.store.RecordMessageEvent(messageID, userID, &deviceID, store.MessageSent); err != nil {
		s.logger.Warn("recording the send", "error", err, "message_id", messageID)
	}
}

// messageIDOf digs the message out of an event payload. Reading it back
// from the payload rather than carrying it alongside means replayed events
// are recorded exactly like live ones.
func messageIDOf(event store.Event) int64 {
	if event.Kind != eventMessageNew {
		return 0
	}
	var envelope struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(event.Payload, &envelope); err != nil {
		return 0
	}
	return envelope.ID
}
