package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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
	// The label rides in front and only the secret is hashed, so the whole
	// string no longer resolves — that is the point of the format.
	if !strings.HasPrefix(issued.Token, "sonarr:") {
		t.Fatalf("the token does not name its producer: %q", issued.Token)
	}
	if _, _, err := repository.PublishTokenByHash(
		auth.HashToken(auth.SecretOf(issued.Token)),
	); err != nil {
		t.Fatalf("the token does not resolve through its secret: %v", err)
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

// The same gesture, twice: revoke then erase. A revoked token one cannot
// remove clutters the list forever.
func TestARevokedTokenCanThenBeDeleted(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	channel, _ := issueChannelAndToken(t, repository, "alerts")
	token := session(t, server, "thomas", testPassword)

	issued := decode[publishTokenPayload](t, call(t, server, http.MethodPost,
		fmt.Sprintf("/api/v1/channels/%d/tokens", channel.ID), token,
		createTokenRequest{Name: "sonarr"}))
	path := fmt.Sprintf("/api/v1/channels/%d/tokens/%d", channel.ID, issued.ID)

	if r := call(t, server, http.MethodDelete, path, token, nil); r.Code != http.StatusNoContent {
		t.Fatalf("revocation: %d %s", r.Code, r.Body)
	}
	// Revoked but still listed: that is the trace of what was cut off.
	listed := decode[[]publishTokenPayload](t, call(t, server, http.MethodGet,
		fmt.Sprintf("/api/v1/channels/%d/tokens", channel.ID), token, nil))
	if len(listed) != 2 {
		t.Fatalf("the revoked token should stay listed: %+v", listed)
	}

	if r := call(t, server, http.MethodDelete, path, token, nil); r.Code != http.StatusNoContent {
		t.Fatalf("deletion: %d %s", r.Code, r.Body)
	}
	listed = decode[[]publishTokenPayload](t, call(t, server, http.MethodGet,
		fmt.Sprintf("/api/v1/channels/%d/tokens", channel.ID), token, nil))
	if len(listed) != 1 {
		t.Fatalf("the token should be gone: %+v", listed)
	}

	// A third call finds nothing any more.
	if r := call(t, server, http.MethodDelete, path, token, nil); r.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", r.Code)
	}
}

// Completion is there to pick a member rather than spell one: it matches on
// the username as well as on the display name.
func TestUserSearchMatchesBothNames(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	if _, err := repository.CreateLocalUser("claire", "Claire Dupont", "x", false); err != nil {
		t.Fatal(err)
	}
	// Completion serves sharing a channel: it requires administering one, or
	// a reader invited to a feed could enumerate every account.
	issueChannelAndToken(t, repository, "alerts")
	token := session(t, server, "thomas", testPassword)

	byUsername := decode[[]userSuggestionPayload](t, call(t, server, http.MethodGet,
		"/api/v1/users?q=clai", token, nil))
	if len(byUsername) != 1 || byUsername[0].Username != "claire" {
		t.Fatalf("search by username: %+v", byUsername)
	}

	byDisplayName := decode[[]userSuggestionPayload](t, call(t, server, http.MethodGet,
		"/api/v1/users?q=Dupont", token, nil))
	if len(byDisplayName) != 1 || byDisplayName[0].Username != "claire" {
		t.Fatalf("search by display name: %+v", byDisplayName)
	}

	// With no fragment, everybody: that is the list one scrolls before typing
	// anything at all.
	all := decode[[]userSuggestionPayload](t, call(t, server, http.MethodGet,
		"/api/v1/users", token, nil))
	if len(all) != 2 {
		t.Fatalf("two accounts expected: %+v", all)
	}
}

// A 1×1 PNG, small enough to sit in a test and real enough to be sniffed.
var tinyPNG = []byte{
	0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R',
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
	0x89, 0x00, 0x00, 0x00, 0x0a, 'I', 'D', 'A', 'T',
	0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00, 0x05,
	0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00,
	0x00, 0x00, 'I', 'E', 'N', 'D', 0xae, 0x42, 0x60, 0x82,
}

func raw(t *testing.T, server *Server, method, path, token, contentType string,
	body []byte) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+token)
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	recorder := httptest.NewRecorder()
	server.Routes().ServeHTTP(recorder, request)
	return recorder
}

// The whole point of the feature: a message says which producer sent it, and
// the feed can put its face beside it.
func TestAMessageNamesTheProducerThatSentIt(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	session := session(t, server, "thomas", testPassword)
	channel, publishToken := issueChannelAndToken(t, repository, "mediacenter")

	if r := publish(t, server, "mediacenter", publishToken, "downloaded", nil); r.Code != http.StatusOK {
		t.Fatalf("publishing: %d %s", r.Code, r.Body)
	}

	feed := decode[[]messagePayload](t, call(t, server, http.MethodGet, "/api/v1/messages", session, nil))
	if len(feed) != 1 {
		t.Fatalf("%d messages", len(feed))
	}
	if feed[0].Producer == "" {
		t.Fatal("the message does not name its producer")
	}
	if feed[0].ProducerIcon {
		t.Fatal("a producer with no picture should not claim one")
	}
	_ = channel
}

