package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/rclsilver-org/home-genie/server/internal/store"
)

func TestManualClosePreservesHistoryAndStopsReminders(t *testing.T) {
	s, repository := newTestServer(t)
	user := withLocalAccount(t, repository, "thomas", testPassword)
	channel, publishToken := issueChannelAndToken(t, repository, "alerts")
	webhook(t, s, "alerts", publishToken, alertmanagerWebhook{Alerts: []alertmanagerAlert{firing("manual", "critical")}})
	token := session(t, s, "thomas", testPassword)
	id := alertsOf(t, s, token, channel.ID, true)[0].ID
	next := time.Now().Add(time.Hour)
	if err := repository.SetNextReminder(id, &next, 2); err != nil {
		t.Fatal(err)
	}
	call(t, s, http.MethodPost, fmt.Sprintf("/api/v1/alerts/%d/ack", id), token, nil)
	path := fmt.Sprintf("/api/v1/alerts/%d/close", id)
	response := call(t, s, http.MethodPost, path, token, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("close: %d: %s", response.Code, response.Body)
	}
	var payload struct {
		Status, ClosedBy string
		ResolvedAt       *time.Time `json:"resolved_at"`
	}
	// Read the wire field explicitly: old clients ignore this additive field.
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &wire); err != nil {
		t.Fatal(err)
	}
	json.Unmarshal(wire["status"], &payload.Status)
	json.Unmarshal(wire["closed_by"], &payload.ClosedBy)
	json.Unmarshal(wire["resolved_at"], &payload.ResolvedAt)
	if payload.Status != "resolved" || payload.ClosedBy != user.Username || payload.ResolvedAt == nil {
		t.Fatalf("closure = %s", response.Body)
	}
	stored, err := repository.AlertByID(id)
	if err != nil || stored.NextReminderAt != nil || stored.AckedAt == nil || stored.ReminderCount != 2 {
		t.Fatalf("stored = %+v, %v", stored, err)
	}
	if len(alertsOf(t, s, token, channel.ID, true)) != 0 {
		t.Fatal("closed alert still open")
	}
	detail := decode[alertDetailPayload](t, call(t, s, http.MethodGet, fmt.Sprintf("/api/v1/alerts/%d", id), token, nil))
	kinds := map[string]int{}
	for _, entry := range detail.Log {
		kinds[entry.Kind]++
		if entry.Kind == "closed" && entry.Detail != "by thomas" {
			t.Fatalf("closer = %q", entry.Detail)
		}
	}
	if kinds["opened"] != 1 || kinds["notified"] != 1 || kinds["acked"] != 1 || kinds["closed"] != 1 || kinds["resolved"] != 0 {
		t.Fatalf("log = %+v", detail.Log)
	}
	before, _ := repository.LatestSeq(user.ID)
	retry := call(t, s, http.MethodPost, path, token, nil)
	after, _ := repository.LatestSeq(user.ID)
	if retry.Code != http.StatusOK || before != after || retry.Body.String() != response.Body.String() {
		t.Fatalf("retry changed closure: %d, seq %d -> %d", retry.Code, before, after)
	}
	events, _ := repository.EventsSince(user.ID, 0, 100)
	resolved, closedMessages := 0, 0
	for _, event := range events {
		if event.Kind == eventAlertResolved {
			resolved++
		}
		if event.Kind == "message.new" {
			var message messagePayload
			json.Unmarshal(event.Payload, &message)
			if message.AlertResolved {
				closedMessages++
				if message.Priority != store.PriorityMin || message.Title != "Closed — Disk full on nas" {
					t.Fatalf("closure message = %+v", message)
				}
			}
		}
	}
	if resolved != 1 || closedMessages != 1 {
		t.Fatalf("resolved events=%d, closure messages=%d", resolved, closedMessages)
	}
}

func TestManualCloseAuthorization(t *testing.T) {
	s, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	withLocalAccount(t, repository, "outsider", testPassword)
	channel, publishToken := issueChannelAndToken(t, repository, "alerts")
	webhook(t, s, "alerts", publishToken, alertmanagerWebhook{Alerts: []alertmanagerAlert{firing("private", "warning")}})
	owner := session(t, s, "thomas", testPassword)
	id := alertsOf(t, s, owner, channel.ID, true)[0].ID
	path := fmt.Sprintf("/api/v1/alerts/%d/close", id)
	for _, tc := range []struct {
		token string
		want  int
	}{{"", 401}, {publishToken, 401}, {session(t, s, "outsider", testPassword), 404}} {
		if r := call(t, s, http.MethodPost, path, tc.token, nil); r.Code != tc.want {
			t.Fatalf("status=%d want %d", r.Code, tc.want)
		}
	}
	if r := call(t, s, http.MethodPost, "/api/v1/alerts/9999/close", owner, nil); r.Code != 404 {
		t.Fatalf("unknown: %d", r.Code)
	}
}

