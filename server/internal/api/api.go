// Package api exposes the HTTP surface. Routing uses net/http alone: since
// Go 1.22 its ServeMux understands method-and-path patterns, which is all
// this service needs, and it keeps the binary free of a router dependency.
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/rclsilver-org/home-genie/server/internal/hub"
	"github.com/rclsilver-org/home-genie/server/internal/oidc"
	"github.com/rclsilver-org/home-genie/server/internal/store"
)

// Server carries what the handlers need.
type Server struct {
	store  *store.Store
	hub    *hub.Hub
	oidc   *oidc.Verifier
	logger *slog.Logger
}

// New builds the HTTP surface.
func New(s *store.Store, h *hub.Hub, verifier *oidc.Verifier, logger *slog.Logger) *Server {
	return &Server{store: s, hub: h, oidc: verifier, logger: logger}
}

// Routes returns the mux serving the API.
func (s *Server) Routes() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/v1/auth/oidc", s.handleOIDCLogin)
	mux.HandleFunc("GET /api/v1/auth/config", s.handleAuthConfig)

	// Everything below authenticates a device, i.e. a human.
	device := func(handler http.HandlerFunc) http.Handler {
		return s.requireDevice(handler)
	}

	mux.Handle("GET /api/v1/me", device(s.handleMe))

	mux.Handle("GET /api/v1/users", device(s.handleSearchUsers))

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

	mux.Handle("GET /api/v1/channels/{id}/messages", device(s.handleListMessages))

	mux.Handle("POST /api/v1/channels/{id}/read", device(s.handleMarkChannelRead))
	mux.Handle("GET /api/v1/messages", device(s.handleListFeed))
	mux.Handle("GET /api/v1/messages/unread", device(s.handleUnreadFeedCount))
	mux.Handle("POST /api/v1/messages/read", device(s.handleMarkFeedRead))
	mux.Handle("POST /api/v1/messages/{id}/read", device(s.handleMarkRead))
	mux.Handle("GET /api/v1/messages/{id}/timeline", device(s.handleTimeline))

	mux.Handle("GET /api/v1/channels/{id}/alerts", device(s.handleListAlerts))
	mux.Handle("GET /api/v1/alerts", device(s.handleListAllAlerts))
	mux.Handle("GET /api/v1/alerts/{id}", device(s.handleGetAlert))
	mux.Handle("POST /api/v1/alerts/{id}/ack", device(s.handleAckAlert))
	mux.Handle("DELETE /api/v1/alerts/{id}/ack", device(s.handleUnackAlert))

	mux.Handle("GET /api/v1/channels/{id}/reminders", device(s.handleListReminderPolicies))
	mux.Handle("PUT /api/v1/channels/{id}/reminders", device(s.handleSetReminderPolicy))
	mux.Handle("GET /api/v1/channels/{id}/quiet-hours", device(s.handleListQuietHours))
	mux.Handle("PUT /api/v1/channels/{id}/quiet-hours", device(s.handleSetQuietHours))

	mux.Handle("GET /api/v1/ws", device(s.handleWS))

	// Alertmanager posts here, authenticated like any other producer.
	mux.Handle("POST /api/v1/ingest/alertmanager/{slug}",
		s.requirePublishToken(http.HandlerFunc(s.handleIngestAlertmanager)))

	// Producer path. Registered last and at the root, where ntfy puts it:
	// the more specific /api/... patterns win under Go 1.22 routing, and
	// reserved slugs keep a channel from ever shadowing them.
	mux.Handle("POST /{slug}", s.requirePublishToken(http.HandlerFunc(s.handleIngestNtfy)))

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
