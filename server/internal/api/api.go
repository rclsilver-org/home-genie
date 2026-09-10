// Package api exposes the HTTP surface. Routing uses net/http alone: since
// Go 1.22 its ServeMux understands method-and-path patterns, which is all
// this service needs, and it keeps the binary free of a router dependency.
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/rclsilver-org/home-notifications/server/internal/hub"
	"github.com/rclsilver-org/home-notifications/server/internal/store"
)

// Server carries what the handlers need.
type Server struct {
	store  *store.Store
	hub    *hub.Hub
	logger *slog.Logger
}

// New builds the HTTP surface.
func New(s *store.Store, h *hub.Hub, logger *slog.Logger) *Server {
	return &Server{store: s, hub: h, logger: logger}
}

// Routes returns the mux serving the API.
func (s *Server) Routes() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)

	// Everything below authenticates a device, i.e. a human.
	device := func(handler http.HandlerFunc) http.Handler {
		return s.requireDevice(handler)
	}

	mux.Handle("GET /api/v1/me", device(s.handleMe))

	mux.Handle("GET /api/v1/channels", device(s.handleListChannels))
	mux.Handle("POST /api/v1/channels", device(s.handleCreateChannel))
	mux.Handle("GET /api/v1/channels/{id}", device(s.handleGetChannel))
	mux.Handle("PATCH /api/v1/channels/{id}", device(s.handleUpdateChannel))
	mux.Handle("DELETE /api/v1/channels/{id}", device(s.handleDeleteChannel))

	mux.Handle("GET /api/v1/channels/{id}/members", device(s.handleListMembers))
	mux.Handle("PUT /api/v1/channels/{id}/members/{username}", device(s.handleSetMember))
	mux.Handle("DELETE /api/v1/channels/{id}/members/{username}", device(s.handleRemoveMember))

	mux.Handle("GET /api/v1/channels/{id}/tokens", device(s.handleListTokens))
	mux.Handle("POST /api/v1/channels/{id}/tokens", device(s.handleCreateToken))
	mux.Handle("DELETE /api/v1/channels/{id}/tokens/{tokenID}", device(s.handleRevokeToken))

	mux.Handle("GET /api/v1/ws", device(s.handleWS))

	return mux
}

// writeJSON sends a value, or logs the failure — by the time encoding
// fails the status line is already out, so there is nothing to tell the
// client.
func (s *Server) writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		s.logger.Error("writing the response", "error", err)
	}
}

// errorResponse is the single error shape of the API, so clients have one
// thing to parse.
type errorResponse struct {
	Error string `json:"error"`
}

func (s *Server) writeError(w http.ResponseWriter, status int, message string) {
	s.writeJSON(w, status, errorResponse{Error: message})
}

// decodeJSON reads a request body, refusing unknown fields so a typo in a
// client is an error rather than a silently ignored setting.
func decodeJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}