func TestManualCloseAllowsSameSourceIncidentToFireAgain(t *testing.T) {
	s, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	channel, publishToken := issueChannelAndToken(t, repository, "alerts")
	token := session(t, s, "thomas", testPassword)
	incoming := firing("same-source", "critical")
	var previous int64
	for i := 0; i < 3; i++ {
		r := webhook(t, s, "alerts", publishToken, alertmanagerWebhook{Alerts: []alertmanagerAlert{incoming}})
		if counts := decode[map[string]int](t, r); counts["opened"] != 1 {
			t.Fatalf("cycle %d: %+v", i, counts)
		}
		open := alertsOf(t, s, token, channel.ID, true)
		if len(open) != 1 || open[0].ID == previous || open[0].AckedAt != nil {
			t.Fatalf("cycle %d: %+v", i, open)
		}
		previous = open[0].ID
		if i < 2 {
			r = call(t, s, http.MethodPost, fmt.Sprintf("/api/v1/alerts/%d/close", previous), token, nil)
			if r.Code != 200 {
				t.Fatalf("close: %d", r.Code)
			}
		}
	}
	if all := alertsOf(t, s, token, channel.ID, false); len(all) != 3 {
		t.Fatalf("history lost: %+v", all)
	}
	incoming.Status = "resolved"
	incoming.EndsAt = time.Now().UTC().Add(time.Minute).Format(time.RFC3339)
	r := webhook(t, s, "alerts", publishToken, alertmanagerWebhook{Alerts: []alertmanagerAlert{incoming}})
	if counts := decode[map[string]int](t, r); counts["resolved"] != 1 {
		t.Fatalf("source recovery: %+v", counts)
	}
	if len(alertsOf(t, s, token, channel.ID, true)) != 0 {
		t.Fatal("source recovery left an alert open")
	}
}

func TestManualCloseThenSourceRecoveryDoesNotCreateAnAlert(t *testing.T) {
	s, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	channel, publishToken := issueChannelAndToken(t, repository, "alerts")
	token := session(t, s, "thomas", testPassword)
	incoming := firing("late-recovery", "warning")
	webhook(t, s, "alerts", publishToken, alertmanagerWebhook{Alerts: []alertmanagerAlert{incoming}})
	id := alertsOf(t, s, token, channel.ID, true)[0].ID
	call(t, s, http.MethodPost, fmt.Sprintf("/api/v1/alerts/%d/close", id), token, nil)
	incoming.Status = "resolved"
	webhook(t, s, "alerts", publishToken, alertmanagerWebhook{Alerts: []alertmanagerAlert{incoming}})
	if all := alertsOf(t, s, token, channel.ID, false); len(all) != 1 || all[0].Status != "resolved" {
		t.Fatalf("alerts = %+v", all)
	}
}

func TestReaderCanCloseAnUnacknowledgedAlertAndCancelReminders(t *testing.T) {
	s, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	reader := withLocalAccount(t, repository, "reader", testPassword)
	channel, publishToken := issueChannelAndToken(t, repository, "alerts")
	if err := repository.SetMember(channel.ID, reader.ID, store.RoleReader); err != nil {
		t.Fatal(err)
	}
	webhook(t, s, "alerts", publishToken, alertmanagerWebhook{Alerts: []alertmanagerAlert{firing("unacked", "warning")}})
	token := session(t, s, "reader", testPassword)
	id := alertsOf(t, s, token, channel.ID, true)[0].ID
	next := time.Now().Add(-time.Minute)
	if err := repository.SetNextReminder(id, &next, 0); err != nil {
		t.Fatal(err)
	}
	r := call(t, s, http.MethodPost, fmt.Sprintf("/api/v1/alerts/%d/close", id), token, nil)
	if r.Code != http.StatusOK {
		t.Fatalf("close: %d: %s", r.Code, r.Body)
	}
	stored, err := repository.AlertByID(id)
	if err != nil || stored.NextReminderAt != nil || stored.AckedAt != nil || stored.ClosedBy != "reader" {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
	due, err := repository.DueReminders(time.Now(), 10)
	if err != nil || len(due) != 0 {
		t.Fatalf("due after closure=%+v err=%v", due, err)
	}
}
