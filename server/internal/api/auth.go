package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/rclsilver-org/home-genie/server/internal/auth"
	"github.com/rclsilver-org/home-genie/server/internal/store"
)

type contextKey int

const (
	contextKeyUser contextKey = iota
	contextKeyDevice
)

// loginRequest enrols a device against the local account. OIDC enrolment
// lands beside it later; the device token it returns is the same either way,
// which is what keeps the identity provider off the request path.
type loginRequest struct {
	Username   string `json:"username"`
	Password   string `json:"password"`
	DeviceName string `json:"device_name"`
	Platform   string `json:"platform"`
}

type loginResponse struct {
	// Shown once. The server only ever stores its hash.
	Token  string        `json:"token"`
	User   userPayload   `json:"user"`
	Device devicePayload `json:"device"`
}

type userPayload struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	IsAdmin     bool   `json:"is_admin"`
	IsLocal     bool   `json:"is_local"`
}

type devicePayload struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Platform  string `json:"platform"`
	Transport string `json:"transport"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var request loginRequest
	if err := decodeJSON(r, &request); err != nil {
		s.writeError(w, http.StatusBadRequest, "malformed request")
		return
	}

	request.Username = strings.TrimSpace(request.Username)
	request.DeviceName = strings.TrimSpace(request.DeviceName)
	if request.Username == "" || request.Password == "" || request.DeviceName == "" {
		s.writeError(w, http.StatusBadRequest, "username, password and device_name are required")
		return
	}
	if request.Platform == "" {
		request.Platform = "unknown"
	}

	user, err := s.store.UserByUsername(request.Username)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		s.logger.Error("looking the user up", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Verify against a decoy hash when the account is unknown, so that a
	// missing account and a wrong password take the same time and cannot be
	// told apart by an outsider.
	stored := user.PasswordHash
	if stored == "" {
		stored = decoyHash
	}

	ok, verifyErr := auth.VerifyPassword(request.Password, stored)
	if verifyErr != nil && !errors.Is(verifyErr, auth.ErrInvalidHash) {
		s.logger.Error("verifying the password", "error", verifyErr)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !ok || user.ID == 0 || !user.IsLocal() {
		s.logger.Warn("failed login", "username", request.Username,
			"remote", r.RemoteAddr)
		s.writeError(w, http.StatusUnauthorized, "invalid credentials")
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

	s.logger.Info("device enrolled", "user", user.Username,
		"device", device.Name, "device_id", device.ID)

	s.writeJSON(w, http.StatusCreated, loginResponse{
		Token:  plain,
		User:   toUserPayload(user),
		Device: toDevicePayload(device),
	})
}

// requireDevice authenticates a request by its device token and puts the
// device and its owner in the context.
func (s *Server) requireDevice(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := auth.BearerToken(r.Header.Get("Authorization"))
		if token == "" {
			s.writeError(w, http.StatusUnauthorized, "missing token")
			return
		}

		device, user, err := s.store.DeviceByTokenHash(auth.HashToken(token))
		if errors.Is(err, store.ErrNotFound) {
			s.writeError(w, http.StatusUnauthorized, "invalid token")
			return
		}
		if err != nil {
			s.logger.Error("resolving the token", "error", err)
			s.writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

		// Best effort: a failed touch must not deny a legitimate request.
		if err := s.store.TouchDevice(device.ID); err != nil {
			s.logger.Warn("updating the device", "error", err, "device_id", device.ID)
		}

		ctx := context.WithValue(r.Context(), contextKeyUser, user)
		ctx = context.WithValue(ctx, contextKeyDevice, device)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// UserFrom returns the authenticated user, if any.
func UserFrom(ctx context.Context) (store.User, bool) {
	user, ok := ctx.Value(contextKeyUser).(store.User)
	return user, ok
}

// DeviceFrom returns the authenticated device, if any.
func DeviceFrom(ctx context.Context) (store.Device, bool) {
	device, ok := ctx.Value(contextKeyDevice).(store.Device)
	return device, ok
}

func toUserPayload(user store.User) userPayload {
	return userPayload{
		ID:          user.ID,
		Username:    user.Username,
		DisplayName: user.DisplayName,
		IsAdmin:     user.IsAdmin,
		IsLocal:     user.IsLocal(),
	}
}

func toDevicePayload(device store.Device) devicePayload {
	return devicePayload{
		ID:        device.ID,
		Name:      device.Name,
		Platform:  device.Platform,
		Transport: device.Transport,
	}
}

const contextKeyPublishTarget contextKey = 2

// publishTarget is what a machine's token resolves to: a single channel,
// write-only.
type publishTarget struct {
	Token   store.PublishToken
	Channel store.Channel
}

// requirePublishToken authenticates a producer. It is deliberately a
// separate middleware from requireDevice rather than a branch inside it: a
// publish token must never reach a read endpoint, and a device token must
// never be usable as a producer. Keeping the two paths apart makes that a
// property of the routing table rather than of a conditional someone could
// get wrong later.
func (s *Server) requirePublishToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := auth.BearerToken(r.Header.Get("Authorization"))
		if token == "" {
			s.writeError(w, http.StatusUnauthorized, "missing token")
			return
		}

		record, channel, err := s.store.PublishTokenByHash(auth.HashToken(token))
		if errors.Is(err, store.ErrNotFound) {
			// Covers the unknown token, the revoked one, and a device token
			// used as a publisher: all indistinguishable from outside.
			s.writeError(w, http.StatusUnauthorized, "invalid token")
			return
		}
		if err != nil {
			s.logger.Error("resolving the publish token", "error", err)
			s.writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

		// The path segment must match the token's channel, so a mistyped URL
		// fails loudly instead of publishing somewhere unintended.
		if slug := r.PathValue("slug"); slug != "" && slug != channel.Slug {
			s.writeError(w, http.StatusForbidden, "this token does not publish to this channel")
			return
		}

		if err := s.store.TouchPublishToken(record.ID); err != nil {
			s.logger.Warn("updating the token", "error", err, "token_id", record.ID)
		}

		ctx := context.WithValue(r.Context(), contextKeyPublishTarget,
			publishTarget{Token: record, Channel: channel})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// PublishTargetFrom returns the channel a producer is authenticated for.
func PublishTargetFrom(ctx context.Context) (store.PublishToken, store.Channel, bool) {
	target, ok := ctx.Value(contextKeyPublishTarget).(publishTarget)
	return target.Token, target.Channel, ok
}
