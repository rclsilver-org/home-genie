package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rclsilver-org/home-notifications/server/internal/store"
)

// webhook posts an Alertmanager payload exactly as Alertmanager would.
func webhook(t *testing.T, server *Server, slug, token string, payload alertmanagerWebhook) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost,
		"/api/v1/ingest/alertmanager/"+slug, bytes.NewReader(encoded))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.Routes().ServeHTTP(recorder, request)
	return recorder
}

func firing(fingerprint, severity string) alertmanagerAlert {
	return alertmanagerAlert{
		Status:      store.AlertFiring,
		Fingerprint: fingerprint,
		Labels: map[string]string{
			"alertname": "DiskFull", "severity": severity, "instance": "nas",
		},
		Annotations:  map[string]string{"summary": "Disk full on nas"},
		StartsAt:     "2026-09-10T20:00:00Z",
		GeneratorURL: "http://prometheus/graph",
	}
}

func alertsOf(t *testing.T, server *Server, token string, channelID int64, openOnly bool) []alertPayload {
	t.Helper()
	path := fmt.Sprintf("/api/v1/channels/%d/alerts", channelID)
	if openOnly {
		path += "?open=1"
	}
	return decode[[]alertPayload](t, call(t, server, http.MethodGet, path, token, nil))
}

func TestFiringAlertOpensAndNotifies(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	channel, publishToken := issueChannelAndToken(t, repository, "alerts-critical")

	recorder := webhook(t, server, "alerts-critical", publishToken, alertmanagerWebhook{
		Version: "4", Status: "firing",
		Alerts: []alertmanagerAlert{firing("abc123", "critical")},
	})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body)
	}
	if counts := decode[map[string]int](t, recorder); counts["opened"] != 1 {
		t.Fatalf("counts = %+v", counts)
	}

	token := session(t, server, "thomas", testPassword)
	alerts := alertsOf(t, server, token, channel.ID, false)
	if len(alerts) != 1 {
		t.Fatalf("%d alerts", len(alerts))
	}
	if alerts[0].Status != store.AlertFiring || alerts[0].Severity != "critical" {
		t.Fatalf("alert = %+v", alerts[0])
	}
	if alerts[0].Title != "Disk full on nas" {
		t.Fatalf("title = %q — the summary annotation should win", alerts[0].Title)
	}

	// A message goes with the alert, at the priority of its severity.
	messages := decode[[]messagePayload](t, call(t, server, http.MethodGet,
		fmt.Sprintf("/api/v1/channels/%d/messages", channel.ID), token, nil))
	if len(messages) != 1 {
		t.Fatalf("%d messages", len(messages))
	}
	if messages[0].Priority != store.PriorityMax {
		t.Fatalf("priority = %d, want %d for critical", messages[0].Priority, store.PriorityMax)
	}
	if messages[0].AlertID == nil || *messages[0].AlertID != alerts[0].ID {
		t.Fatal("the message is not tied to its alert")
	}
}

// Alertmanager repeats an alert for as long as it lasts. Notifying again on
// every repeat is exactly the noise this design removes.
func TestRepeatedFiringDoesNotRenotify(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	channel, publishToken := issueChannelAndToken(t, repository, "alerts-critical")

	payload := alertmanagerWebhook{
		Version: "4", Status: "firing",
		Alerts: []alertmanagerAlert{firing("abc123", "critical")},
	}
	webhook(t, server, "alerts-critical", publishToken, payload)
	second := webhook(t, server, "alerts-critical", publishToken, payload)

	if counts := decode[map[string]int](t, second); counts["refreshed"] != 1 || counts["opened"] != 0 {
		t.Fatalf("counts = %+v — the repeat reopened an alert", counts)
	}

	token := session(t, server, "thomas", testPassword)
	if alerts := alertsOf(t, server, token, channel.ID, false); len(alerts) != 1 {
		t.Fatalf("%d alerts for two repeats", len(alerts))
	}
	messages := decode[[]messagePayload](t, call(t, server, http.MethodGet,
		fmt.Sprintf("/api/v1/channels/%d/messages", channel.ID), token, nil))
	if len(messages) != 1 {
		t.Fatalf("%d messages for two repeats, want 1", len(messages))
	}
}

