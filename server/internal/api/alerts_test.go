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
