package api

import (
	"net/http"
	"time"

	"github.com/rclsilver-org/home-genie/server/internal/store"
)

type reminderPolicyPayload struct {
	Severity string `json:"severity"`
	// Seconds rather than a duration string: the client is an Android app,
	// and a number needs no parser on either side.
	IntervalSeconds int  `json:"interval_seconds"`
	Enabled         bool `json:"enabled"`
	// Scope says where the row lives, so the app can show whether a channel
	// overrides the default or merely inherits it.
	Scope string `json:"scope"`
}

const (
	scopeDefault = "default"
	scopeChannel = "channel"
)

// handleListReminderPolicies returns the policies that apply to a channel:
// its own overrides and the defaults it would otherwise inherit.
func (s *Server) handleListReminderPolicies(w http.ResponseWriter, r *http.Request) {
	channel, _, ok := s.channelForMember(w, r)
	if !ok {
		return
	}

	policies, err := s.store.ReminderPoliciesOf(channel.ID)
	if err != nil {
		s.logger.Error("listing the policies", "error", err, "channel_id", channel.ID)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	payload := []reminderPolicyPayload{}
	for _, policy := range policies {
		scope := scopeDefault
		if policy.ChannelID != nil {
			scope = scopeChannel
		}
		payload = append(payload, reminderPolicyPayload{
			Severity:        policy.Severity,
			IntervalSeconds: int(policy.Interval.Seconds()),
			Enabled:         policy.Enabled,
			Scope:           scope,
		})
	}
	s.writeJSON(w, http.StatusOK, payload)
}

// handleSetReminderPolicy sets a channel's override for a severity.
func (s *Server) handleSetReminderPolicy(w http.ResponseWriter, r *http.Request) {
	channel, _, ok := s.channelForAdmin(w, r)
	if !ok {
		return
	}

	var request reminderPolicyPayload
	if err := decodeJSON(r, &request); err != nil {
		s.writeError(w, http.StatusBadRequest, "malformed request")
		return
	}
	if request.Severity == "" {
		s.writeError(w, http.StatusBadRequest, "severity is required")
		return
	}
	if request.IntervalSeconds < 0 {
		s.writeError(w, http.StatusBadRequest, "interval_seconds must not be negative")
		return
	}
	policy := store.ReminderPolicy{
		ChannelID: &channel.ID,
		Severity:  request.Severity,
		Interval:  time.Duration(request.IntervalSeconds) * time.Second,
		Enabled:   request.Enabled,
	}
	if err := s.store.SetReminderPolicy(policy); err != nil {
		s.logger.Error("recording the policy", "error", err, "channel_id", channel.ID)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	s.logger.Info("reminder policy set", "channel", channel.Slug,
		"severity", policy.Severity, "interval", policy.Interval)

	request.Scope = scopeChannel
	s.writeJSON(w, http.StatusOK, request)
}

type quietHoursPayload struct {
	// Severity empty means the whole channel — which is the only form that
	// makes sense on a channel carrying notifications rather than alerts.
	Severity string `json:"severity"`
	From     string `json:"from"`
	To       string `json:"to"`
}

// handleListQuietHours returns a channel's quiet windows.
func (s *Server) handleListQuietHours(w http.ResponseWriter, r *http.Request) {
	channel, _, ok := s.channelForMember(w, r)
	if !ok {
		return
	}

	windows, err := s.store.QuietHoursOf(channel.ID)
	if err != nil {
		s.logger.Error("listing the quiet hours", "error", err, "channel_id", channel.ID)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	payload := []quietHoursPayload{}
	for _, window := range windows {
		payload = append(payload, quietHoursPayload{
			Severity: window.Severity, From: window.From, To: window.To,
		})
	}
	s.writeJSON(w, http.StatusOK, payload)
}

// handleSetQuietHours records a window, or removes it when both bounds are
// empty — "no quiet hours" is what an emptied form means, and asking for a
// second verb to express it would only be a second thing to get wrong.
func (s *Server) handleSetQuietHours(w http.ResponseWriter, r *http.Request) {
	channel, _, ok := s.channelForAdmin(w, r)
	if !ok {
		return
	}

	var request quietHoursPayload
	if err := decodeJSON(r, &request); err != nil {
		s.writeError(w, http.StatusBadRequest, "malformed request")
		return
	}

	if request.From == "" && request.To == "" {
		if err := s.store.DeleteQuietHours(channel.ID, request.Severity); err != nil {
			s.logger.Error("deleting the quiet hours", "error", err)
			s.writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		s.logger.Info("quiet hours cleared", "channel", channel.Slug,
			"severity", request.Severity)
		s.writeJSON(w, http.StatusOK, request)
		return
	}

	// Half a window would silence at an unpredictable hour; better to refuse
	// than to guess the missing bound.
	window := store.QuietHours{
		ChannelID: channel.ID, Severity: request.Severity,
		From: request.From, To: request.To,
	}
	if err := s.store.SetQuietHours(window); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.logger.Info("quiet hours set", "channel", channel.Slug,
		"severity", request.Severity, "from", request.From, "to", request.To)
	s.writeJSON(w, http.StatusOK, request)
}
