package api

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rclsilver-org/home-genie/server/internal/auth"
	"github.com/rclsilver-org/home-genie/server/internal/store"
)

type channelPayload struct {
	ID          int64  `json:"id"`
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Role        string `json:"role,omitempty"`
	// Per caller: a message read by one member stays unread for the others.
	Unread int `json:"unread"`
}

type memberPayload struct {
	UserID      int64  `json:"user_id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
}

type publishTokenPayload struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	LastUsedAt *time.Time `json:"last_used_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
	// Only ever set on creation: the clear value is shown once.
	Token string `json:"token,omitempty"`
}

// handleListChannels returns the caller's channels with their role. It never
// mentions a channel they are not a member of.
func (s *Server) handleListChannels(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())

	memberships, err := s.store.MembershipsOf(user.ID)
	if err != nil {
		s.logger.Error("listing the channels", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	unread, err := s.store.UnreadCounts(user.ID)
	if err != nil {
		s.logger.Error("counting the unread messages", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	payload := []channelPayload{}
	for _, membership := range memberships {
		entry := toChannelPayload(membership.Channel, membership.Role)
		entry.Unread = unread[membership.Channel.ID]
		payload = append(payload, entry)
	}
	s.writeJSON(w, http.StatusOK, payload)
}

type createChannelRequest struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (s *Server) handleCreateChannel(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())

	var request createChannelRequest
	if err := decodeJSON(r, &request); err != nil {
		s.writeError(w, http.StatusBadRequest, "malformed request")
		return
	}

	channel, err := s.store.CreateChannel(
		strings.TrimSpace(request.Slug), request.Name, request.Description, user.ID)
	switch {
	case errors.Is(err, store.ErrInvalidSlug):
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	case errors.Is(err, store.ErrConflict):
		s.writeError(w, http.StatusConflict, "this slug is already taken")
		return
	case err != nil:
		s.logger.Error("creating the channel", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	s.logger.Info("channel created", "slug", channel.Slug, "owner", user.Username)
	s.publishToChannel(channel.ID, store.EventChannelCreated, toChannelPayload(channel, store.RoleOwner))
	s.writeJSON(w, http.StatusCreated, toChannelPayload(channel, store.RoleOwner))
}

func (s *Server) handleGetChannel(w http.ResponseWriter, r *http.Request) {
	channel, role, ok := s.channelForMember(w, r)
	if !ok {
		return
	}
	s.writeJSON(w, http.StatusOK, toChannelPayload(channel, role))
}

type updateChannelRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

func (s *Server) handleUpdateChannel(w http.ResponseWriter, r *http.Request) {
	channel, _, ok := s.channelForAdmin(w, r)
	if !ok {
		return
	}

	var request updateChannelRequest
	if err := decodeJSON(r, &request); err != nil {
		s.writeError(w, http.StatusBadRequest, "malformed request")
		return
	}

	if err := s.store.UpdateChannel(channel.ID, request.Name, request.Description); err != nil {
		s.logger.Error("updating the channel", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	updated, err := s.store.ChannelByID(channel.ID)
	if err != nil {
		s.logger.Error("rereading the channel", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.publishToChannel(updated.ID, store.EventChannelUpdated, toChannelPayload(updated, ""))
	s.writeJSON(w, http.StatusOK, toChannelPayload(updated, store.RoleOwner))
}

func (s *Server) handleDeleteChannel(w http.ResponseWriter, r *http.Request) {
	channel, _, ok := s.channelForAdmin(w, r)
	if !ok {
		return
	}

	// The members have to be read before the delete: the cascade removes
	// them, and afterwards there would be nobody left to notify.
	members, err := s.store.MemberUserIDs(channel.ID)
	if err != nil {
		s.logger.Error("listing the members", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := s.store.DeleteChannel(channel.ID); err != nil {
		s.logger.Error("deleting the channel", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	s.publishToUsers(members, store.EventChannelDeleted,
		map[string]any{"id": channel.ID, "slug": channel.Slug})

	s.logger.Info("channel deleted", "slug", channel.Slug)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListMembers(w http.ResponseWriter, r *http.Request) {
	channel, _, ok := s.channelForMember(w, r)
	if !ok {
		return
	}

	members, err := s.store.MembersOf(channel.ID)
	if err != nil {
		s.logger.Error("listing the members", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	payload := []memberPayload{}
	for _, member := range members {
		payload = append(payload, memberPayload{
			UserID:      member.UserID,
			Username:    member.Username,
			DisplayName: member.DisplayName,
			Role:        string(member.Role),
		})
	}
	s.writeJSON(w, http.StatusOK, payload)
}

type setMemberRequest struct {
	Role string `json:"role"`
}

func (s *Server) handleSetMember(w http.ResponseWriter, r *http.Request) {
	channel, _, ok := s.channelForAdmin(w, r)
	if !ok {
		return
	}

	var request setMemberRequest
	if err := decodeJSON(r, &request); err != nil {
		s.writeError(w, http.StatusBadRequest, "malformed request")
		return
	}
	role := store.Role(request.Role)
	if !role.Valid() {
		s.writeError(w, http.StatusBadRequest, "role must be owner, writer or reader")
		return
	}

	target, err := s.store.UserByUsername(r.PathValue("username"))
	if errors.Is(err, store.ErrNotFound) {
		s.writeError(w, http.StatusNotFound, "unknown user")
		return
	}
	if err != nil {
		s.logger.Error("looking the user up", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := s.store.SetMember(channel.ID, target.ID, role); err != nil {
		s.logger.Error("assigning the member", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	payload := memberPayload{
		UserID:      target.ID,
		Username:    target.Username,
		DisplayName: target.DisplayName,
		Role:        string(role),
	}
	// Published after the membership exists, so the new member is among the
	// recipients and learns about the channel they just gained access to.
	s.publishToChannel(channel.ID, store.EventMemberChanged, map[string]any{
		"channel_id": channel.ID, "member": payload,
	})
	s.writeJSON(w, http.StatusOK, payload)
}

func (s *Server) handleRemoveMember(w http.ResponseWriter, r *http.Request) {
	channel, _, ok := s.channelForAdmin(w, r)
	if !ok {
		return
	}

	target, err := s.store.UserByUsername(r.PathValue("username"))
	if errors.Is(err, store.ErrNotFound) {
		s.writeError(w, http.StatusNotFound, "unknown user")
		return
	}
	if err != nil {
		s.logger.Error("looking the user up", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	err = s.store.RemoveMember(channel.ID, target.ID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		s.writeError(w, http.StatusNotFound, "this user is not a member")
		return
	case errors.Is(err, store.ErrConflict):
		s.writeError(w, http.StatusConflict,
			"removing the last owner would leave the channel unadministrable")
		return
	case err != nil:
		s.logger.Error("removing the member", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// The removed user is no longer a member, so they would not be in the
	// fanout: they are told separately, otherwise their app would keep
	// showing a channel they can no longer read.
	s.publishToChannel(channel.ID, store.EventMemberRemoved, map[string]any{
		"channel_id": channel.ID, "user_id": target.ID, "username": target.Username,
	})
	s.publishToUsers([]int64{target.ID}, store.EventMemberRemoved, map[string]any{
		"channel_id": channel.ID, "user_id": target.ID, "username": target.Username,
	})

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListTokens(w http.ResponseWriter, r *http.Request) {
	channel, _, ok := s.channelForAdmin(w, r)
	if !ok {
		return
	}

	tokens, err := s.store.PublishTokensOf(channel.ID)
	if err != nil {
		s.logger.Error("listing the tokens", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	payload := []publishTokenPayload{}
	for _, token := range tokens {
		payload = append(payload, publishTokenPayload{
			ID:         token.ID,
			Name:       token.Name,
			LastUsedAt: token.LastUsedAt,
			RevokedAt:  token.RevokedAt,
		})
	}
	s.writeJSON(w, http.StatusOK, payload)
}

type createTokenRequest struct {
	Name string `json:"name"`
}

func (s *Server) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	channel, _, ok := s.channelForAdmin(w, r)
	if !ok {
		return
	}

	var request createTokenRequest
	if err := decodeJSON(r, &request); err != nil {
		s.writeError(w, http.StatusBadRequest, "malformed request")
		return
	}
	if strings.TrimSpace(request.Name) == "" {
		s.writeError(w, http.StatusBadRequest, "name is required, to know which producer holds it")
		return
	}

	plain, hashed, err := auth.NewToken(auth.PublishTokenPrefix, request.Name)
	if err != nil {
		s.logger.Error("drawing the token", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	token, err := s.store.CreatePublishToken(channel.ID, request.Name, hashed)
	if err != nil {
		s.logger.Error("creating the token", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	s.logger.Info("publish token created", "channel", channel.Slug, "name", token.Name)
	s.writeJSON(w, http.StatusCreated, publishTokenPayload{
		ID:    token.ID,
		Name:  token.Name,
		Token: plain,
	})
}

func (s *Server) handleRevokeToken(w http.ResponseWriter, r *http.Request) {
	channel, _, ok := s.channelForAdmin(w, r)
	if !ok {
		return
	}

	tokenID, err := strconv.ParseInt(r.PathValue("tokenID"), 10, 64)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "unknown token")
		return
	}

	// The first DELETE revokes, the second erases. Revoking cuts publishing
	// immediately and keeps the row, which is the only trace saying
	// which producer was cut off and when; once that trace has served, the
	// same gesture removes it from the list.
	err = s.store.RevokePublishToken(channel.ID, tokenID)
	if errors.Is(err, store.ErrNotFound) {
		if err := s.store.DeletePublishToken(channel.ID, tokenID); err != nil {
			s.writeError(w, http.StatusNotFound, "unknown token")
			return
		}
		s.logger.Info("publish token deleted", "channel", channel.Slug, "token_id", tokenID)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		s.logger.Error("revoking the token", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	s.logger.Info("publish token revoked", "channel", channel.Slug, "token_id", tokenID)
	w.WriteHeader(http.StatusNoContent)
}

// channelForMember resolves the channel in the path and checks the caller
// belongs to it.
//
// A non-member gets 404, not 403: answering "forbidden" would confirm the
// channel exists to someone who has no business knowing.
func (s *Server) channelForMember(w http.ResponseWriter, r *http.Request) (store.Channel, store.Role, bool) {
	user, _ := UserFrom(r.Context())

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "unknown channel")
		return store.Channel{}, "", false
	}

	role, err := s.store.RoleOn(id, user.ID)
	if errors.Is(err, store.ErrNotFound) {
		s.writeError(w, http.StatusNotFound, "unknown channel")
		return store.Channel{}, "", false
	}
	if err != nil {
		s.logger.Error("reading the role", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return store.Channel{}, "", false
	}

	channel, err := s.store.ChannelByID(id)
	if err != nil {
		s.logger.Error("reading the channel", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return store.Channel{}, "", false
	}

	return channel, role, true
}

// channelForAdmin additionally requires the owner role. Here 403 is right:
// the caller is a member, so the channel's existence is not a secret from
// them.
func (s *Server) channelForAdmin(w http.ResponseWriter, r *http.Request) (store.Channel, store.Role, bool) {
	channel, role, ok := s.channelForMember(w, r)
	if !ok {
		return store.Channel{}, "", false
	}
	if !role.CanAdminister() {
		s.writeError(w, http.StatusForbidden, "only an owner can do this")
		return store.Channel{}, "", false
	}
	return channel, role, true
}

func toChannelPayload(channel store.Channel, role store.Role) channelPayload {
	return channelPayload{
		ID:          channel.ID,
		Slug:        channel.Slug,
		Name:        channel.Name,
		Description: channel.Description,
		Role:        string(role),
	}
}

// iconTypes are the pictures a producer may wear.
//
// The list is closed and the type is taken from the bytes rather than from
// what the uploader claimed: these are served back to devices, and a caller
// able to choose the Content-Type of what the server returns is a caller able
// to serve HTML from it.
var iconTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/webp": true,
}

// handleSetTokenIcon stores the picture shown beside what a producer sends.
// An empty body clears it.
func (s *Server) handleSetTokenIcon(w http.ResponseWriter, r *http.Request) {
	channel, _, ok := s.channelForAdmin(w, r)
	if !ok {
		return
	}
	tokenID, err := strconv.ParseInt(r.PathValue("tokenID"), 10, 64)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "unknown token")
		return
	}

	// One byte past the limit is read on purpose, so an oversized upload is
	// refused rather than silently truncated to something that still decodes.
	data, err := io.ReadAll(io.LimitReader(r.Body, store.MaxIconBytes+1))
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "unreadable body")
		return
	}
	if len(data) > store.MaxIconBytes {
		s.writeError(w, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("an icon may not exceed %d bytes", store.MaxIconBytes))
		return
	}

	mime := ""
	if len(data) > 0 {
		mime = strings.SplitN(http.DetectContentType(data), ";", 2)[0]
		if !iconTypes[mime] {
			s.writeError(w, http.StatusUnsupportedMediaType,
				"an icon must be a PNG, a JPEG or a WebP")
			return
		}
	}

	if err := s.store.SetPublishTokenIcon(channel.ID, tokenID, data, mime); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.writeError(w, http.StatusNotFound, "unknown token")
			return
		}
		s.logger.Error("storing the icon", "error", err, "token_id", tokenID)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	s.logger.Info("producer icon set", "channel", channel.Slug,
		"token_id", tokenID, "bytes", len(data), "type", mime)
	w.WriteHeader(http.StatusNoContent)
}

// handleTokenIcon serves a producer's picture to any signed-in device.
//
// Not gated on channel membership: the icon is a logo, it says nothing the
// message beside it does not already say, and gating it would mean a second
// lookup on every row of a feed.
func (s *Server) handleTokenIcon(w http.ResponseWriter, r *http.Request) {
	tokenID, err := strconv.ParseInt(r.PathValue("tokenID"), 10, 64)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "unknown token")
		return
	}

	data, mime, err := s.store.PublishTokenIcon(tokenID)
	if errors.Is(err, store.ErrNotFound) {
		s.writeError(w, http.StatusNotFound, "no icon")
		return
	}
	if err != nil {
		s.logger.Error("reading the icon", "error", err, "token_id", tokenID)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Tagged by the bytes themselves, so a changed icon is fetched again and
	// an unchanged one is not — which is what lets a client cache it for a
	// day without going stale the moment someone replaces it.
	sum := sha256.Sum256(data)
	etag := `"` + hex.EncodeToString(sum[:8]) + `"`
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.Header().Set("Content-Type", mime)
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
