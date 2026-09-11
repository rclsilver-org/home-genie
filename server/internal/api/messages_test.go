package api

import (
	"net/http"
	"testing"

	"github.com/rclsilver-org/home-genie/server/internal/store"
)

// The notifications feed crosses the channels, and carries only what lives
// by "read / unread": alerts have a console of their own, and their reminders
// would flood a view whose whole point is to be emptied.
func TestFeedCrossesChannelsAndExcludesAlerts(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	_, mediacenter := issueChannelAndToken(t, repository, "mediacenter")
	_, veille := issueChannelAndToken(t, repository, "veille")
	_, alerting := issueChannelAndToken(t, repository, "alerts-critical")

	publish(t, server, "mediacenter", mediacenter, "a film", map[string]string{"Title": "Sonarr"})
	publish(t, server, "veille", veille, "an image", map[string]string{"Title": "diun"})
	webhook(t, server, "alerts-critical", alerting, alertmanagerWebhook{
		Version: "4", Alerts: []alertmanagerAlert{firing("a", "critical")}})

	token := session(t, server, "thomas", testPassword)
	feed := decode[[]messagePayload](t, call(t, server, http.MethodGet,
		"/api/v1/messages", token, nil))
	if len(feed) != 2 {
		t.Fatalf("want two notifications, got %+v", feed)
	}
	slugs := map[string]bool{feed[0].ChannelSlug: true, feed[1].ChannelSlug: true}
	if !slugs["mediacenter"] || !slugs["veille"] {
		t.Fatalf("both channels should be present: %+v", feed)
	}
	for _, message := range feed {
		if message.AlertID != nil {
			t.Fatalf("an alert slipped into the feed: %+v", message)
		}
	}
}

// "Mark everything read" empties the view, and for that user alone.
func TestMarkFeedReadIsPersonal(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	withLocalAccount(t, repository, "claire", testPassword)
	channel, publishToken := issueChannelAndToken(t, repository, "mediacenter")
	shareChannel(t, repository, channel.ID, "claire", store.RoleReader)

	publish(t, server, "mediacenter", publishToken, "a film", map[string]string{"Title": "Sonarr"})
	publish(t, server, "mediacenter", publishToken, "another one", map[string]string{"Title": "Sonarr"})

	thomasToken := session(t, server, "thomas", testPassword)
	claireToken := session(t, server, "claire", testPassword)

	response := call(t, server, http.MethodPost, "/api/v1/messages/read", thomasToken, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("code %d: %s", response.Code, response.Body)
	}

	mine := decode[[]messagePayload](t, call(t, server, http.MethodGet,
		"/api/v1/messages?unread=1", thomasToken, nil))
	if len(mine) != 0 {
		t.Fatalf("thomas's feed should be empty: %+v", mine)
	}
	hers := decode[[]messagePayload](t, call(t, server, http.MethodGet,
		"/api/v1/messages?unread=1", claireToken, nil))
	if len(hers) != 2 {
		t.Fatalf("claire still has her two unread ones, got %+v", hers)
	}
}

// The badge counter counts only what the view shows: neither the alerts, nor
// what other people have not read.
func TestUnreadFeedCountIgnoresAlertsAndOtherPeople(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	withLocalAccount(t, repository, "claire", testPassword)
	channel, publishToken := issueChannelAndToken(t, repository, "mediacenter")
	shareChannel(t, repository, channel.ID, "claire", store.RoleReader)
	_, alerting := issueChannelAndToken(t, repository, "alerts-critical")

	publish(t, server, "mediacenter", publishToken, "a film", map[string]string{"Title": "Sonarr"})
	publish(t, server, "mediacenter", publishToken, "another one", map[string]string{"Title": "Sonarr"})
	webhook(t, server, "alerts-critical", alerting, alertmanagerWebhook{
		Version: "4", Alerts: []alertmanagerAlert{firing("a", "critical")}})

	thomasToken := session(t, server, "thomas", testPassword)
	count := decode[map[string]int](t, call(t, server, http.MethodGet,
		"/api/v1/messages/unread", thomasToken, nil))
	if count["count"] != 2 {
		t.Fatalf("two unread expected, got %+v", count)
	}

	call(t, server, http.MethodPost, "/api/v1/messages/read", thomasToken, nil)

	count = decode[map[string]int](t, call(t, server, http.MethodGet,
		"/api/v1/messages/unread", thomasToken, nil))
	if count["count"] != 0 {
		t.Fatalf("the view should be empty: %+v", count)
	}
	claireToken := session(t, server, "claire", testPassword)
	hers := decode[map[string]int](t, call(t, server, http.MethodGet,
		"/api/v1/messages/unread", claireToken, nil))
	if hers["count"] != 2 {
		t.Fatalf("claire keeps her two unread ones: %+v", hers)
	}
}
