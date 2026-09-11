package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rclsilver-org/home-genie/server/internal/store"
)

// publish posts to the ntfy-compatible endpoint exactly as a producer would.
func publish(t *testing.T, server *Server, slug, token, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/"+slug, bytes.NewReader([]byte(body)))
	request.Header.Set("Authorization", "Bearer "+token)
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	recorder := httptest.NewRecorder()
	server.Routes().ServeHTTP(recorder, request)
	return recorder
}

func lastMessage(t *testing.T, s *store.Store, channelID int64) store.Message {
	t.Helper()
	messages, err := s.MessagesOf(store.MessageQuery{ChannelID: channelID, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) == 0 {
		t.Fatal("no message was recorded")
	}
	return messages[0]
}

// The whole point of the compatibility layer: the plain-text body plus
// headers form that *arr and diun already emit.
func TestNtfyTextBodyWithHeaders(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	channel, token := issueChannelAndToken(t, repository, "mediacenter")

	recorder := publish(t, server, "mediacenter", token, "The film is downloaded", map[string]string{
		"Title":    "Sonarr",
		"Priority": "high",
		"Tags":     "movie, download",
		"Click":    "https://sonarr.example/series/1",
	})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body)
	}

	message := lastMessage(t, repository, channel.ID)
	if message.Title != "Sonarr" || message.Body != "The film is downloaded" {
		t.Fatalf("message = %+v", message)
	}
	if message.Priority != store.PriorityHigh {
		t.Fatalf("priority = %d, want %d", message.Priority, store.PriorityHigh)
	}
	if len(message.Tags) != 2 || message.Tags[0] != "movie" || message.Tags[1] != "download" {
		t.Fatalf("tags = %v", message.Tags)
	}
	if message.ClickURL == "" {
		t.Fatal("the click URL was lost")
	}
}

// ntfy accepts several spellings for the same header; producers use them
// interchangeably.
func TestNtfyHeaderAliases(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	channel, token := issueChannelAndToken(t, repository, "mediacenter")

	cases := []struct {
		header string
		value  string
	}{
		{"X-Title", "via X-Title"},
		{"Title", "via Title"},
		{"t", "via t"},
	}
	for _, c := range cases {
		if r := publish(t, server, "mediacenter", token, "corps",
			map[string]string{c.header: c.value}); r.Code != http.StatusOK {
			t.Fatalf("%s: status %d", c.header, r.Code)
		}
		if got := lastMessage(t, repository, channel.ID).Title; got != c.value {
			t.Errorf("%s: title = %q, want %q", c.header, got, c.value)
		}
	}
}

func TestNtfyPriorityNamesAndNumbers(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	channel, token := issueChannelAndToken(t, repository, "alerts")

	cases := map[string]int{
		"min": store.PriorityMin, "low": store.PriorityLow,
		"default": store.PriorityDefault, "high": store.PriorityHigh,
		"max": store.PriorityMax, "urgent": store.PriorityMax,
		"1": store.PriorityMin, "5": store.PriorityMax,
		// Out of range or unreadable: fall back to the default rather than
		// refuse the notification.
		"9": store.PriorityDefault, "anything at all": store.PriorityDefault,
	}
	for raw, want := range cases {
		publish(t, server, "alerts", token, "corps", map[string]string{"Priority": raw})
		if got := lastMessage(t, repository, channel.ID).Priority; got != want {
			t.Errorf("Priority %q -> %d, want %d", raw, got, want)
		}
	}
}

func TestNtfyJSONBody(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	channel, token := issueChannelAndToken(t, repository, "mediacenter")

	body := `{"title":"Diun","message":"new image","priority":4,"tags":["docker"],"click":"https://x"}`
	recorder := publish(t, server, "mediacenter", token, body,
		map[string]string{"Content-Type": "application/json"})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body)
	}

	message := lastMessage(t, repository, channel.ID)
	if message.Title != "Diun" || message.Body != "new image" ||
		message.Priority != store.PriorityHigh || len(message.Tags) != 1 {
		t.Fatalf("message = %+v", message)
	}
}

