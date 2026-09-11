package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rclsilver-org/home-genie/server/internal/store"
)

// maxBodySize bounds a published message. Generous for text, small enough
// that a misconfigured producer cannot fill the server's disk in an afternoon.
const maxBodySize = 256 * 1024

// ntfyPublish is ntfy's JSON publish shape. Only the fields that mean
// something here are read; the rest are accepted and ignored so a producer
// configured for ntfy does not get a 400 for sending extras.
type ntfyPublish struct {
	Topic    string          `json:"topic"`
	Title    string          `json:"title"`
	Message  string          `json:"message"`
	Priority any             `json:"priority"`
	Tags     []string        `json:"tags"`
	Click    string          `json:"click"`
	Actions  json.RawMessage `json:"actions"`
}

// handleIngestNtfy accepts a message on POST /{slug}, in ntfy's own format.
//
// This is what lets *arr, diun and anything else already speaking ntfy move
// over by changing a URL and a token, with no other configuration touched.
func (s *Server) handleIngestNtfy(w http.ResponseWriter, r *http.Request) {
	_, channel, ok := PublishTargetFrom(r.Context())
	if !ok {
		s.writeError(w, http.StatusUnauthorized, "invalid token")
		return
	}

	input, err := s.parseNtfyRequest(r, channel)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	message, err := s.store.CreateMessage(input)
	if err != nil {
		s.logger.Error("recording the message", "error", err, "channel", channel.Slug)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	s.logger.Info("message published", "channel", channel.Slug,
		"message_id", message.ID, "priority", message.Priority)

	s.publishMessage(channel, message)

	// The response mimics ntfy's, so a producer that parses it is not
	// surprised. Most only look at the status code.
	s.writeJSON(w, http.StatusOK, map[string]any{
		"id":       strconv.FormatInt(message.ID, 10),
		"time":     message.CreatedAt.Unix(),
		"event":    "message",
		"topic":    channel.Slug,
		"title":    message.Title,
		"message":  message.Body,
		"priority": message.Priority,
		"tags":     message.Tags,
	})
}

func (s *Server) parseNtfyRequest(r *http.Request, channel store.Channel) (store.NewMessage, error) {
	body, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, maxBodySize))
	if err != nil {
		return store.NewMessage{}, fmt.Errorf("body too large or unreadable")
	}

	input := store.NewMessage{
		ChannelID: channel.ID,
		Priority:  store.PriorityDefault,
		Tags:      []string{},
	}

	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		var payload ntfyPublish
		if err := json.Unmarshal(body, &payload); err != nil {
			return store.NewMessage{}, fmt.Errorf("malformed JSON body")
		}
		// ntfy carries the topic in the body when publishing to the root.
		// Here the path already decided it; a mismatch is a misrouted
		// producer and must not silently publish to the wrong channel.
		if payload.Topic != "" && payload.Topic != channel.Slug {
			return store.NewMessage{}, fmt.Errorf(
				"the topic in the body (%q) does not match the URL (%q)", payload.Topic, channel.Slug)
		}
		input.Title = payload.Title
		input.Body = payload.Message
		input.ClickURL = payload.Click
		if payload.Tags != nil {
			input.Tags = payload.Tags
		}
		if payload.Priority != nil {
			input.Priority = parsePriority(fmt.Sprint(payload.Priority))
		}
		if len(payload.Actions) > 0 {
			input.Actions = payload.Actions
		}
	} else {
		input.Body = string(body)
	}

	// Headers win over the JSON body, which is also ntfy's behaviour.
	if title := firstHeader(r, "X-Title", "Title", "t"); title != "" {
		input.Title = title
	}
	if priority := firstHeader(r, "X-Priority", "Priority", "prio", "p"); priority != "" {
		input.Priority = parsePriority(priority)
	}
	if tags := firstHeader(r, "X-Tags", "Tags", "tag", "ta"); tags != "" {
		input.Tags = splitTags(tags)
	}
	if click := firstHeader(r, "X-Click", "Click"); click != "" {
		input.ClickURL = click
	}

	// ntfy's compact Actions header ("view, Label, https://…; …") is not
	// parsed yet. It is ignored rather than half-understood: storing
	// something the app cannot render would be worse than nothing, and no
	// producer we are migrating uses it. Our own alerts set actions
	// natively, in JSON.
	if raw := firstHeader(r, "X-Actions", "Actions", "action"); raw != "" {
		s.logger.Warn("the Actions header is not supported yet, ignored",
			"channel", channel.Slug, "value", raw)
	}

	if input.Title == "" && strings.TrimSpace(input.Body) == "" {
		return store.NewMessage{}, fmt.Errorf("an empty message has nothing to notify")
	}

	return input, nil
}

