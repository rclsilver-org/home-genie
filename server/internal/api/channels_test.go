package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rclsilver-org/home-genie/server/internal/auth"
	"github.com/rclsilver-org/home-genie/server/internal/store"
)

// session enrols a device for an existing account and returns its token.
func session(t *testing.T, server *Server, username, password string) string {
	t.Helper()
	recorder := post(t, server, "/api/v1/auth/login", loginRequest{
		Username: username, Password: password,
		DeviceName: username + "-device", Platform: "android",
	})
	if recorder.Code != http.StatusCreated {
		t.Fatalf("login: status %d: %s", recorder.Code, recorder.Body)
	}
	var response loginResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return response.Token
}

// call issues an authenticated request and returns the recorder.
func call(t *testing.T, server *Server, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(encoded)
	} else {
		reader = bytes.NewReader(nil)
	}

	request := httptest.NewRequest(method, path, reader)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	server.Routes().ServeHTTP(recorder, request)
	return recorder
}

func decode[T any](t *testing.T, recorder *httptest.ResponseRecorder) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(recorder.Body.Bytes(), &value); err != nil {
		t.Fatalf("decoding %s: %v", recorder.Body, err)
	}
	return value
}

const testPassword = "a-solid-password"

func TestCreateChannelMakesTheCallerOwner(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	token := session(t, server, "thomas", testPassword)

	recorder := call(t, server, http.MethodPost, "/api/v1/channels", token,
		createChannelRequest{Slug: "alerts-critical", Name: "Critical alerts"})
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body)
	}

	created := decode[channelPayload](t, recorder)
	if created.Role != string(store.RoleOwner) {
		t.Fatalf("role = %q, want owner", created.Role)
	}

	list := decode[[]channelPayload](t, call(t, server, http.MethodGet, "/api/v1/channels", token, nil))
	if len(list) != 1 || list[0].Slug != "alerts-critical" {
		t.Fatalf("list = %+v", list)
	}
}

// A slug that would shadow the API must be refused: the ntfy-compatible
// ingest endpoint lives at the root.
func TestReservedAndMalformedSlugsAreRefused(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	token := session(t, server, "thomas", testPassword)

	for _, slug := range []string{"api", "healthz", "Alerts", "alerts/prod", ""} {
		recorder := call(t, server, http.MethodPost, "/api/v1/channels", token,
			createChannelRequest{Slug: slug})
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("slug %q: status = %d, want 400", slug, recorder.Code)
		}
	}
}

func TestDuplicateSlugIsAConflict(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	token := session(t, server, "thomas", testPassword)

	call(t, server, http.MethodPost, "/api/v1/channels", token,
		createChannelRequest{Slug: "alerts"})
	recorder := call(t, server, http.MethodPost, "/api/v1/channels", token,
		createChannelRequest{Slug: "alerts"})
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", recorder.Code)
	}
}

// A non-member must get 404, never 403: a 403 would confirm the channel
// exists to somebody who has no business knowing.
func TestANonMemberSeesA404NotA403(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	withLocalAccount(t, repository, "other", testPassword)

	ownerToken := session(t, server, "thomas", testPassword)
	strangerToken := session(t, server, "other", testPassword)

	created := decode[channelPayload](t, call(t, server, http.MethodPost, "/api/v1/channels",
		ownerToken, createChannelRequest{Slug: "prive"}))
	path := fmt.Sprintf("/api/v1/channels/%d", created.ID)

	for _, method := range []string{http.MethodGet, http.MethodPatch, http.MethodDelete} {
		var body any
		if method == http.MethodPatch {
			body = updateChannelRequest{}
		}
		recorder := call(t, server, method, path, strangerToken, body)
		if recorder.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", method, recorder.Code)
		}
	}

	// And the channel does not show up in their list either.
	list := decode[[]channelPayload](t, call(t, server, http.MethodGet, "/api/v1/channels",
		strangerToken, nil))
	if len(list) != 0 {
		t.Fatalf("a stranger sees %d channels", len(list))
	}
}

