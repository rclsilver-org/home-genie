package api

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/rclsilver-org/home-genie/server/internal/store"
)

// shareChannel adds a second member so read state can be compared between
// two people — the whole point of the shared-stream model.
func shareChannel(t *testing.T, s *store.Store, channelID int64, username string, role store.Role) store.User {
	t.Helper()
	user, err := s.UserByUsername(username)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetMember(channelID, user.ID, role); err != nil {
		t.Fatal(err)
	}
	return user
}

// The defining behaviour: one member reading leaves the message unread for
// everybody else.
func TestReadingIsPerUser(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	withLocalAccount(t, repository, "claire", testPassword)
	channel, publishToken := issueChannelAndToken(t, repository, "mediacenter")
	shareChannel(t, repository, channel.ID, "claire", store.RoleReader)

	publish(t, server, "mediacenter", publishToken, "a film", map[string]string{"Title": "Sonarr"})

	thomasToken := session(t, server, "thomas", testPassword)
	claireToken := session(t, server, "claire", testPassword)

	// Both see one unread.
	for name, token := range map[string]string{"thomas": thomasToken, "claire": claireToken} {
		list := decode[[]channelPayload](t, call(t, server, http.MethodGet, "/api/v1/channels", token, nil))
		if len(list) != 1 || list[0].Unread != 1 {
			t.Fatalf("%s: unread = %+v", name, list)
		}
	}

	// Thomas lit.
	messages := decode[[]messagePayload](t, call(t, server, http.MethodGet,
		fmt.Sprintf("/api/v1/channels/%d/messages", channel.ID), thomasToken, nil))
	if r := call(t, server, http.MethodPost,
		fmt.Sprintf("/api/v1/messages/%d/read", messages[0].ID), thomasToken, nil); r.Code != http.StatusNoContent {
		t.Fatalf("marking as read: %d %s", r.Code, r.Body)
	}

	// He has nothing left; Claire is untouched.
	mine := decode[[]channelPayload](t, call(t, server, http.MethodGet, "/api/v1/channels", thomasToken, nil))
	if mine[0].Unread != 0 {
		t.Fatalf("thomas: unread = %d, want 0", mine[0].Unread)
	}
	hers := decode[[]channelPayload](t, call(t, server, http.MethodGet, "/api/v1/channels", claireToken, nil))
	if hers[0].Unread != 1 {
		t.Fatalf("claire: unread = %d, want 1 — thomas's read spilled over", hers[0].Unread)
	}

	// And the feed reflects it message by message.
	hersMessages := decode[[]messagePayload](t, call(t, server, http.MethodGet,
		fmt.Sprintf("/api/v1/channels/%d/messages", channel.ID), claireToken, nil))
	if hersMessages[0].Read {
		t.Fatal("claire sees the message as read")
	}
}

func TestMarkReadIsIdempotent(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	channel, publishToken := issueChannelAndToken(t, repository, "mediacenter")
	publish(t, server, "mediacenter", publishToken, "a film", nil)

	token := session(t, server, "thomas", testPassword)
	messages := decode[[]messagePayload](t, call(t, server, http.MethodGet,
		fmt.Sprintf("/api/v1/channels/%d/messages", channel.ID), token, nil))
	path := fmt.Sprintf("/api/v1/messages/%d/read", messages[0].ID)

	call(t, server, http.MethodPost, path, token, nil)
	call(t, server, http.MethodPost, path, token, nil)

	timeline := decode[[]timelineEntry](t, call(t, server, http.MethodGet,
		fmt.Sprintf("/api/v1/messages/%d/timeline", messages[0].ID), token, nil))

	reads := 0
	for _, entry := range timeline {
		if entry.Kind == store.MessageRead {
			reads++
		}
	}
	if reads != 1 {
		t.Fatalf("%d \"read\" entries for two calls, want 1", reads)
	}
}