func TestResolutionClosesAndNotifiesQuietly(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	channel, publishToken := issueChannelAndToken(t, repository, "alerts-critical")

	webhook(t, server, "alerts-critical", publishToken, alertmanagerWebhook{
		Version: "4", Status: "firing",
		Alerts: []alertmanagerAlert{firing("abc123", "critical")},
	})

	resolved := firing("abc123", "critical")
	resolved.Status = store.AlertResolved
	resolved.EndsAt = "2026-09-10T20:30:00Z"
	recorder := webhook(t, server, "alerts-critical", publishToken, alertmanagerWebhook{
		Version: "4", Status: "resolved", Alerts: []alertmanagerAlert{resolved},
	})
	if counts := decode[map[string]int](t, recorder); counts["resolved"] != 1 {
		t.Fatalf("counts = %+v", counts)
	}

	token := session(t, server, "thomas", testPassword)
	alerts := alertsOf(t, server, token, channel.ID, false)
	if alerts[0].Status != store.AlertResolved || alerts[0].ResolvedAt == nil {
		t.Fatalf("alert = %+v", alerts[0])
	}
	if open := alertsOf(t, server, token, channel.ID, true); len(open) != 0 {
		t.Fatalf("%d alerts still open", len(open))
	}

	// A resolution must not shout: it is good news arriving after the alert
	// has already woken somebody.
	messages := decode[[]messagePayload](t, call(t, server, http.MethodGet,
		fmt.Sprintf("/api/v1/channels/%d/messages", channel.ID), token, nil))
	if len(messages) != 2 {
		t.Fatalf("%d messages, want 2", len(messages))
	}
	if messages[0].Priority != store.PriorityLow {
		t.Fatalf("resolution priority = %d, want %d", messages[0].Priority, store.PriorityLow)
	}
}

// A resolution for an alert never seen open must create nothing.
func TestResolvingAnUnknownAlertIsIgnored(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	channel, publishToken := issueChannelAndToken(t, repository, "alerts-critical")

	resolved := firing("never-seen", "critical")
	resolved.Status = store.AlertResolved
	recorder := webhook(t, server, "alerts-critical", publishToken, alertmanagerWebhook{
		Version: "4", Status: "resolved", Alerts: []alertmanagerAlert{resolved},
	})
	if counts := decode[map[string]int](t, recorder); counts["resolved"] != 0 {
		t.Fatalf("counts = %+v", counts)
	}

	token := session(t, server, "thomas", testPassword)
	if alerts := alertsOf(t, server, token, channel.ID, false); len(alerts) != 0 {
		t.Fatalf("%d alerts created out of a resolution", len(alerts))
	}
}

// The same alert may reopen after a resolution: that is a new entity, not a
// resurrection of the old one.
func TestAlertCanReopenAfterResolution(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	channel, publishToken := issueChannelAndToken(t, repository, "alerts-critical")

	open := firing("abc123", "critical")
	webhook(t, server, "alerts-critical", publishToken, alertmanagerWebhook{
		Version: "4", Alerts: []alertmanagerAlert{open}})

	closed := open
	closed.Status = store.AlertResolved
	closed.EndsAt = "2026-09-10T20:30:00Z"
	webhook(t, server, "alerts-critical", publishToken, alertmanagerWebhook{
		Version: "4", Alerts: []alertmanagerAlert{closed}})

	reopened := open
	reopened.StartsAt = "2026-09-10T21:00:00Z"
	recorder := webhook(t, server, "alerts-critical", publishToken, alertmanagerWebhook{
		Version: "4", Alerts: []alertmanagerAlert{reopened}})
	if counts := decode[map[string]int](t, recorder); counts["opened"] != 1 {
		t.Fatalf("counts = %+v — reopening created no alert", counts)
	}

	token := session(t, server, "thomas", testPassword)
	if alerts := alertsOf(t, server, token, channel.ID, false); len(alerts) != 2 {
		t.Fatalf("%d alerts, want 2 (one closed, one open)", len(alerts))
	}
	if open := alertsOf(t, server, token, channel.ID, true); len(open) != 1 {
		t.Fatalf("%d open alerts, want 1", len(open))
	}
}

