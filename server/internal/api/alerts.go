package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/rclsilver-org/home-notifications/server/internal/store"
)

// Event kinds for the alert lifecycle.
const (
	eventAlertOpened   = "alert.opened"
	eventAlertResolved = "alert.resolved"
	eventAlertAcked    = "alert.acked"
	eventAlertUnacked  = "alert.unacked"
)

// alertmanagerWebhook is Alertmanager's v4 payload. Only what carries
// meaning here is read; the rest is accepted and ignored.
type alertmanagerWebhook struct {
	Version  string              `json:"version"`
	Status   string              `json:"status"`
	Receiver string              `json:"receiver"`
	Alerts   []alertmanagerAlert `json:"alerts"`
}

type alertmanagerAlert struct {
	Status       string            `json:"status"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     string            `json:"startsAt"`
	EndsAt       string            `json:"endsAt"`
	GeneratorURL string            `json:"generatorURL"`
	Fingerprint  string            `json:"fingerprint"`
}

type alertPayload struct {
	ID           int64             `json:"id"`
	ChannelID    int64             `json:"channel_id"`
	ChannelSlug  string            `json:"channel_slug,omitempty"`
	Fingerprint  string            `json:"fingerprint"`
	Status       string            `json:"status"`
	Severity     string            `json:"severity"`
	Title        string            `json:"title"`
	Body         string            `json:"body"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	GeneratorURL string            `json:"generator_url,omitempty"`
	StartedAt    time.Time         `json:"started_at"`
	ResolvedAt   *time.Time        `json:"resolved_at,omitempty"`
	AckedBy      string            `json:"acked_by,omitempty"`
	AckedAt      *time.Time        `json:"acked_at,omitempty"`
	// Occurrences tells an alert that beats from an alert that is stable.
	Occurrences   int `json:"occurrences"`
	ReminderCount int `json:"reminder_count"`
}

// handleIngestAlertmanager consumes the Alertmanager webhook.
//
// Taking the webhook directly rather than through a ntfy bridge is the
// whole point: the payload carries the fingerprint, the full label set and
// the status, which is exactly what a stateful alert needs and precisely
// what a translation into a ntfy message destroys.
func (s *Server) handleIngestAlertmanager(w http.ResponseWriter, r *http.Request) {
	_, channel, ok := PublishTargetFrom(r.Context())
	if !ok {
		s.writeError(w, http.StatusUnauthorized, "invalid token")
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, maxBodySize))
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "body too large or unreadable")
		return
	}

	var payload alertmanagerWebhook
	if err := json.Unmarshal(body, &payload); err != nil {
		s.writeError(w, http.StatusBadRequest, "malformed Alertmanager payload")
		return
	}
	if len(payload.Alerts) == 0 {
		s.writeError(w, http.StatusBadRequest, "no alert in the payload")
		return
	}

	opened, resolved, refreshed := 0, 0, 0
	for _, incoming := range payload.Alerts {
		switch s.applyAlert(channel, incoming) {
		case resultOpened:
			opened++
		case resultResolved:
			resolved++
		case resultRefreshed:
			refreshed++
		}
	}

	s.logger.Info("alertmanager webhook", "channel", channel.Slug,
		"opened", opened, "resolved", resolved, "refreshed", refreshed)

	s.writeJSON(w, http.StatusOK, map[string]int{
		"opened": opened, "resolved": resolved, "refreshed": refreshed,
	})
}

type applyResult int

const (
	resultIgnored applyResult = iota
	resultOpened
	resultResolved
	resultRefreshed
)

