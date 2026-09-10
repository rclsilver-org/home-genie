package api

import (
	"net/http"
	"strconv"

	"github.com/rclsilver-org/home-notifications/server/internal/store"
)

// handleListMessages returns a channel's stream, newest first. Membership is
// required, so a non-member gets the same 404 as everywhere else.
func (s *Server) handleListMessages(w http.ResponseWriter, r *http.Request) {
	channel, _, ok := s.channelForMember(w, r)
	if !ok {
		return
	}

	query := store.MessageQuery{ChannelID: channel.ID}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil {
			query.Limit = value
		}
	}
	// before_id pages backwards: the client passes the oldest id it holds.
	if raw := r.URL.Query().Get("before_id"); raw != "" {
		if value, err := strconv.ParseInt(raw, 10, 64); err == nil {
			query.BeforeID = value
		}
	}

	messages, err := s.store.MessagesOf(query)
	if err != nil {
		s.logger.Error("listing the messages", "error", err, "channel_id", channel.ID)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	payload := []messagePayload{}
	for _, message := range messages {
		payload = append(payload, toMessagePayload(message, channel.Slug))
	}
	s.writeJSON(w, http.StatusOK, payload)
}