func TestSeverityDrivesPriority(t *testing.T) {
	cases := map[string]int{
		"critical": store.PriorityMax,
		"warning":  store.PriorityHigh,
		"info":     store.PriorityLow,
		"":         store.PriorityDefault,
		"unknown":  store.PriorityDefault,
	}
	for severity, want := range cases {
		if got := severityPriority(severity); got != want {
			t.Errorf("severityPriority(%q) = %d, want %d", severity, got, want)
		}
	}
}

func TestAckIsRecordedAndNamed(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	channel, publishToken := issueChannelAndToken(t, repository, "alerts-critical")
	webhook(t, server, "alerts-critical", publishToken, alertmanagerWebhook{
		Version: "4", Alerts: []alertmanagerAlert{firing("abc123", "critical")}})

	token := session(t, server, "thomas", testPassword)
	alerts := alertsOf(t, server, token, channel.ID, false)

	acked := decode[alertPayload](t, call(t, server, http.MethodPost,
		fmt.Sprintf("/api/v1/alerts/%d/ack", alerts[0].ID), token, nil))
	if acked.AckedAt == nil || acked.AckedBy != "thomas" {
		t.Fatalf("acknowledgement = %+v", acked)
	}

	// The alert stays open: acknowledging is not resolving.
	if acked.Status != store.AlertFiring {
		t.Fatalf("status = %q — acknowledging closed the alert", acked.Status)
	}

	// Acknowledging twice changes neither the author nor the timestamp.
	again := decode[alertPayload](t, call(t, server, http.MethodPost,
		fmt.Sprintf("/api/v1/alerts/%d/ack", alerts[0].ID), token, nil))
	if !again.AckedAt.Equal(*acked.AckedAt) {
		t.Fatal("a second acknowledgement overwrote the first")
	}
}

func TestAlertsRequireMembership(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	withLocalAccount(t, repository, "other", testPassword)
	channel, publishToken := issueChannelAndToken(t, repository, "alerts-critical")
	webhook(t, server, "alerts-critical", publishToken, alertmanagerWebhook{
		Version: "4", Alerts: []alertmanagerAlert{firing("abc123", "critical")}})

	owner := session(t, server, "thomas", testPassword)
	alerts := alertsOf(t, server, owner, channel.ID, false)

	stranger := session(t, server, "other", testPassword)
	if r := call(t, server, http.MethodGet,
		fmt.Sprintf("/api/v1/channels/%d/alerts", channel.ID), stranger, nil); r.Code != http.StatusNotFound {
		t.Errorf("list: status = %d, want 404", r.Code)
	}
	if r := call(t, server, http.MethodPost,
		fmt.Sprintf("/api/v1/alerts/%d/ack", alerts[0].ID), stranger, nil); r.Code != http.StatusNotFound {
		t.Errorf("acknowledgement: status = %d, want 404", r.Code)
	}
}

func TestMalformedWebhookIsRefused(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	issueChannelAndToken(t, repository, "alerts-critical")
	_, token := issueChannelAndToken(t, repository, "vide")

	// A payload with no alert.
	if r := webhook(t, server, "vide", token, alertmanagerWebhook{Version: "4"}); r.Code != http.StatusBadRequest {
		t.Errorf("empty payload: status = %d, want 400", r.Code)
	}

	// An alert with no fingerprint: ignored, no entity created.
	noFingerprint := firing("", "critical")
	r := webhook(t, server, "vide", token, alertmanagerWebhook{
		Version: "4", Alerts: []alertmanagerAlert{noFingerprint}})
	if counts := decode[map[string]int](t, r); counts["opened"] != 0 {
		t.Errorf("an alert with no fingerprint was opened: %+v", counts)
	}
}