func (s *Server) applyAlert(channel store.Channel, incoming alertmanagerAlert) applyResult {
	if incoming.Fingerprint == "" {
		s.logger.Warn("alert without a fingerprint, ignored", "channel", channel.Slug)
		return resultIgnored
	}

	existing, err := s.store.OpenAlert(channel.ID, incoming.Fingerprint)
	known := err == nil
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		s.logger.Error("reading the alert", "error", err)
		return resultIgnored
	}

	input := store.NewAlert{
		ChannelID:    channel.ID,
		Fingerprint:  incoming.Fingerprint,
		Severity:     incoming.Labels["severity"],
		Labels:       incoming.Labels,
		Annotations:  incoming.Annotations,
		GeneratorURL: incoming.GeneratorURL,
		StartedAt:    parseInstant(incoming.StartsAt),
	}

	if incoming.Status == store.AlertResolved {
		if !known {
			// Resolved without ever being seen firing: nothing to close.
			// Happens after a restart, or when the alert was created before
			// this receiver existed.
			return resultIgnored
		}
		if err := s.store.ResolveAlert(existing.ID, parseInstant(incoming.EndsAt)); err != nil {
			s.logger.Error("resolving the alert", "error", err, "alert_id", existing.ID)
			return resultIgnored
		}
		closed, err := s.store.AlertByID(existing.ID)
		if err != nil {
			return resultIgnored
		}
		s.announceAlert(channel, closed, eventAlertResolved)
		return resultResolved
	}

	if known {
		// Alertmanager repeats a firing alert for as long as it lasts.
		// Refreshing silently is what keeps repeat_interval from becoming a
		// second reminder engine fighting ours.
		if err := s.store.RefreshAlert(existing.ID, input); err != nil {
			s.logger.Error("refreshing the alert", "error", err, "alert_id", existing.ID)
		}
		return resultRefreshed
	}

	alert, err := s.store.CreateAlert(input)
	if err != nil {
		s.logger.Error("opening the alert", "error", err, "channel", channel.Slug)
		return resultIgnored
	}
	s.announceAlert(channel, alert, eventAlertOpened)
	s.scheduleFirstReminder(alert)
	return resultOpened
}

// announceAlert records the message the members will see and fans the
// lifecycle event out beside it. The message is what the phone shows; the
// event is what the alert console reacts to.
func (s *Server) announceAlert(channel store.Channel, alert store.Alert, kind string) {
	title, priority := alert.Title(), severityPriority(alert.Severity)
	body := alert.Body()
	if kind == eventAlertResolved {
		title = "Resolved — " + title
		// A resolution must not shout: it is good news arriving after the
		// alert already woke somebody.
		priority = store.PriorityLow
	}

	alertID := alert.ID
	message, err := s.store.CreateMessage(store.NewMessage{
		ChannelID: channel.ID,
		AlertID:   &alertID,
		Title:     title,
		Body:      body,
		Priority:  priority,
		Tags:      alertTags(alert),
		ClickURL:  alert.GeneratorURL,
	})
	if err != nil {
		s.logger.Error("recording the alert message", "error", err, "alert_id", alert.ID)
		return
	}

	s.publishMessage(channel, message)
	s.publishToChannel(channel.ID, kind, toAlertPayload(alert, channel.Slug))
}

// severityPriority maps Alertmanager's severity label onto the ntfy scale
// the rest of the system already speaks.
func severityPriority(severity string) int {
	switch severity {
	case "critical":
		return store.PriorityMax
	case "warning":
		return store.PriorityHigh
	case "info":
		return store.PriorityLow
	default:
		return store.PriorityDefault
	}
}

func alertTags(alert store.Alert) []string {
	tags := []string{"alert"}
	if alert.Severity != "" {
		tags = append(tags, alert.Severity)
	}
	if name := alert.Labels["alertname"]; name != "" {
		tags = append(tags, name)
	}
	return tags
}

// parseInstant reads an RFC3339 timestamp, returning the zero time for the
// absent or placeholder values Alertmanager uses ("0001-01-01T00:00:00Z").
func parseInstant(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil || parsed.Year() <= 1 {
		return time.Time{}
	}
	return parsed.UTC()
}

// handleListAlerts returns a channel's alerts; ?open=1 restricts to the
// ones still firing, which is the console view.
func (s *Server) handleListAlerts(w http.ResponseWriter, r *http.Request) {
	channel, _, ok := s.channelForMember(w, r)
	if !ok {
		return
	}

	query := store.AlertQuery{ChannelID: channel.ID, OnlyOpen: r.URL.Query().Get("open") == "1"}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil {
			query.Limit = value
		}
	}

	alerts, err := s.store.AlertsOf(query)
	if err != nil {
		s.logger.Error("listing the alerts", "error", err, "channel_id", channel.ID)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	payload := []alertPayload{}
	for _, alert := range alerts {
		payload = append(payload, toAlertPayload(alert, channel.Slug))
	}
	s.writeJSON(w, http.StatusOK, payload)
}