func TestAProducerIconIsStoredAndServedBack(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	session := session(t, server, "thomas", testPassword)

	created := decode[channelPayload](t, call(t, server, http.MethodPost, "/api/v1/channels",
		session, createChannelRequest{Slug: "mediacenter"}))
	issued := decode[publishTokenPayload](t, call(t, server, http.MethodPost,
		fmt.Sprintf("/api/v1/channels/%d/tokens", created.ID), session,
		createTokenRequest{Name: "sonarr"}))

	iconPath := fmt.Sprintf("/api/v1/channels/%d/tokens/%d/icon", created.ID, issued.ID)
	if r := raw(t, server, http.MethodPut, iconPath, session, "image/png", tinyPNG); r.Code != http.StatusNoContent {
		t.Fatalf("uploading: %d %s", r.Code, r.Body)
	}

	servePath := fmt.Sprintf("/api/v1/tokens/%d/icon", issued.ID)
	served := call(t, server, http.MethodGet, servePath, session, nil)
	if served.Code != http.StatusOK {
		t.Fatalf("serving: %d %s", served.Code, served.Body)
	}
	if got := served.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("content type = %q", got)
	}
	if !bytes.Equal(served.Body.Bytes(), tinyPNG) {
		t.Fatal("the bytes came back changed")
	}

	// Tagged by its content, so a client that already has it is told so.
	etag := served.Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag")
	}
	again := httptest.NewRequest(http.MethodGet, servePath, nil)
	again.Header.Set("Authorization", "Bearer "+session)
	again.Header.Set("If-None-Match", etag)
	recorder := httptest.NewRecorder()
	server.Routes().ServeHTTP(recorder, again)
	if recorder.Code != http.StatusNotModified {
		t.Fatalf("a matching ETag returned %d, want 304", recorder.Code)
	}
}

// The type is taken from the bytes, never from what the uploader claimed:
// these are served back, and choosing the Content-Type of a server's response
// is how one serves HTML from someone else's origin.
func TestAnIconIsRefusedUnlessItsBytesAreAnImage(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	session := session(t, server, "thomas", testPassword)

	created := decode[channelPayload](t, call(t, server, http.MethodPost, "/api/v1/channels",
		session, createChannelRequest{Slug: "mediacenter"}))
	issued := decode[publishTokenPayload](t, call(t, server, http.MethodPost,
		fmt.Sprintf("/api/v1/channels/%d/tokens", created.ID), session,
		createTokenRequest{Name: "sonarr"}))
	iconPath := fmt.Sprintf("/api/v1/channels/%d/tokens/%d/icon", created.ID, issued.ID)

	html := []byte("<html><script>alert(1)</script></html>")
	if r := raw(t, server, http.MethodPut, iconPath, session, "image/png", html); r.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("HTML claiming to be a PNG returned %d, want 415", r.Code)
	}

	oversized := make([]byte, store.MaxIconBytes+1)
	copy(oversized, tinyPNG)
	if r := raw(t, server, http.MethodPut, iconPath, session, "image/png", oversized); r.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("an oversized icon returned %d, want 413", r.Code)
	}
}

// The administration screen decides whether to fetch a picture from this,
// so a token that has one must say so and one that has not must not.
func TestATokenListingSaysWhichProducersHaveAPicture(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	session := session(t, server, "thomas", testPassword)

	created := decode[channelPayload](t, call(t, server, http.MethodPost, "/api/v1/channels",
		session, createChannelRequest{Slug: "mediacenter"}))
	tokensPath := fmt.Sprintf("/api/v1/channels/%d/tokens", created.ID)
	withIcon := decode[publishTokenPayload](t, call(t, server, http.MethodPost, tokensPath,
		session, createTokenRequest{Name: "sonarr"}))
	decode[publishTokenPayload](t, call(t, server, http.MethodPost, tokensPath,
		session, createTokenRequest{Name: "radarr"}))

	iconPath := fmt.Sprintf("%s/%d/icon", tokensPath, withIcon.ID)
	if r := raw(t, server, http.MethodPut, iconPath, session, "image/png", tinyPNG); r.Code != http.StatusNoContent {
		t.Fatalf("uploading: %d %s", r.Code, r.Body)
	}

	listed := decode[[]publishTokenPayload](t, call(t, server, http.MethodGet, tokensPath, session, nil))
	for _, entry := range listed {
		want := entry.Name == "sonarr"
		if entry.HasIcon != want {
			t.Errorf("%s: has_icon = %v, want %v", entry.Name, entry.HasIcon, want)
		}
	}

	// Clearing it takes the flag back down: an empty body is how one removes.
	if r := raw(t, server, http.MethodPut, iconPath, session, "", nil); r.Code != http.StatusNoContent {
		t.Fatalf("clearing: %d %s", r.Code, r.Body)
	}
	for _, entry := range decode[[]publishTokenPayload](t,
		call(t, server, http.MethodGet, tokensPath, session, nil)) {
		if entry.HasIcon {
			t.Errorf("%s still claims a picture after it was cleared", entry.Name)
		}
	}
}
