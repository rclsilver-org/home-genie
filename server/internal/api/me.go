package api

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"time"

	"github.com/rclsilver-org/home-genie/server/internal/auth"
)

// decoyHash is verified against when the account does not exist, so that a
// wrong username and a wrong password cost the same time. Computed once at
// startup from a random secret nobody can supply.
var decoyHash = mustDecoyHash()

func mustDecoyHash() string {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		// Without a CSPRNG nothing else in this server is trustworthy either.
		panic("no entropy available: " + err.Error())
	}
	hash, err := auth.HashPassword(base64.RawStdEncoding.EncodeToString(raw))
	if err != nil {
		panic("computing the decoy hash: " + err.Error())
	}
	return hash
}

type meResponse struct {
	User    userPayload    `json:"user"`
	Devices []mePayloadDev `json:"devices"`
}

type mePayloadDev struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	Platform   string     `json:"platform"`
	Transport  string     `json:"transport"`
	Connected  bool       `json:"connected"`
	LastSeenAt *time.Time `json:"last_seen_at"`
	Current    bool       `json:"current"`
}

// handleMe reports the caller and its devices. It is the smallest endpoint
// that proves the whole authentication chain, and it is what the app's
// connection diagnostic will read.
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFrom(r.Context())
	if !ok {
		s.writeError(w, http.StatusUnauthorized, "missing token")
		return
	}
	current, _ := DeviceFrom(r.Context())

	devices, err := s.store.DevicesByUser(user.ID)
	if err != nil {
		s.logger.Error("listing the devices", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	payload := meResponse{User: toUserPayload(user), Devices: []mePayloadDev{}}
	for _, device := range devices {
		payload.Devices = append(payload.Devices, mePayloadDev{
			ID:         device.ID,
			Name:       device.Name,
			Platform:   device.Platform,
			Transport:  device.Transport,
			Connected:  device.IsConnected(),
			LastSeenAt: device.LastSeenAt,
			Current:    device.ID == current.ID,
		})
	}

	s.writeJSON(w, http.StatusOK, payload)
}

type userSuggestionPayload struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
}

// handleSearchUsers backs the completion when adding a member.
//
// Any authenticated device may call it. The alternative — restricting it to
// channel owners — would protect a list that anyone can already read from the
// members of their own channels, at the cost of a screen that works for some
// accounts and not others.
func (s *Server) handleSearchUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.store.SearchUsers(r.URL.Query().Get("q"), 20)
	if err != nil {
		s.logger.Error("searching the users", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	payload := []userSuggestionPayload{}
	for _, user := range users {
		payload = append(payload, userSuggestionPayload{
			Username: user.Username, DisplayName: user.DisplayName,
		})
	}
	s.writeJSON(w, http.StatusOK, payload)
}
