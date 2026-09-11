package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/rclsilver-org/home-genie/server/internal/store"
)

func TestReminderPolicyIsSetAndListed(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	channel, _ := issueChannelAndToken(t, repository, "alerts-critical")
	token := session(t, server, "thomas", testPassword)
	path := fmt.Sprintf("/api/v1/channels/%d/reminders", channel.ID)

	recorder := call(t, server, http.MethodPut, path, token, reminderPolicyPayload{
		Severity: "critical", IntervalSeconds: 900, Enabled: true,
	})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body)
	}

	listed := decode[[]reminderPolicyPayload](t, call(t, server, http.MethodGet, path, token, nil))
	if len(listed) != 1 {
		t.Fatalf("%d politiques", len(listed))
	}
	if listed[0].Scope != scopeChannel || listed[0].IntervalSeconds != 900 {
		t.Fatalf("politique = %+v", listed[0])
	}
}

// Half a quiet window would silence at an unpredictable hour.
func TestHalfAQuietWindowIsRefused(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	channel, _ := issueChannelAndToken(t, repository, "alerts")
	token := session(t, server, "thomas", testPassword)
	path := fmt.Sprintf("/api/v1/channels/%d/reminders", channel.ID)

	path = fmt.Sprintf("/api/v1/channels/%d/quiet-hours", channel.ID)
	for _, window := range []quietHoursPayload{
		{Severity: "critical", From: "23:00"},
		{Severity: "critical", To: "07:00"},
		{Severity: "critical", From: "minuit", To: "07:00"},
	} {
		if r := call(t, server, http.MethodPut, path, token, window); r.Code != http.StatusBadRequest {
			t.Errorf("%+v: status = %d, want 400", window, r.Code)
		}
	}
}

func TestReminderPolicyValidation(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	channel, _ := issueChannelAndToken(t, repository, "alerts")
	token := session(t, server, "thomas", testPassword)
	path := fmt.Sprintf("/api/v1/channels/%d/reminders", channel.ID)

	for name, policy := range map[string]reminderPolicyPayload{
		"no severity":       {IntervalSeconds: 900, Enabled: true},
		"negative interval": {Severity: "critical", IntervalSeconds: -1, Enabled: true},
	} {
		if r := call(t, server, http.MethodPut, path, token, policy); r.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, r.Code)
		}
	}
}

// Setting a cadence is an administrative act; reading one is not.
func TestOnlyAnOwnerSetsThePolicy(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	withLocalAccount(t, repository, "reader", testPassword)
	channel, _ := issueChannelAndToken(t, repository, "alerts")

	owner := session(t, server, "thomas", testPassword)
	call(t, server, http.MethodPut,
		fmt.Sprintf("/api/v1/channels/%d/members/reader", channel.ID),
		owner, setMemberRequest{Role: "reader"})

	reader := session(t, server, "reader", testPassword)
	path := fmt.Sprintf("/api/v1/channels/%d/reminders", channel.ID)

	if r := call(t, server, http.MethodGet, path, reader, nil); r.Code != http.StatusOK {
		t.Errorf("reading by a reader: status = %d, want 200", r.Code)
	}
	if r := call(t, server, http.MethodPut, path, reader, reminderPolicyPayload{
		Severity: "critical", IntervalSeconds: 60, Enabled: true,
	}); r.Code != http.StatusForbidden {
		t.Errorf("setting by a reader: status = %d, want 403", r.Code)
	}
}

// An alert opening while a policy exists must be scheduled right away;
// with no policy, it stays silent.
func TestOpeningAnAlertArmsTheCadence(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	channel, publishToken := issueChannelAndToken(t, repository, "alerts-critical")
	token := session(t, server, "thomas", testPassword)

	// With no policy: nothing scheduled.
	webhook(t, server, "alerts-critical", publishToken, alertmanagerWebhook{
		Version: "4", Alerts: []alertmanagerAlert{firing("no-policy", "critical")}})

	alerts, err := repository.AlertsOf(store2Query(channel.ID))
	if err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 1 || alerts[0].NextReminderAt != nil {
		t.Fatalf("an alert was scheduled with no policy: %+v", alerts)
	}

	// With a policy: scheduled on opening.
	call(t, server, http.MethodPut,
		fmt.Sprintf("/api/v1/channels/%d/reminders", channel.ID), token,
		reminderPolicyPayload{Severity: "critical", IntervalSeconds: 900, Enabled: true})

	webhook(t, server, "alerts-critical", publishToken, alertmanagerWebhook{
		Version: "4", Alerts: []alertmanagerAlert{firing("with-policy", "critical")}})

	alerts, _ = repository.AlertsOf(store2Query(channel.ID))
	armed := 0
	for _, alert := range alerts {
		if alert.NextReminderAt != nil {
			armed++
		}
	}
	if armed != 1 {
		t.Fatalf("%d alerts scheduled, want 1", armed)
	}
}

