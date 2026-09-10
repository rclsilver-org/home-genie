package api

import (
	"net/http"
	"strconv"

	"github.com/rclsilver-org/home-notifications/server/internal/store"
)

// handleListMessages returns a channel's stream, newest first. Membership is
// required, so a non-member gets the same 404 as everywhere else.
func (s *Server) handleListMessages(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())

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

	ids := make([]int64, 0, len(messages))
	for _, message := range messages {
		ids = append(ids, message.ID)
	}
	// One query for the whole batch rather than one per message.
	read, err := s.store.ReadMessageIDs(user.ID, ids)
	if err != nil {
		s.logger.Error("reading the read state", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	payload := []messagePayload{}
	for _, message := range messages {
		entry := toMessagePayload(message, channel.Slug)
		entry.Read = read[message.ID]
		payload = append(payload, entry)
	}
	s.writeJSON(w, http.StatusOK, payload)
}
