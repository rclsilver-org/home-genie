package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	token, channel, ok := PublishTargetFrom(r.Context())
	if !ok {
		s.writeError(w, http.StatusUnauthorized, "invalid token")
		return
	}

	input, err := s.parseNtfyRequest(r, channel)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Which producer sent it. This is what lets the feed put a face beside a
	// notification: the token is already one per software, it only lacked a
	// way back from the message.
	input.PublishTokenID = &token.ID

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

	// A producer that cannot set headers passes the same fields in the query
	// string, which ntfy accepts too. This is not a marginal path: the *arr
	// suite — the one this endpoint exists for — sends every field this way
	// and no other, so without it the endpoint misses the case it was built
	// for.
	query := r.URL.Query()
	if title := firstValue(query, "title", "t"); title != "" {
		input.Title = title
	}
	if message := firstValue(query, "message", "m"); message != "" {
		input.Body = message
	}
	if priority := firstValue(query, "priority", "prio", "p"); priority != "" {
		input.Priority = parsePriority(priority)
	}
	if tags := firstValue(query, "tags", "tag", "ta"); tags != "" {
		input.Tags = splitTags(tags)
	}
	if click := firstValue(query, "click"); click != "" {
		input.ClickURL = click
	}

	// Headers win over the query string, which wins over the body: the more
	// explicit the carrier, the later it is applied. That is ntfy's order too.
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
	raw := firstHeader(r, "X-Actions", "Actions", "action")
	if raw == "" {
		raw = firstValue(query, "actions", "action")
	}
	if raw != "" {
		s.logger.Warn("the Actions field is not supported yet, ignored",
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

// firstValue returns the first of names present in the query string, so
// both ntfy's long and short spellings work there as well.
func firstValue(values url.Values, names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(values.Get(name)); value != "" {
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
	// ProducerID identifies the publish token that sent it — what a client
	// needs to ask for its icon. Producer names it, and ProducerIcon says
	// whether that token has a picture to fetch — so a client asks for one
	// only where there is one to get.
	ProducerID   *int64 `json:"producer_id,omitempty"`
	Producer     string `json:"producer,omitempty"`
	ProducerIcon bool   `json:"producer_icon,omitempty"`
	// Rank of the reminder, only on what the devices receive: the stored
	// message does not carry it, and reading it back later would give
	// today's count, not the one it was sent with.
	ReminderCount int `json:"reminder_count,omitempty"`
	// Silent asks the device not to notify at all — the mute, as opposed to
	// the quiet hours, which merely lower the priority. The message still
	// arrives and still counts as unread.
	Silent bool `json:"silent,omitempty"`
	// AlertResolved marks the message that closes an alert. The phone needs
	// it to drop the acknowledge action: acknowledging a closed alert is a
	// gesture the server refuses, so offering it is only a way to look broken.
	AlertResolved bool `json:"alert_resolved,omitempty"`
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
		ProducerID: message.ProducerID, Producer: message.Producer,
		ProducerIcon: message.ProducerIcon,
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

// deliveryPriority applies the quiet hours to what the phone receives.
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
// let a broad window cover one. Silencing a critical is a legitimate thing to
// want on a homelab, but not something to inherit by accident.
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

	// A mute belongs to each person: the same message goes out audible for
	// whoever asked for nothing and silent for whoever muted themselves.
	// It is the only thing that differs between recipients, and the reason
	// the payload is computed per user.
	muted, err := s.store.MutedUsers(members, time.Now())
	if err != nil {
		s.logger.Error("reading the mutes", "error", err, "channel_id", channel.ID)
	}
	if len(muted) > 0 {
		s.logger.Info("delivered muted", "channel", channel.Slug,
			"message_id", message.ID, "recipients", len(muted))
	}

	s.publishPerUser(members, eventMessageNew, func(userID int64) any {
		personal := payload
		personal.Silent = muted[userID]
		return personal
	})
}