func store2Query(channelID int64) store.AlertQuery {
	return store.AlertQuery{ChannelID: channelID}
}

// What the phone receives for a reminder: the alert's title, intact, and the
// rank beside it. The rank inside the title made it truncate on the lock
// screen, which is exactly where it must be read at a glance.
func TestAReminderCarriesItsRankBesideTheTitle(t *testing.T) {
	httpServer, server, token := liveServer(t)
	repository := server.store

	channel := decode[channelPayload](t, call(t, server, http.MethodPost, "/api/v1/channels",
		token, createChannelRequest{Slug: "alerts-critical", Name: "Alertes"}))
	publishToken := decode[publishTokenPayload](t, call(t, server, http.MethodPost,
		fmt.Sprintf("/api/v1/channels/%d/tokens", channel.ID), token,
		createTokenRequest{Name: "alertmanager"}))

	webhook(t, server, "alerts-critical", publishToken.Token, alertmanagerWebhook{
		Version: "4", Alerts: []alertmanagerAlert{firing("a", "critical")}})

	alerts, err := repository.AlertsOf(store.AlertQuery{ChannelID: channel.ID})
	if err != nil || len(alerts) != 1 {
		t.Fatalf("alerts = %+v (%v)", alerts, err)
	}

	conn := dial(t, httpServer.URL, token, 0)
	readUntil(t, conn, frameReady)

	if err := server.RemindAlert(alerts[0], 3); err != nil {
		t.Fatal(err)
	}

	frame := readUntil(t, conn, eventMessageNew)
	var payload messagePayload
	if err := json.Unmarshal(frame.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Title != alerts[0].Title() {
		t.Fatalf("the title should be the alert's: %q", payload.Title)
	}
	if payload.ReminderCount != 3 {
		t.Fatalf("the reminder rank should travel with the message: %+v", payload)
	}
	// The body names the rule and the machine: "laptop" alone does not say
	// what is wrong with it.
	if payload.Body != "DiskFull — nas" {
		t.Fatalf("body = %q", payload.Body)
	}
}

// Quiet hours silence, they do not hold back: the message arrives, but at
// minimum priority, hence without noise.
func TestQuietHoursSilenceANotificationWithoutHoldingIt(t *testing.T) {
	httpServer, server, token := liveServer(t)

	channel := decode[channelPayload](t, call(t, server, http.MethodPost, "/api/v1/channels",
		token, createChannelRequest{Slug: "notifications", Name: "Notifications"}))
	publishToken := decode[publishTokenPayload](t, call(t, server, http.MethodPost,
		fmt.Sprintf("/api/v1/channels/%d/tokens", channel.ID), token,
		createTokenRequest{Name: "sonarr"}))

	// A window covering the whole day, so the test does not depend on the
	// hour it runs at.
	if r := call(t, server, http.MethodPut,
		fmt.Sprintf("/api/v1/channels/%d/quiet-hours", channel.ID), token,
		quietHoursPayload{From: "00:00", To: "23:59"}); r.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", r.Code, r.Body)
	}

	conn := dial(t, httpServer.URL, token, 0)
	readUntil(t, conn, frameReady)

	publish(t, server, "notifications", publishToken.Token, "a movie",
		map[string]string{"Title": "Sonarr", "Priority": "5"})

	frame := readUntil(t, conn, eventMessageNew)
	var payload messagePayload
	if err := json.Unmarshal(frame.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Priority != store.PriorityMin {
		t.Fatalf("the message should arrive silent: priority %d", payload.Priority)
	}
	if payload.Title != "Sonarr" {
		t.Fatalf("the message itself does not change: %+v", payload)
	}

	// What is recorded keeps the published priority: the window changes how
	// the message arrives, not what it is.
	stored := decode[[]messagePayload](t, call(t, server, http.MethodGet,
		"/api/v1/messages", token, nil))
	if len(stored) != 1 || stored[0].Priority != 5 {
		t.Fatalf("recorded priority = %+v", stored)
	}
}

// A window set on the whole channel does not cover criticals: one silences
// the *arr suite without silencing oneself about a full disk.
func TestAChannelWideWindowDoesNotCoverCriticals(t *testing.T) {
	httpServer, server, token := liveServer(t)

	channel := decode[channelPayload](t, call(t, server, http.MethodPost, "/api/v1/channels",
		token, createChannelRequest{Slug: "alerts", Name: "Alerts"}))
	publishToken := decode[publishTokenPayload](t, call(t, server, http.MethodPost,
		fmt.Sprintf("/api/v1/channels/%d/tokens", channel.ID), token,
		createTokenRequest{Name: "alertmanager"}))
	if r := call(t, server, http.MethodPut,
		fmt.Sprintf("/api/v1/channels/%d/quiet-hours", channel.ID), token,
		quietHoursPayload{From: "00:00", To: "23:59"}); r.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", r.Code, r.Body)
	}

	conn := dial(t, httpServer.URL, token, 0)
	readUntil(t, conn, frameReady)

	webhook(t, server, "alerts", publishToken.Token, alertmanagerWebhook{
		Version: "4", Alerts: []alertmanagerAlert{firing("c", "critical")}})

	frame := readUntil(t, conn, eventMessageNew)
	var critical messagePayload
	if err := json.Unmarshal(frame.Payload, &critical); err != nil {
		t.Fatal(err)
	}
	if critical.Priority != store.PriorityMax {
		t.Fatalf("a channel window must not cover a critical: %+v", critical)
	}

	// A warning, on the other hand, does fall silent.
	webhook(t, server, "alerts", publishToken.Token, alertmanagerWebhook{
		Version: "4", Alerts: []alertmanagerAlert{firing("w", "warning")}})
	frame = readUntil(t, conn, eventMessageNew)
	var warning messagePayload
	if err := json.Unmarshal(frame.Payload, &warning); err != nil {
		t.Fatal(err)
	}
	if warning.Priority != store.PriorityMin {
		t.Fatalf("a warning should arrive silent: %+v", warning)
	}
}

