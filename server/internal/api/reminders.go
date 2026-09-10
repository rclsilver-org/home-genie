package api

import (
	"net/http"
	"time"

	"github.com/rclsilver-org/home-notifications/server/internal/store"
)

type reminderPolicyPayload struct {
	Severity string `json:"severity"`
	// Seconds rather than a duration string: the client is an Android app,
	// and a number needs no parser on either side.
	IntervalSeconds int    `json:"interval_seconds"`
	QuietFrom       string `json:"quiet_from,omitempty"`
	QuietTo         string `json:"quiet_to,omitempty"`
	Enabled         bool   `json:"enabled"`
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
			QuietFrom:       policy.QuietFrom,
			QuietTo:         policy.QuietTo,
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
	// A quiet window given half-way would silence at an unpredictable hour;
	// better to refuse than to guess the missing half.
	if (request.QuietFrom == "") != (request.QuietTo == "") {
		s.writeError(w, http.StatusBadRequest,
			"quiet_from and quiet_to go together")
		return
	}

	policy := store.ReminderPolicy{
		ChannelID: &channel.ID,
		Severity:  request.Severity,
		Interval:  time.Duration(request.IntervalSeconds) * time.Second,
		QuietFrom: request.QuietFrom,
		QuietTo:   request.QuietTo,
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
