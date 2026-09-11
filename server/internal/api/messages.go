package api

import (
	"net/http"
	"strconv"

	"github.com/rclsilver-org/home-genie/server/internal/store"
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

// handleListFeed returns the caller's notifications across every channel they
// belong to: what the *arr suite and diun publish, newest first.
//
// It is deliberately a different surface from the alert console. These
// objects have their own lifecycle — a message is read or unread, per user,
// and nothing else ever happens to it — where an alert opens, is taken,
// reminds and closes for everybody at once.
func (s *Server) handleListFeed(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())

	params := r.URL.Query()
	query := store.FeedQuery{OnlyUnread: params.Get("unread") == "1"}
	if raw := params.Get("limit"); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil {
			query.Limit = value
		}
	}
	if raw := params.Get("before_id"); raw != "" {
		if value, err := strconv.ParseInt(raw, 10, 64); err == nil {
			query.BeforeID = value
		}
	}

	messages, err := s.store.MessagesForUser(user.ID, query)
	if err != nil {
		s.logger.Error("listing the feed", "error", err, "user_id", user.ID)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	ids := make([]int64, 0, len(messages))
	for _, message := range messages {
		ids = append(ids, message.ID)
	}
	read, err := s.store.ReadMessageIDs(user.ID, ids)
	if err != nil {
		s.logger.Error("reading the read state", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// The slug travels with each message: a feed mixing several channels
	// without saying which is which is unreadable.
	slugs := map[int64]string{}
	payload := []messagePayload{}
	for _, message := range messages {
		slug, known := slugs[message.ChannelID]
		if !known {
			if channel, err := s.store.ChannelByID(message.ChannelID); err == nil {
				slug = channel.Slug
			}
			slugs[message.ChannelID] = slug
		}
		entry := toMessagePayload(message, slug)
		entry.Read = read[message.ID]
		payload = append(payload, entry)
	}
	s.writeJSON(w, http.StatusOK, payload)
}

// handleMarkFeedRead empties the notifications view in one gesture.
func (s *Server) handleMarkFeedRead(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	device, _ := DeviceFrom(r.Context())

	deviceID := device.ID
	ids, err := s.store.MarkFeedRead(user.ID, &deviceID)
	if err != nil {
		s.logger.Error("marking the feed read", "error", err, "user_id", user.ID)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Told to this user's devices only: the read state is personal, and the
	// other members of the channel are still waiting to see these.
	if len(ids) > 0 {
		s.publishToUsers([]int64{user.ID}, eventMessagesRead, map[string]any{
			"channel_id":  0,
			"message_ids": ids,
		})
	}

	s.writeJSON(w, http.StatusOK, map[string]any{"marked": len(ids)})
}

// handleUnreadFeedCount feeds the badge on the notifications tab.
func (s *Server) handleUnreadFeedCount(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())

	count, err := s.store.UnreadFeedCount(user.ID)
	if err != nil {
		s.logger.Error("counting the unread messages", "error", err, "user_id", user.ID)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]int{"count": count})
}