// A member who is not an owner gets 403: the channel's existence is not a
// secret from them, only the administration is.
func TestAReaderCannotAdministerButGetsA403(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	withLocalAccount(t, repository, "reader", testPassword)

	ownerToken := session(t, server, "thomas", testPassword)
	created := decode[channelPayload](t, call(t, server, http.MethodPost, "/api/v1/channels",
		ownerToken, createChannelRequest{Slug: "alerts"}))

	if r := call(t, server, http.MethodPut,
		fmt.Sprintf("/api/v1/channels/%d/members/reader", created.ID),
		ownerToken, setMemberRequest{Role: "reader"}); r.Code != http.StatusOK {
		t.Fatalf("adding the reader: %d %s", r.Code, r.Body)
	}

	readerToken := session(t, server, "reader", testPassword)

	// They can read.
	if r := call(t, server, http.MethodGet,
		fmt.Sprintf("/api/v1/channels/%d", created.ID), readerToken, nil); r.Code != http.StatusOK {
		t.Fatalf("a reader cannot read the channel: %d", r.Code)
	}

	// They cannot administer.
	forbidden := map[string]string{
		http.MethodDelete: fmt.Sprintf("/api/v1/channels/%d", created.ID),
		http.MethodGet:    fmt.Sprintf("/api/v1/channels/%d/tokens", created.ID),
		http.MethodPost:   fmt.Sprintf("/api/v1/channels/%d/tokens", created.ID),
	}
	for method, path := range forbidden {
		if r := call(t, server, method, path, readerToken, createTokenRequest{Name: "x"}); r.Code != http.StatusForbidden {
			t.Errorf("%s %s: status = %d, want 403", method, path, r.Code)
		}
	}
}

func TestRemovingTheLastOwnerIsAConflict(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	token := session(t, server, "thomas", testPassword)

	created := decode[channelPayload](t, call(t, server, http.MethodPost, "/api/v1/channels",
		token, createChannelRequest{Slug: "alerts"}))

	recorder := call(t, server, http.MethodDelete,
		fmt.Sprintf("/api/v1/channels/%d/members/thomas", created.ID), token, nil)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", recorder.Code, recorder.Body)
	}
}

func TestMuteIsSetThenCleared(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	token := session(t, server, "thomas", testPassword)

	created := decode[channelPayload](t, call(t, server, http.MethodPost, "/api/v1/channels",
		token, createChannelRequest{Slug: "alerts"}))
	path := fmt.Sprintf("/api/v1/channels/%d", created.ID)

	until := "2026-12-31T23:00:00Z"
	updated := decode[channelPayload](t, call(t, server, http.MethodPatch, path, token,
		updateChannelRequest{MutedUntil: &until}))
	if updated.MutedUntil == nil {
		t.Fatal("the mute was not applied")
	}

	empty := ""
	cleared := decode[channelPayload](t, call(t, server, http.MethodPatch, path, token,
		updateChannelRequest{MutedUntil: &empty}))
	if cleared.MutedUntil != nil {
		t.Fatal("the mute was not cleared")
	}

	// A malformed instant is a 400, not a silently ignored field.
	bad := "demain"
	if r := call(t, server, http.MethodPatch, path, token,
		updateChannelRequest{MutedUntil: &bad}); r.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", r.Code)
	}
}

func TestPublishTokenIsShownOnceThenRevocable(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	token := session(t, server, "thomas", testPassword)

	created := decode[channelPayload](t, call(t, server, http.MethodPost, "/api/v1/channels",
		token, createChannelRequest{Slug: "mediacenter"}))
	tokensPath := fmt.Sprintf("/api/v1/channels/%d/tokens", created.ID)

	issued := decode[publishTokenPayload](t, call(t, server, http.MethodPost, tokensPath,
		token, createTokenRequest{Name: "sonarr"}))
	if issued.Token == "" {
		t.Fatal("no clear token was returned")
	}
	if _, _, err := repository.PublishTokenByHash(issued.Token); err == nil {
		t.Fatal("the token resolves in clear, so it was stored unhashed")
	}
	if _, _, err := repository.PublishTokenByHash(auth.HashToken(issued.Token)); err != nil {
		t.Fatalf("the token does not resolve through its hash: %v", err)
	}

	// The listing never shows it again.
	listed := decode[[]publishTokenPayload](t, call(t, server, http.MethodGet, tokensPath, token, nil))
	if len(listed) != 1 {
		t.Fatalf("%d tokens listed", len(listed))
	}
	if listed[0].Token != "" {
		t.Fatal("the listing returns the token in clear")
	}

	// Revocation is immediate.
	if r := call(t, server, http.MethodDelete,
		fmt.Sprintf("%s/%d", tokensPath, issued.ID), token, nil); r.Code != http.StatusNoContent {
		t.Fatalf("revocation: status = %d", r.Code)
	}
	if _, _, err := repository.PublishTokenByHash(auth.HashToken(issued.Token)); err == nil {
		t.Fatal("the revoked token still resolves")
	}
}

func TestTokenRequiresAName(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	token := session(t, server, "thomas", testPassword)

	created := decode[channelPayload](t, call(t, server, http.MethodPost, "/api/v1/channels",
		token, createChannelRequest{Slug: "mediacenter"}))

	r := call(t, server, http.MethodPost,
		fmt.Sprintf("/api/v1/channels/%d/tokens", created.ID), token, createTokenRequest{Name: "  "})
	if r.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", r.Code)
	}
}
