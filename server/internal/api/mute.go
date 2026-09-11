package api

import (
	"net/http"
	"time"
)

type mutePayload struct {
	// A nil MutedUntil means "I am not muted".
	MutedUntil *time.Time `json:"muted_until"`
}

// handleGetMute returns the caller's own mute.
func (s *Server) handleGetMute(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())

	until, err := s.store.MutedUntil(user.ID)
	if err != nil {
		s.logger.Error("reading the mute", "error", err, "user_id", user.ID)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.writeJSON(w, http.StatusOK, mutePayload{MutedUntil: until})
}

type setMuteRequest struct {
	// Empty or absent lifts the mute, which is what an emptied form means.
	MutedUntil string `json:"muted_until"`
}

// handleSetMute silences the caller until an instant, or lifts their silence.
//
// No right is checked, and none is needed: one silences oneself. Muting on
// somebody else's behalf is not a gesture this application offers, so there
// is nothing to protect here.
func (s *Server) handleSetMute(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())

	var request setMuteRequest
	if err := decodeJSON(r, &request); err != nil {
		s.writeError(w, http.StatusBadRequest, "malformed request")
		return
	}

	var until *time.Time
	if request.MutedUntil != "" {
		parsed, err := time.Parse(time.RFC3339, request.MutedUntil)
		if err != nil {
			s.writeError(w, http.StatusBadRequest, "muted_until must be an RFC3339 instant")
			return
		}
		until = &parsed
	}

	if err := s.store.SetMute(user.ID, until); err != nil {
		s.logger.Error("recording the mute", "error", err, "user_id", user.ID)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if until == nil {
		s.logger.Info("mute lifted", "user", user.Username)
	} else {
		s.logger.Info("user muted", "user", user.Username, "until", *until)
	}

	// Announced to this user's devices, and to them alone: it is their own
	// silence, and their tablet should show it without being asked again.
	s.publishToUsers([]int64{user.ID}, eventMuteChanged, mutePayload{MutedUntil: until})
	s.writeJSON(w, http.StatusOK, mutePayload{MutedUntil: until})
}
