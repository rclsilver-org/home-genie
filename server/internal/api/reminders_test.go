package api

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/rclsilver-org/home-notifications/server/internal/store"
)

func TestReminderPolicyIsSetAndListed(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	channel, _ := issueChannelAndToken(t, repository, "alerts-critical")
	token := session(t, server, "thomas", testPassword)
	path := fmt.Sprintf("/api/v1/channels/%d/reminders", channel.ID)

	recorder := call(t, server, http.MethodPut, path, token, reminderPolicyPayload{
		Severity: "critical", IntervalSeconds: 900,
		QuietFrom: "23:00", QuietTo: "07:00", Enabled: true,
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
	if listed[0].QuietFrom != "23:00" || listed[0].QuietTo != "07:00" {
		t.Fatalf("heures calmes perdues : %+v", listed[0])
	}
}

// Half a quiet window would silence at an unpredictable hour.
func TestHalfAQuietWindowIsRefused(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	channel, _ := issueChannelAndToken(t, repository, "alerts")
	token := session(t, server, "thomas", testPassword)
	path := fmt.Sprintf("/api/v1/channels/%d/reminders", channel.ID)

	for _, policy := range []reminderPolicyPayload{
		{Severity: "critical", IntervalSeconds: 900, QuietFrom: "23:00", Enabled: true},
		{Severity: "critical", IntervalSeconds: 900, QuietTo: "07:00", Enabled: true},
	} {
		if r := call(t, server, http.MethodPut, path, token, policy); r.Code != http.StatusBadRequest {
			t.Errorf("%+v : status = %d, want 400", policy, r.Code)
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