// But a window naming "critical" does silence them: on a homelab, a disk
// filling up at three in the morning can wait until seven. The broad gesture
// stays safe, the dangerous one stays deliberate.
func TestAWindowNamingCriticalSilencesIt(t *testing.T) {
	httpServer, server, token := liveServer(t)

	channel := decode[channelPayload](t, call(t, server, http.MethodPost, "/api/v1/channels",
		token, createChannelRequest{Slug: "alerts", Name: "Alerts"}))
	publishToken := decode[publishTokenPayload](t, call(t, server, http.MethodPost,
		fmt.Sprintf("/api/v1/channels/%d/tokens", channel.ID), token,
		createTokenRequest{Name: "alertmanager"}))
	if r := call(t, server, http.MethodPut,
		fmt.Sprintf("/api/v1/channels/%d/quiet-hours", channel.ID), token,
		quietHoursPayload{Severity: "critical", From: "00:00", To: "23:59"}); r.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", r.Code, r.Body)
	}

	conn := dial(t, httpServer.URL, token, 0)
	readUntil(t, conn, frameReady)

	webhook(t, server, "alerts", publishToken.Token, alertmanagerWebhook{
		Version: "4", Alerts: []alertmanagerAlert{firing("c", "critical")}})

	frame := readUntil(t, conn, eventMessageNew)
	var critical messagePayload
	if err := json.Unmarshal(frame.Payload, &critical); err != nil {
		t.Fatal(err)
	}
	if critical.Priority != store.PriorityMin {
		t.Fatalf("the window names critical: it must silence it — %+v", critical)
	}

	// And the alert is there, open: silencing is not losing.
	alerts := decode[[]alertPayload](t, call(t, server, http.MethodGet,
		"/api/v1/alerts?unacked=1", token, nil))
	if len(alerts) != 1 {
		t.Fatalf("the alert must stay to be dealt with: %+v", alerts)
	}
}