// The home screen reads every alert of the caller, across all channels, so
// that an alert without its origin is unusable when the console mixes
// several of them.
func TestAllAlertsSpanChannelsAndCarryTheirSlug(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	_, criticalToken := issueChannelAndToken(t, repository, "alerts-critical")
	_, warningToken := issueChannelAndToken(t, repository, "alerts-warning")

	webhook(t, server, "alerts-critical", criticalToken, alertmanagerWebhook{
		Version: "4", Alerts: []alertmanagerAlert{firing("fp-critical", "critical")}})
	webhook(t, server, "alerts-warning", warningToken, alertmanagerWebhook{
		Version: "4", Alerts: []alertmanagerAlert{firing("fp-warning", "warning")}})

	token := session(t, server, "thomas", testPassword)
	all := decode[[]alertPayload](t, call(t, server, http.MethodGet, "/api/v1/alerts", token, nil))
	if len(all) != 2 {
		t.Fatalf("%d alerts, want 2 across all channels", len(all))
	}
	slugs := map[string]bool{}
	for _, alert := range all {
		if alert.ChannelSlug == "" {
			t.Fatalf("alert with no channel of origin: %+v", alert)
		}
		slugs[alert.ChannelSlug] = true
	}
	if !slugs["alerts-critical"] || !slugs["alerts-warning"] {
		t.Fatalf("channels seen: %v", slugs)
	}
}

// And never the alerts of a channel one does not belong to.
func TestAllAlertsExcludeForeignChannels(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	withLocalAccount(t, repository, "other", testPassword)
	_, publishToken := issueChannelAndToken(t, repository, "prive")

	webhook(t, server, "prive", publishToken, alertmanagerWebhook{
		Version: "4", Alerts: []alertmanagerAlert{firing("fp", "critical")}})

	stranger := session(t, server, "other", testPassword)
	all := decode[[]alertPayload](t, call(t, server, http.MethodGet, "/api/v1/alerts", stranger, nil))
	if len(all) != 0 {
		t.Fatalf("a non-member sees %d alerts", len(all))
	}
}

func TestAllAlertsCanBeRestrictedToOpenOnes(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	_, publishToken := issueChannelAndToken(t, repository, "alerts-critical")

	webhook(t, server, "alerts-critical", publishToken, alertmanagerWebhook{
		Version: "4", Alerts: []alertmanagerAlert{firing("ouverte", "critical")}})

	closed := firing("fermee", "critical")
	webhook(t, server, "alerts-critical", publishToken, alertmanagerWebhook{
		Version: "4", Alerts: []alertmanagerAlert{closed}})
	closed.Status = store.AlertResolved
	closed.EndsAt = "2026-09-10T21:00:00Z"
	webhook(t, server, "alerts-critical", publishToken, alertmanagerWebhook{
		Version: "4", Alerts: []alertmanagerAlert{closed}})

	token := session(t, server, "thomas", testPassword)
	all := decode[[]alertPayload](t, call(t, server, http.MethodGet, "/api/v1/alerts", token, nil))
	open := decode[[]alertPayload](t, call(t, server, http.MethodGet, "/api/v1/alerts?open=1", token, nil))

	if len(all) != 2 || len(open) != 1 {
		t.Fatalf("all = %d, open = %d — want 2 and 1", len(all), len(open))
	}
	if open[0].Status != store.AlertFiring {
		t.Fatalf("statut = %q", open[0].Status)
	}
}

// The occurrence counter is what tells an alert that beats from a stable
// one: without it, forty deliveries and a single one are
// indistinguishable.
func TestOccurrencesCountAlertmanagerDeliveries(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	issueChannelAndToken(t, repository, "alerts-critical")
	_, publishToken := issueChannelAndToken(t, repository, "alerts-critical-2")

	payload := alertmanagerWebhook{
		Version: "4", Alerts: []alertmanagerAlert{firing("beating", "critical")},
	}
	for i := 0; i < 4; i++ {
		webhook(t, server, "alerts-critical-2", publishToken, payload)
	}

	token := session(t, server, "thomas", testPassword)
	all := decode[[]alertPayload](t, call(t, server, http.MethodGet, "/api/v1/alerts", token, nil))
	if len(all) != 1 {
		t.Fatalf("%d alerts for four deliveries, want 1", len(all))
	}
	if all[0].Occurrences != 4 {
		t.Fatalf("occurrences = %d, want 4", all[0].Occurrences)
	}
}

