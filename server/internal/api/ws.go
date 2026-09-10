package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/coder/websocket"
)

const (
	// heartbeatInterval is how often the server proves the socket is alive.
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

	lastSeq, err := s.replay(ctx, conn, user.ID, sinceSeq)
	if err != nil {
		s.logger.Warn("replay failed", "error", err, "device_id", device.ID)
		return
	}

	if err := writeFrame(ctx, conn, frame{Kind: frameReady, Seq: lastSeq, At: time.Now().UTC()}); err != nil {
		return
	}

	// Reading is what surfaces a client-side close and drives the pings the
	// library answers on our behalf. The clients send nothing, so anything
	// read is discarded.
	go func() {
		for {
			if _, _, err := conn.Read(ctx); err != nil {
				subscription.Close()
				return
			}
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
			lastSeq = event.Seq

		case <-ticker.C:
			if err := writeFrame(ctx, conn, frame{
				Kind: frameHeartbeat, At: time.Now().UTC(),
			}); err != nil {
				return
			}
			if err := s.store.TouchDevice(device.ID); err != nil {
				s.logger.Warn("updating the device", "error", err, "device_id", device.ID)
			}
		}
	}
}

// replay sends everything the client missed, page by page, and returns the
// last Seq it now holds.
func (s *Server) replay(ctx context.Context, conn *websocket.Conn, userID, sinceSeq int64) (int64, error) {
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