// firstHeader returns the first of names that is set, so both ntfy's long
// and short header spellings work.
func firstHeader(r *http.Request, names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(r.Header.Get(name)); value != "" {
			return value
		}
	}
	return ""
}

// parsePriority accepts ntfy's numbers and its names.
func parsePriority(raw string) int {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "min":
		return store.PriorityMin
	case "low":
		return store.PriorityLow
	case "default":
		return store.PriorityDefault
	case "high":
		return store.PriorityHigh
	case "max", "urgent":
		return store.PriorityMax
	}
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value < store.PriorityMin || value > store.PriorityMax {
		return store.PriorityDefault
	}
	return value
}

func splitTags(raw string) []string {
	tags := []string{}
	for _, tag := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(tag); trimmed != "" {
			tags = append(tags, trimmed)
		}
	}
	return tags
}

const eventMessageNew = "message.new"

type messagePayload struct {
	ID          int64           `json:"id"`
	ChannelID   int64           `json:"channel_id"`
	ChannelSlug string          `json:"channel_slug"`
	AlertID     *int64          `json:"alert_id,omitempty"`
	Title       string          `json:"title"`
	Body        string          `json:"body"`
	Priority    int             `json:"priority"`
	Tags        []string        `json:"tags"`
	ClickURL    string          `json:"click_url,omitempty"`
	Actions     json.RawMessage `json:"actions,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	// The reminder rank, only on what the devices receive: the stored message
	// does not carry it, and reading it back later would give today's counter,
	// not the one of the day it went out.
	ReminderCount int `json:"reminder_count,omitempty"`
	// Per caller, like the unread counter.
	Read bool `json:"read"`
}

func toMessagePayload(message store.Message, slug string) messagePayload {
	tags := message.Tags
	if tags == nil {
		tags = []string{}
	}
	return messagePayload{
		ID: message.ID, ChannelID: message.ChannelID, ChannelSlug: slug,
		AlertID: message.AlertID, Title: message.Title, Body: message.Body,
		Priority: message.Priority, Tags: tags, ClickURL: message.ClickURL,
		Actions: message.Actions, CreatedAt: message.CreatedAt,
	}
}

// publishMessage records the message as queued for every member, then fans
// it out. Queued is written before the fanout on purpose: a recipient with
// no live socket must still appear in the timeline, since "queued but never
// sent" is exactly the miss the reliability figure is looking for.
func (s *Server) publishMessage(channel store.Channel, message store.Message) {
	payload := toMessagePayload(message, channel.Slug)
	payload.Priority = s.deliveryPriority(channel, message, "", payload.Priority)
	s.publishMessagePayload(channel, message, payload)
}

// deliveryPriority applies the channel's quiet hours to what the phone
// receives.
//
// The priority is lowered on the way out and not in the record: the quiet
// hours change how a message arrives, not what it is, and reading it back
// tomorrow must still show the priority it was published with.
//
// Silenced and not postponed. A notification held until morning would land
// out of order in a feed whose unread state already keeps it for then, and an
// alert held until morning is an alert lost. What the window buys is the
// phone staying quiet; everything still arrives.
//
// A critical is silenced only by a window that names it: the store refuses to
// let a channel-wide window cover one. Silencing a critical is a legitimate
// thing to want on a homelab, but not something to inherit by accident.
func (s *Server) deliveryPriority(
	channel store.Channel, message store.Message, severity string, priority int,
) int {
	quiet, err := s.store.QuietHoursFor(channel.ID, severity)
	if err != nil {
		s.logger.Error("reading the quiet hours", "error", err, "channel_id", channel.ID)
		return priority
	}
	if !quiet.Covers(time.Now()) {
		return priority
	}
	s.logger.Info("delivered silently", "channel", channel.Slug,
		"message_id", message.ID, "severity", severity)
	return store.PriorityMin
}

// publishMessagePayload is the same fanout with a payload the caller owns —
// a reminder carries a rank the stored message does not.
func (s *Server) publishMessagePayload(
	channel store.Channel, message store.Message, payload messagePayload,
) {
	members, err := s.store.MemberUserIDs(channel.ID)
	if err != nil {
		s.logger.Error("listing the members", "error", err, "channel_id", channel.ID)
		return
	}

	for _, userID := range members {
		if err := s.store.RecordMessageEvent(message.ID, userID, nil, store.MessageQueued); err != nil {
			s.logger.Warn("recording the queueing", "error", err, "user_id", userID)
		}
	}

	s.publishToUsers(members, eventMessageNew, payload)
}