// The filter that matters: what is still open AND that nobody has taken.
func TestUnackedFilterKeepsOnlyWhatNeedsAction(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	_, publishToken := issueChannelAndToken(t, repository, "alerts-critical")

	for _, fingerprint := range []string{"prise", "libre"} {
		alert := firing(fingerprint, "critical")
		webhook(t, server, "alerts-critical", publishToken, alertmanagerWebhook{
			Version: "4", Alerts: []alertmanagerAlert{alert}})
	}

	token := session(t, server, "thomas", testPassword)
	all := decode[[]alertPayload](t, call(t, server, http.MethodGet, "/api/v1/alerts", token, nil))
	if len(all) != 2 {
		t.Fatalf("%d alerts", len(all))
	}

	call(t, server, http.MethodPost, fmt.Sprintf("/api/v1/alerts/%d/ack", all[0].ID), token, nil)

	unacked := decode[[]alertPayload](t, call(t, server, http.MethodGet,
		"/api/v1/alerts?unacked=1", token, nil))
	if len(unacked) != 1 || unacked[0].ID == all[0].ID {
		t.Fatalf("unacked = %+v — the acknowledged one should be gone", unacked)
	}
}

func TestSeverityFilter(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	_, criticalToken := issueChannelAndToken(t, repository, "alerts-critical")
	_, warningToken := issueChannelAndToken(t, repository, "alerts-warning")

	webhook(t, server, "alerts-critical", criticalToken, alertmanagerWebhook{
		Version: "4", Alerts: []alertmanagerAlert{firing("c", "critical")}})
	webhook(t, server, "alerts-warning", warningToken, alertmanagerWebhook{
		Version: "4", Alerts: []alertmanagerAlert{firing("w", "warning")}})

	token := session(t, server, "thomas", testPassword)
	criticals := decode[[]alertPayload](t, call(t, server, http.MethodGet,
		"/api/v1/alerts?severity=critical", token, nil))
	if len(criticals) != 1 || criticals[0].Severity != "critical" {
		t.Fatalf("criticals = %+v", criticals)
	}
}

// The journal is rebuilt from what is already recorded; it must tell the
// story: the opening, the notification, the reminders, the taking over.
func TestAlertLogTellsTheStory(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	channel, publishToken := issueChannelAndToken(t, repository, "alerts-critical")
	token := session(t, server, "thomas", testPassword)

	call(t, server, http.MethodPut,
		fmt.Sprintf("/api/v1/channels/%d/reminders", channel.ID), token,
		reminderPolicyPayload{Severity: "critical", IntervalSeconds: 60, Enabled: true})

	webhook(t, server, "alerts-critical", publishToken, alertmanagerWebhook{
		Version: "4", Alerts: []alertmanagerAlert{firing("histoire", "critical")}})
	// A repeat, silent but counted.
	webhook(t, server, "alerts-critical", publishToken, alertmanagerWebhook{
		Version: "4", Alerts: []alertmanagerAlert{firing("histoire", "critical")}})

	all := decode[[]alertPayload](t, call(t, server, http.MethodGet, "/api/v1/alerts", token, nil))
	call(t, server, http.MethodPost, fmt.Sprintf("/api/v1/alerts/%d/ack", all[0].ID), token, nil)

	detail := decode[alertDetailPayload](t, call(t, server, http.MethodGet,
		fmt.Sprintf("/api/v1/alerts/%d", all[0].ID), token, nil))

	kinds := map[string]bool{}
	for _, entry := range detail.Log {
		kinds[entry.Kind] = true
	}
	for _, expected := range []string{
		store.AlertLogOpened, store.AlertLogNotified, store.AlertLogRepeated, store.AlertLogAcked,
	} {
		if !kinds[expected] {
			t.Errorf("the journal does not tell %q: %+v", expected, detail.Log)
		}
	}
	if detail.Alert.Occurrences != 2 {
		t.Errorf("occurrences = %d, want 2", detail.Alert.Occurrences)
	}
}

func TestAlertDetailRequiresMembership(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	withLocalAccount(t, repository, "other", testPassword)
	_, publishToken := issueChannelAndToken(t, repository, "prive")
	webhook(t, server, "prive", publishToken, alertmanagerWebhook{
		Version: "4", Alerts: []alertmanagerAlert{firing("fp", "critical")}})

	owner := session(t, server, "thomas", testPassword)
	all := decode[[]alertPayload](t, call(t, server, http.MethodGet, "/api/v1/alerts", owner, nil))

	stranger := session(t, server, "other", testPassword)
	if r := call(t, server, http.MethodGet,
		fmt.Sprintf("/api/v1/alerts/%d", all[0].ID), stranger, nil); r.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", r.Code)
	}
}