func TestMarkChannelReadUpTo(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	channel, publishToken := issueChannelAndToken(t, repository, "mediacenter")
	for i := 0; i < 4; i++ {
		publish(t, server, "mediacenter", publishToken, fmt.Sprintf("m%d", i), nil)
	}

	token := session(t, server, "thomas", testPassword)
	messages := decode[[]messagePayload](t, call(t, server, http.MethodGet,
		fmt.Sprintf("/api/v1/channels/%d/messages", channel.ID), token, nil))
	// messages[0] is the most recent; mark up to the second oldest.
	upto := messages[2].ID

	result := decode[map[string]int](t, call(t, server, http.MethodPost,
		fmt.Sprintf("/api/v1/channels/%d/read?upto_id=%d", channel.ID, upto), token, nil))
	if result["marked"] != 2 {
		t.Fatalf("marked = %d, want 2", result["marked"])
	}

	list := decode[[]channelPayload](t, call(t, server, http.MethodGet, "/api/v1/channels", token, nil))
	if list[0].Unread != 2 {
		t.Fatalf("unread = %d, want 2", list[0].Unread)
	}

	// With no bound, everything becomes read.
	call(t, server, http.MethodPost, fmt.Sprintf("/api/v1/channels/%d/read", channel.ID), token, nil)
	list = decode[[]channelPayload](t, call(t, server, http.MethodGet, "/api/v1/channels", token, nil))
	if list[0].Unread != 0 {
		t.Fatalf("unread = %d, want 0", list[0].Unread)
	}
}

// The timeline must show the queueing even for a recipient with no socket:
// "queued with no sent" is precisely the miss being looked for.
func TestTimelineRecordsQueueingForEveryRecipient(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	withLocalAccount(t, repository, "claire", testPassword)
	channel, publishToken := issueChannelAndToken(t, repository, "mediacenter")
	shareChannel(t, repository, channel.ID, "claire", store.RoleReader)

	publish(t, server, "mediacenter", publishToken, "a film", nil)

	token := session(t, server, "thomas", testPassword)
	messages := decode[[]messagePayload](t, call(t, server, http.MethodGet,
		fmt.Sprintf("/api/v1/channels/%d/messages", channel.ID), token, nil))
	timeline := decode[[]timelineEntry](t, call(t, server, http.MethodGet,
		fmt.Sprintf("/api/v1/messages/%d/timeline", messages[0].ID), token, nil))

	queued := map[string]bool{}
	for _, entry := range timeline {
		if entry.Kind == store.MessageQueued {
			queued[entry.Username] = true
		}
	}
	if !queued["thomas"] || !queued["claire"] {
		t.Fatalf("queueing missing for one recipient: %+v", timeline)
	}
}

func TestTimelineRequiresMembership(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	withLocalAccount(t, repository, "other", testPassword)
	channel, publishToken := issueChannelAndToken(t, repository, "mediacenter")
	publish(t, server, "mediacenter", publishToken, "a film", nil)

	owner := session(t, server, "thomas", testPassword)
	messages := decode[[]messagePayload](t, call(t, server, http.MethodGet,
		fmt.Sprintf("/api/v1/channels/%d/messages", channel.ID), owner, nil))

	stranger := session(t, server, "other", testPassword)
	for _, path := range []string{
		fmt.Sprintf("/api/v1/messages/%d/timeline", messages[0].ID),
		fmt.Sprintf("/api/v1/messages/%d/read", messages[0].ID),
	} {
		method := http.MethodGet
		if path[len(path)-4:] == "read" {
			method = http.MethodPost
		}
		if r := call(t, server, method, path, stranger, nil); r.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", path, r.Code)
		}
	}
}

func TestUptoIdMustBeValid(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	channel, _ := issueChannelAndToken(t, repository, "mediacenter")
	token := session(t, server, "thomas", testPassword)

	if r := call(t, server, http.MethodPost,
		fmt.Sprintf("/api/v1/channels/%d/read?upto_id=demain", channel.ID), token, nil); r.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", r.Code)
	}
}
