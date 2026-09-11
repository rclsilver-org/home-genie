package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/rclsilver-org/home-genie/server/internal/auth"
	"github.com/rclsilver-org/home-genie/server/internal/oidc"
	"github.com/rclsilver-org/home-genie/server/internal/store"
)

// oidcLoginRequest enrols a device with an identity token the application
// obtained from the provider by Authorization Code + PKCE.
type oidcLoginRequest struct {
	IDToken    string `json:"id_token"`
	DeviceName string `json:"device_name"`
	Platform   string `json:"platform"`
}

// handleOIDCLogin trades an OIDC identity token for a device token.
//
// The device token is what the app uses afterwards, so the provider is out
// of the request path from then on: it can be down without signing anybody
// out of their alerting tool.
func (s *Server) handleOIDCLogin(w http.ResponseWriter, r *http.Request) {
	if s.oidc == nil || !s.oidc.Enabled() {
		s.writeError(w, http.StatusNotImplemented, "OIDC is not configured on this server")
		return
	}

	var request oidcLoginRequest
	if err := decodeJSON(r, &request); err != nil {
		s.writeError(w, http.StatusBadRequest, "malformed request")
		return
	}
	request.DeviceName = strings.TrimSpace(request.DeviceName)
	if request.IDToken == "" || request.DeviceName == "" {
		s.writeError(w, http.StatusBadRequest, "id_token and device_name are required")
		return
	}
	if request.Platform == "" {
		request.Platform = "unknown"
	}

	identity, err := s.oidc.Verify(r.Context(), request.IDToken)
	if errors.Is(err, oidc.ErrDisabled) {
		s.writeError(w, http.StatusNotImplemented, "OIDC is not configured on this server")
		return
	}
	if err != nil {
		s.logger.Warn("OIDC token rejected", "error", err, "remote", r.RemoteAddr)
		s.writeError(w, http.StatusUnauthorized, "invalid token")
		return
	}

	user, err := s.resolveOIDCUser(identity)
	if err != nil {
		s.logger.Error("resolving the OIDC account", "error", err, "subject", identity.Subject)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	plain, hashed, err := auth.NewToken(auth.DeviceTokenPrefix)
	if err != nil {
		s.logger.Error("drawing the token", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	device, err := s.store.CreateDevice(user.ID, request.DeviceName, request.Platform, hashed)
	if err != nil {
		s.logger.Error("enrolling the device", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	s.logger.Info("device enrolled through OIDC", "user", user.Username,
		"device", device.Name, "device_id", device.ID)

	s.writeJSON(w, http.StatusCreated, loginResponse{
		Token:  plain,
		User:   toUserPayload(user),
		Device: toDevicePayload(device),
	})
}

// resolveOIDCUser finds or creates the account behind an identity.
//
// The match is on the subject and nothing else. Matching on the username or
// the email would mean that renaming an account at the provider, or
// reassigning an address inside an organisation, silently hands somebody
// another person's channels. The username is merely refreshed from the
// provider, which owns it.
func (s *Server) resolveOIDCUser(identity oidc.Identity) (store.User, error) {
	user, err := s.store.UserByOIDCSubject(identity.Subject)
	if err == nil {
		if user.Username != identity.Username || user.DisplayName != identity.DisplayName {
			if err := s.store.UpdateUserProfile(user.ID, identity.Username, identity.DisplayName); err != nil {
				// A rename colliding with an existing name must not block
				// the login: the account is identified by its subject.
				s.logger.Warn("profile not refreshed", "error", err, "user_id", user.ID)
			} else {
				user.Username, user.DisplayName = identity.Username, identity.DisplayName
			}
		}
		return user, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return store.User{}, err
	}

	created, err := s.store.CreateOIDCUser(identity.Subject, identity.Username, identity.DisplayName)
	if errors.Is(err, store.ErrConflict) {
		// The username is taken by a different account — typically the
		// local break-glass one sharing the name. Fall back to a name
		// derived from the subject rather than merging the two: an OIDC
		// login must never take over the account that exists precisely to
		// work when OIDC does not.
		fallback := identity.Username + "-oidc"
		s.logger.Warn("username already taken, falling back",
			"wanted", identity.Username, "used", fallback)
		return s.store.CreateOIDCUser(identity.Subject, fallback, identity.DisplayName)
	}
	return created, err
}

type authConfigPayload struct {
	OIDC oidcConfigPayload `json:"oidc"`
}

type oidcConfigPayload struct {
	Enabled  bool   `json:"enabled"`
	Issuer   string `json:"issuer,omitempty"`
	ClientID string `json:"client_id,omitempty"`
}

// handleAuthConfig tells the application how to authenticate.
//
// Unauthenticated on purpose, and harmless: an issuer URL and a public
// client id are exactly the values a native app would otherwise hardcode.
// Serving them means changing realms or renaming the client does not
// require shipping a new APK.
func (s *Server) handleAuthConfig(w http.ResponseWriter, r *http.Request) {
	payload := authConfigPayload{}
	if s.oidc != nil && s.oidc.Enabled() {
		payload.OIDC = oidcConfigPayload{
			Enabled:  true,
			Issuer:   s.oidc.Issuer(),
			ClientID: s.oidc.ClientID(),
		}
	}
	s.writeJSON(w, http.StatusOK, payload)
}