// The mute is above everything: it silences what quiet hours would let ring,
// a critical included. Being woken by the rack one is working on is the most
// useless alert there is — for the person working on it, and for them alone.
func TestTheMuteSilencesEvenACritical(t *testing.T) {
	httpServer, server, token := liveServer(t)

	channel := decode[channelPayload](t, call(t, server, http.MethodPost, "/api/v1/channels",
		token, createChannelRequest{Slug: "alerts", Name: "Alerts"}))
	publishToken := decode[publishTokenPayload](t, call(t, server, http.MethodPost,
		fmt.Sprintf("/api/v1/channels/%d/tokens", channel.ID), token,
		createTokenRequest{Name: "alertmanager"}))

	until := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	if r := call(t, server, http.MethodPut, "/api/v1/mute", token,
		setMuteRequest{MutedUntil: until}); r.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", r.Code, r.Body)
	}

	conn := dial(t, httpServer.URL, token, 0)
	readUntil(t, conn, frameReady)

	webhook(t, server, "alerts", publishToken.Token, alertmanagerWebhook{
		Version: "4", Alerts: []alertmanagerAlert{firing("c", "critical")}})

	frame := readUntil(t, conn, eventMessageNew)
	var payload messagePayload
	if err := json.Unmarshal(frame.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Silent {
		t.Fatalf("the mute must cover a critical: %+v", payload)
	}
	// It silences, it does not lose: the alert is there, open, to be dealt with.
	alerts := decode[[]alertPayload](t, call(t, server, http.MethodGet,
		"/api/v1/alerts?unacked=1", token, nil))
	if len(alerts) != 1 {
		t.Fatalf("the alert must stay to be dealt with: %+v", alerts)
	}

	// Lifted, the silence stops at once.
	if r := call(t, server, http.MethodPut, "/api/v1/mute", token,
		setMuteRequest{}); r.Code != http.StatusOK {
		t.Fatalf("lifting: %d", r.Code)
	}
	webhook(t, server, "alerts", publishToken.Token, alertmanagerWebhook{
		Version: "4", Alerts: []alertmanagerAlert{firing("d", "critical")}})
	frame = readUntil(t, conn, eventMessageNew)
	var after messagePayload
	if err := json.Unmarshal(frame.Payload, &after); err != nil {
		t.Fatal(err)
	}
	if after.Silent {
		t.Fatalf("the mute lifted, nothing must be silenced: %+v", after)
	}
}

// The global window applies everywhere, the channel one replaces it where it
// exists: the "most specific wins" rule, set once for the night and argued
// channel by channel only when a channel deserves it.
func TestAChannelWindowOverridesTheGlobalOne(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	alerts, _ := issueChannelAndToken(t, repository, "alerts")
	notifs, _ := issueChannelAndToken(t, repository, "notifications")
	token := session(t, server, "thomas", testPassword)

	// The default: the night, for everybody.
	if r := call(t, server, http.MethodPut, "/api/v1/quiet-hours", token,
		quietHoursPayload{From: "23:00", To: "07:00"}); r.Code != http.StatusOK {
		t.Fatalf("default: %d %s", r.Code, r.Body)
	}
	// The override: on notifications, one also falls silent in the afternoon.
	if r := call(t, server, http.MethodPut,
		fmt.Sprintf("/api/v1/channels/%d/quiet-hours", notifs.ID), token,
		quietHoursPayload{From: "14:00", To: "16:00"}); r.Code != http.StatusOK {
		t.Fatalf("override: %d %s", r.Code, r.Body)
	}

	// The channel with no override inherits the default.
	inherited, err := repository.QuietHoursFor(alerts.ID, "warning")
	if err != nil || inherited.From != "23:00" || !inherited.IsDefault() {
		t.Fatalf("inheritance = %+v (%v)", inherited, err)
	}
	// The one that has an override sees it win.
	own, err := repository.QuietHoursFor(notifs.ID, "")
	if err != nil || own.From != "14:00" || own.IsDefault() {
		t.Fatalf("override = %+v (%v)", own, err)
	}

	// And a channel's listing shows both, each labelled.
	listed := decode[[]quietHoursPayload](t, call(t, server, http.MethodGet,
		fmt.Sprintf("/api/v1/channels/%d/quiet-hours", notifs.ID), token, nil))
	scopes := map[string]string{}
	for _, window := range listed {
		scopes[window.Scope] = window.From
	}
	if scopes[scopeChannel] != "14:00" || scopes[scopeDefault] != "23:00" {
		t.Fatalf("scopes = %+v", listed)
	}
}

// A broad window, global or not, does not cover a critical.
func TestTheGlobalWindowDoesNotCoverCriticals(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	channel, _ := issueChannelAndToken(t, repository, "alerts")
	token := session(t, server, "thomas", testPassword)

	if r := call(t, server, http.MethodPut, "/api/v1/quiet-hours", token,
		quietHoursPayload{From: "00:00", To: "23:59"}); r.Code != http.StatusOK {
		t.Fatalf("status = %d", r.Code)
	}

	critical, _ := repository.QuietHoursFor(channel.ID, "critical")
	if critical.Set() {
		t.Fatalf("a global window must not cover a critical: %+v", critical)
	}
	warning, _ := repository.QuietHoursFor(channel.ID, "warning")
	if !warning.Set() {
		t.Fatal("a warning should be covered by the global window")
	}
}