// handleAckAlert acknowledges an alert. Local only: nothing is written to
// Alertmanager, so the alert stays visible in the dashboards and the phone never
// writes into the chain it is watching.
func (s *Server) handleAckAlert(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "unknown alert")
		return
	}

	alert, err := s.store.AlertByID(id)
	if errors.Is(err, store.ErrNotFound) {
		s.writeError(w, http.StatusNotFound, "unknown alert")
		return
	}
	if err != nil {
		s.logger.Error("reading the alert", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if _, err := s.store.RoleOn(alert.ChannelID, user.ID); err != nil {
		s.writeError(w, http.StatusNotFound, "unknown alert")
		return
	}

	changed, err := s.store.AckAlert(alert.ID, user.ID)
	if err != nil {
		s.logger.Error("acknowledging the alert", "error", err, "alert_id", alert.ID)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	updated, err := s.store.AlertByID(alert.ID)
	if err != nil {
		s.logger.Error("rereading the alert", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if changed {
		channel, err := s.store.ChannelByID(alert.ChannelID)
		if err == nil {
			s.logger.Info("alert acknowledged", "alert_id", alert.ID, "by", user.Username)
			s.publishToChannel(alert.ChannelID, eventAlertAcked,
				toAlertPayload(updated, channel.Slug))
		}
	}

	s.writeJSON(w, http.StatusOK, toAlertPayload(updated, ""))
}

// handleUnackAlert takes an acknowledgement back. Acknowledging is the
// gesture made half asleep, on the wrong alert as often as on the right one;
// without a way back it would have to be corrected by waiting for the next
// reminder, which is precisely what the acknowledgement stopped.
func (s *Server) handleUnackAlert(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())

	alert, channel, ok := s.alertForMember(w, r)
	if !ok {
		return
	}

	changed, err := s.store.UnackAlert(alert.ID)
	if err != nil {
		s.logger.Error("taking the acknowledgement back", "error", err, "alert_id", alert.ID)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// The cadence starts again where it stopped, and only for an alert still
	// firing: a resolved one has nothing left to remind anybody about. The
	// count is not reset, so the next reminder reads "Rappel 4" and not
	// "Rappel 1" — the numbering is the history of what this alert cost in
	// interruptions, not a counter of the current attempt.
	if changed && alert.Status == store.AlertFiring {
		s.rearmReminder(alert)
	}

	updated, err := s.store.AlertByID(alert.ID)
	if err != nil {
		s.logger.Error("rereading the alert", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if changed {
		s.logger.Info("alert acknowledgement withdrawn", "alert_id", alert.ID,
			"by", user.Username)
		s.publishToChannel(alert.ChannelID, eventAlertUnacked,
			toAlertPayload(updated, channel.Slug))
	}

	s.writeJSON(w, http.StatusOK, toAlertPayload(updated, channel.Slug))
}

// rearmReminder puts an alert back on its cadence, keeping the count.
func (s *Server) rearmReminder(alert store.Alert) {
	policy, err := s.store.ReminderPolicyFor(alert.ChannelID, alert.Severity)
	if err != nil || !policy.Reminds() {
		return
	}
	next := policy.NextAfter(time.Now().UTC())
	if err := s.store.SetNextReminder(alert.ID, &next, alert.ReminderCount); err != nil {
		s.logger.Error("rearming the reminder", "error", err, "alert_id", alert.ID)
	}
}

func toAlertPayload(alert store.Alert, slug string) alertPayload {
	return alertPayload{
		ID: alert.ID, ChannelID: alert.ChannelID, ChannelSlug: slug,
		Fingerprint: alert.Fingerprint, Status: alert.Status, Severity: alert.Severity,
		Title: alert.Title(), Body: alert.Body(),
		Labels: alert.Labels, Annotations: alert.Annotations,
		GeneratorURL: alert.GeneratorURL, StartedAt: alert.StartedAt,
		ResolvedAt: alert.ResolvedAt, AckedBy: alert.AckedByName, AckedAt: alert.AckedAt,
		Occurrences: alert.Occurrences, ReminderCount: alert.ReminderCount,
	}
}

// RemindAlert re-notifies an alert nobody acknowledged. It satisfies
// reminder.Notifier.
func (s *Server) RemindAlert(alert store.Alert, count int) error {
	channel, err := s.store.ChannelByID(alert.ChannelID)
	if err != nil {
		return err
	}

	alertID := alert.ID
	message, err := s.store.CreateMessage(store.NewMessage{
		ChannelID: channel.ID,
		AlertID:   &alertID,
		// The count is in the title on purpose: a reminder that looks
		// exactly like the original is indistinguishable from a duplicate,
		// and gets dismissed as one.
		Title:    fmt.Sprintf("Rappel %d — %s", count, alert.Title()),
		Body:     alert.Body(),
		Priority: severityPriority(alert.Severity),
		Tags:     append(alertTags(alert), "reminder"),
		ClickURL: alert.GeneratorURL,
	})
	if err != nil {
		return err
	}

	s.logger.Info("alert reminder", "alert_id", alert.ID, "count", count,
		"channel", channel.Slug)
	s.publishMessage(channel, message)
	return nil
}

// scheduleFirstReminder arms the cadence when an alert opens. No policy
// means no reminder — silence is the default, so a channel only insists
// when somebody asked it to.
func (s *Server) scheduleFirstReminder(alert store.Alert) {
	policy, err := s.store.ReminderPolicyFor(alert.ChannelID, alert.Severity)
	if err != nil || !policy.Reminds() {
		return
	}
	next := policy.NextAfter(time.Now().UTC())
	if err := s.store.SetNextReminder(alert.ID, &next, 0); err != nil {
		s.logger.Error("scheduling the first reminder", "error", err, "alert_id", alert.ID)
	}
}

// handleListAllAlerts returns the caller's alerts across every channel they
// belong to. This is what the application's home screen reads.
func (s *Server) handleListAllAlerts(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())

	params := r.URL.Query()
	query := store.AlertQuery{
		OnlyOpen:    params.Get("open") == "1",
		OnlyUnacked: params.Get("unacked") == "1",
		OnlyClosed:  params.Get("closed") == "1",
		Severity:    params.Get("severity"),
	}
	if raw := params.Get("limit"); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil {
			query.Limit = value
		}
	}

	alerts, err := s.store.AlertsForUser(user.ID, query)
	if err != nil {
		s.logger.Error("listing the alerts", "error", err, "user_id", user.ID)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// The channel slug travels with each alert: the console shows alerts from
	// several channels at once, and an alert without its origin is unusable.
	slugs := map[int64]string{}
	payload := []alertPayload{}
	for _, alert := range alerts {
		slug, known := slugs[alert.ChannelID]
		if !known {
			if channel, err := s.store.ChannelByID(alert.ChannelID); err == nil {
				slug = channel.Slug
			}
			slugs[alert.ChannelID] = slug
		}
		payload = append(payload, toAlertPayload(alert, slug))
	}
	s.writeJSON(w, http.StatusOK, payload)
}

type alertDetailPayload struct {
	Alert alertPayload    `json:"alert"`
	Log   []alertLogEntry `json:"log"`
}

type alertLogEntry struct {
	At     time.Time `json:"at"`
	Kind   string    `json:"kind"`
	Detail string    `json:"detail"`
}

// handleGetAlert returns one alert with its history.
//
// The history is derived from what is already recorded — the messages the
// alert produced and its own lifecycle fields — rather than kept as a second
// journal. Two records of the same events would eventually disagree.
func (s *Server) handleGetAlert(w http.ResponseWriter, r *http.Request) {
	alert, channel, ok := s.alertForMember(w, r)
	if !ok {
		return
	}

	entries, err := s.store.LogOf(alert)
	if err != nil {
		s.logger.Error("building the alert log", "error", err, "alert_id", alert.ID)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	log := []alertLogEntry{}
	for _, entry := range entries {
		log = append(log, alertLogEntry{At: entry.At, Kind: entry.Kind, Detail: entry.Detail})
	}

	s.writeJSON(w, http.StatusOK, alertDetailPayload{
		Alert: toAlertPayload(alert, channel.Slug),
		Log:   log,
	})
}

// alertForMember resolves the alert in the path and checks membership. A
// non-member gets 404, as everywhere: the alert's existence is not theirs to
// learn.
func (s *Server) alertForMember(w http.ResponseWriter, r *http.Request) (store.Alert, store.Channel, bool) {
	user, _ := UserFrom(r.Context())

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "unknown alert")
		return store.Alert{}, store.Channel{}, false
	}

	alert, err := s.store.AlertByID(id)
	if errors.Is(err, store.ErrNotFound) {
		s.writeError(w, http.StatusNotFound, "unknown alert")
		return store.Alert{}, store.Channel{}, false
	}
	if err != nil {
		s.logger.Error("reading the alert", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return store.Alert{}, store.Channel{}, false
	}

	if _, err := s.store.RoleOn(alert.ChannelID, user.ID); err != nil {
		s.writeError(w, http.StatusNotFound, "unknown alert")
		return store.Alert{}, store.Channel{}, false
	}

	channel, err := s.store.ChannelByID(alert.ChannelID)
	if err != nil {
		s.logger.Error("reading the channel", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return store.Alert{}, store.Channel{}, false
	}

	return alert, channel, true
}
