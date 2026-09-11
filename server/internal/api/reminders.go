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
	// Severity empty means the whole scope — which is the only form that
	// makes sense on a channel carrying notifications rather than alerts.
	Severity string `json:"severity"`
	From     string `json:"from"`
	To       string `json:"to"`
	// Scope says where the row lives, so the application can show whether a
	// channel overrides the default or merely inherits it.
	Scope string `json:"scope"`
}

func toQuietHoursPayload(window store.QuietHours) quietHoursPayload {
	scope := scopeChannel
	if window.IsDefault() {
		scope = scopeDefault
	}
	return quietHoursPayload{
		Severity: window.Severity, From: window.From, To: window.To, Scope: scope,
	}
}

// handleListQuietHours returns a channel's windows and the defaults it
// inherits.
func (s *Server) handleListQuietHours(w http.ResponseWriter, r *http.Request) {
	channel, _, ok := s.channelForMember(w, r)
	if !ok {
		return
	}
	s.writeQuietHours(w, &channel.ID)
}

// handleListDefaultQuietHours returns the global windows alone.
func (s *Server) handleListDefaultQuietHours(w http.ResponseWriter, r *http.Request) {
	s.writeQuietHours(w, nil)
}

func (s *Server) writeQuietHours(w http.ResponseWriter, channelID *int64) {
	windows, err := s.store.QuietHoursOf(channelID)
	if err != nil {
		s.logger.Error("listing the quiet hours", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	payload := []quietHoursPayload{}
	for _, window := range windows {
		payload = append(payload, toQuietHoursPayload(window))
	}
	s.writeJSON(w, http.StatusOK, payload)
}

// handleSetQuietHours records a channel's window.
func (s *Server) handleSetQuietHours(w http.ResponseWriter, r *http.Request) {
	channel, _, ok := s.channelForAdmin(w, r)
	if !ok {
		return
	}
	s.setQuietHours(w, r, &channel.ID, channel.Slug)
}

// handleSetDefaultQuietHours records the global window.
//
// Any member may set it, like the mute: it says when the house sleeps, and
// asking for a right to say that would mean someone cannot.
func (s *Server) handleSetDefaultQuietHours(w http.ResponseWriter, r *http.Request) {
	s.setQuietHours(w, r, nil, "(default)")
}

// setQuietHours records a window, or removes it when both bounds are empty —
// "no quiet hours" is what an emptied form means, and asking for a second
// verb to express it would only be a second thing to get wrong.
func (s *Server) setQuietHours(
	w http.ResponseWriter, r *http.Request, channelID *int64, scope string,
) {
	var request quietHoursPayload
	if err := decodeJSON(r, &request); err != nil {
		s.writeError(w, http.StatusBadRequest, "malformed request")
		return
	}

	if request.From == "" && request.To == "" {
		if err := s.store.DeleteQuietHours(channelID, request.Severity); err != nil {
			s.logger.Error("deleting the quiet hours", "error", err)
			s.writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		s.logger.Info("quiet hours cleared", "scope", scope, "severity", request.Severity)
		s.writeJSON(w, http.StatusOK, request)
		return
	}

	// Half a window would silence at an unpredictable hour; better to refuse
	// than to guess the missing bound.
	window := store.QuietHours{
		ChannelID: channelID, Severity: request.Severity,
		From: request.From, To: request.To,
	}
	if err := s.store.SetQuietHours(window); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.logger.Info("quiet hours set", "scope", scope,
		"severity", request.Severity, "from", request.From, "to", request.To)
	s.writeJSON(w, http.StatusOK, toQuietHoursPayload(window))
}