// A producer misconfigured with someone else's topic must fail loudly, not
// publish to the channel its token happens to own.
func TestJSONTopicMismatchIsRefused(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	issueChannelAndToken(t, repository, "mediacenter")
	_, alertsToken := issueChannelAndToken(t, repository, "alerts")

	recorder := publish(t, server, "alerts", alertsToken,
		`{"topic":"mediacenter","message":"lost"}`,
		map[string]string{"Content-Type": "application/json"})
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestEmptyMessageIsRefused(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	issueChannelAndToken(t, repository, "alerts")
	_, token := issueChannelAndToken(t, repository, "vide")

	if r := publish(t, server, "vide", token, "   ", nil); r.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", r.Code)
	}
}

// The ingest route sits at the root; it must not have swallowed the API.
func TestIngestDoesNotShadowTheAPI(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)

	// Without a token, /api/v1/auth/login must still be the login endpoint
	// and not be treated as a publish to a channel called "api".
	recorder := post(t, server, "/api/v1/auth/login", loginRequest{
		Username: "thomas", Password: testPassword, DeviceName: "phone",
	})
	if recorder.Code != http.StatusCreated {
		t.Fatalf("the login endpoint was shadowed: status %d, %s", recorder.Code, recorder.Body)
	}
}

// A published message reaches the members' sockets, which is what makes the
// notification appear on the phone.
func TestPublishingProducesAnEvent(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	channel, token := issueChannelAndToken(t, repository, "mediacenter")

	owner, _ := repository.UserByUsername("thomas")
	before, _ := repository.LatestSeq(owner.ID)

	publish(t, server, "mediacenter", token, "a film", map[string]string{"Title": "Sonarr"})

	events, err := repository.EventsSince(owner.ID, before, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != eventMessageNew {
		t.Fatalf("events = %+v", events)
	}

	var payload messagePayload
	if err := json.Unmarshal(events[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.ChannelSlug != channel.Slug || payload.Title != "Sonarr" {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestListMessagesRequiresMembership(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	withLocalAccount(t, repository, "other", testPassword)
	channel, token := issueChannelAndToken(t, repository, "mediacenter")
	publish(t, server, "mediacenter", token, "a film", nil)

	strangerToken := session(t, server, "other", testPassword)
	path := fmt.Sprintf("/api/v1/channels/%d/messages", channel.ID)

	if r := call(t, server, http.MethodGet, path, strangerToken, nil); r.Code != http.StatusNotFound {
		t.Fatalf("a stranger read the messages: status %d", r.Code)
	}

	ownerToken := session(t, server, "thomas", testPassword)
	listed := decode[[]messagePayload](t, call(t, server, http.MethodGet, path, ownerToken, nil))
	if len(listed) != 1 || listed[0].Body != "a film" {
		t.Fatalf("listed = %+v", listed)
	}
}

// Paging backwards is how the app loads history without holding it all.
func TestMessagePaging(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	channel, token := issueChannelAndToken(t, repository, "mediacenter")
	for i := 0; i < 5; i++ {
		publish(t, server, "mediacenter", token, fmt.Sprintf("message %d", i), nil)
	}

	ownerToken := session(t, server, "thomas", testPassword)
	base := fmt.Sprintf("/api/v1/channels/%d/messages", channel.ID)

	first := decode[[]messagePayload](t, call(t, server, http.MethodGet, base+"?limit=2", ownerToken, nil))
	if len(first) != 2 || first[0].Body != "message 4" {
		t.Fatalf("first page = %+v", first)
	}

	next := decode[[]messagePayload](t, call(t, server, http.MethodGet,
		fmt.Sprintf("%s?limit=2&before_id=%d", base, first[1].ID), ownerToken, nil))
	if len(next) != 2 || next[0].Body != "message 2" {
		t.Fatalf("second page = %+v", next)
	}
}
