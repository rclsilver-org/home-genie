package store

import (
	"testing"
	"time"
)

func TestManualCloseKeepsTheFirstCloserAndHistoryWithoutDelivery(t *testing.T) {
	s := newTestStore(t)
	first, err := s.CreateLocalUser("first", "", "hash", false)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CreateLocalUser("second", "", "hash", false)
	if err != nil {
		t.Fatal(err)
	}
	channel, err := s.CreateChannel("alerts", "", "", first.ID)
	if err != nil {
		t.Fatal(err)
	}
	alert, err := s.CreateAlert(NewAlert{ChannelID: channel.ID, Fingerprint: "incident"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateMessage(NewMessage{ChannelID: channel.ID, AlertID: &alert.ID, Title: "original", Priority: 3}); err != nil {
		t.Fatal(err)
	}
	if changed, err := s.CloseAlert(alert.ID, first.ID); err != nil || !changed {
		t.Fatalf("first close: %v %v", changed, err)
	}
	if changed, err := s.CloseAlert(alert.ID, second.ID); err != nil || changed {
		t.Fatalf("second close: %v %v", changed, err)
	}
	closed, err := s.AlertByID(alert.ID)
	if err != nil || closed.ClosedBy != "first" {
		t.Fatalf("closer changed: %+v %v", closed, err)
	}
	log, err := s.LogOf(closed)
	if err != nil {
		t.Fatal(err)
	}
	if len(log) != 3 || log[1].Kind != AlertLogNotified || log[1].Detail != "original" || log[2].Kind != AlertLogClosed {
		t.Fatalf("history lost without a closure message: %+v", log)
	}
}

func TestManualCloseAndRefireWithAFixedClock(t *testing.T) {
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	s := newTestStore(t).WithClock(func() time.Time { return now })
	user, err := s.CreateLocalUser("member", "", "hash", false)
	if err != nil {
		t.Fatal(err)
	}
	channel, err := s.CreateChannel("alerts", "", "", user.ID)
	if err != nil {
		t.Fatal(err)
	}
	input := NewAlert{ChannelID: channel.ID, Fingerprint: "same-time", StartedAt: now}
	for i := 0; i < 3; i++ {
		alert, err := s.CreateAlert(input)
		if err != nil {
			t.Fatalf("occurrence %d: %v", i, err)
		}
		if !alert.StartedAt.Equal(now.Add(time.Duration(i) * time.Second)) {
			t.Fatalf("opening time = %s", alert.StartedAt)
		}
		if changed, err := s.CloseAlert(alert.ID, user.ID); err != nil || !changed {
			t.Fatalf("close: %v %v", changed, err)
		}
		closed, err := s.AlertByID(alert.ID)
		if err != nil || closed.ResolvedAt == nil || closed.ResolvedAt.Before(closed.StartedAt) {
			t.Fatalf("closure precedes opening: %+v %v", closed, err)
		}
	}
}

func TestNewSourceIncidentDoesNotCollideWithALocalOccurrence(t *testing.T) {
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	s := newTestStore(t).WithClock(func() time.Time { return now })
	user, err := s.CreateLocalUser("member", "", "hash", false)
	if err != nil {
		t.Fatal(err)
	}
	channel, err := s.CreateChannel("alerts", "", "", user.ID)
	if err != nil {
		t.Fatal(err)
	}
	input := NewAlert{ChannelID: channel.ID, Fingerprint: "next-incident", StartedAt: now}
	first, err := s.CreateAlert(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CloseAlert(first.ID, user.ID); err != nil {
		t.Fatal(err)
	}
	local, err := s.CreateAlert(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ResolveAlert(local.ID, now); err != nil {
		t.Fatal(err)
	}
	resolved, err := s.AlertByID(local.ID)
	if err != nil {
		t.Fatal(err)
	}
	input.StartedAt = local.StartedAt
	fresh, err := s.CreateAlert(input)
	if err != nil || fresh.ID == local.ID {
		t.Fatalf("new source incident lost: %+v %v", fresh, err)
	}
	if resolved.ResolvedAt.Before(resolved.StartedAt) {
		t.Fatalf("source resolution precedes local opening: %+v", resolved)
	}
	all, err := s.AlertsOf(AlertQuery{ChannelID: channel.ID})
	if err != nil || len(all) != 3 {
		t.Fatalf("history = %+v %v", all, err)
	}
}
