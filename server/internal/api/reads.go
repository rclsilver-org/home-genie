package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/rclsilver-org/home-genie/server/internal/store"
)

// Event kinds telling a user's other devices that read state moved, so
// reading on the phone clears the badge on the tablet — and only there.
const (
	eventMessagesRead = "messages.read"
)

type timelineEntry struct {
	Kind     string    `json:"kind"`
	Username string    `json:"username"`
	Device   string    `json:"device,omitempty"`
	At       time.Time `json:"at"`
}

// handleMarkRead marks one message read for the caller.
func (s *Server) handleMarkRead(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	device, _ := DeviceFrom(r.Context())

	message, ok := s.messageForMember(w, r)
	if !ok {
		return
	}

	deviceID := device.ID
	changed, err := s.store.MarkRead(message.ID, user.ID, &deviceID)
	if err != nil {
		s.logger.Error("marking as read", "error", err, "message_id", message.ID)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if changed {
		s.publishToUsers([]int64{user.ID}, eventMessagesRead, map[string]any{
			"channel_id":  message.ChannelID,
			"message_ids": []int64{message.ID},
		})
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleMarkChannelRead marks a channel read up to a message, or entirely.
func (s *Server) handleMarkChannelRead(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	device, _ := DeviceFrom(r.Context())

	channel, _, ok := s.channelForMember(w, r)
	if !ok {
		return
	}

	uptoID := int64(0)
	if raw := r.URL.Query().Get("upto_id"); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value < 0 {
			s.writeError(w, http.StatusBadRequest, "upto_id must be a positive integer")
			return
		}
		uptoID = value
	}

	deviceID := device.ID
	ids, err := s.store.MarkChannelRead(channel.ID, user.ID, uptoID, &deviceID)
	if err != nil {
		s.logger.Error("marking the channel read", "error", err, "channel_id", channel.ID)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if len(ids) > 0 {
		s.publishToUsers([]int64{user.ID}, eventMessagesRead, map[string]any{
			"channel_id":  channel.ID,
			"message_ids": ids,
		})
	}

	s.writeJSON(w, http.StatusOK, map[string]any{"marked": len(ids)})
}

// handleTimeline returns a message's distribution history: who it went to,
// on which device, and when it was sent, delivered and read.
func (s *Server) handleTimeline(w http.ResponseWriter, r *http.Request) {
	message, ok := s.messageForMember(w, r)
	if !ok {
		return
	}

	events, err := s.store.TimelineOf(message.ID)
	if err != nil {
		s.logger.Error("reading the timeline", "error", err, "message_id", message.ID)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	payload := []timelineEntry{}
	for _, event := range events {
		payload = append(payload, timelineEntry{
			Kind: event.Kind, Username: event.Username,
			Device: event.Device, At: event.At,
		})
	}
	s.writeJSON(w, http.StatusOK, payload)
}

// messageForMember resolves a message and checks the caller belongs to its
// channel. A non-member gets 404, as everywhere: the message's existence is
// not theirs to learn.
func (s *Server) messageForMember(w http.ResponseWriter, r *http.Request) (store.Message, bool) {
	user, _ := UserFrom(r.Context())

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "unknown message")
		return store.Message{}, false
	}

	message, err := s.store.MessageByID(id)
	if errors.Is(err, store.ErrNotFound) {
		s.writeError(w, http.StatusNotFound, "unknown message")
		return store.Message{}, false
	}
	if err != nil {
		s.logger.Error("reading the message", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return store.Message{}, false
	}

	if _, err := s.store.RoleOn(message.ChannelID, user.ID); err != nil {
		s.writeError(w, http.StatusNotFound, "unknown message")
		return store.Message{}, false
	}

	return message, true
}
