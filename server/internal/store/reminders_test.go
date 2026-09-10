package store

import (
	"errors"
	"testing"
	"time"
)

func TestPolicyResolutionPrefersTheChannelOverride(t *testing.T) {
	s := newTestStore(t)
	owner, _ := s.CreateLocalUser("thomas", "", "hash", true)
	channel, _ := s.CreateChannel("alerts-critical", "", "", owner.ID)
	other, _ := s.CreateChannel("homelab-info", "", "", owner.ID)

	// A default for the severity, then an override on a single channel.
	if err := s.SetReminderPolicy(ReminderPolicy{
		Severity: "critical", Interval: time.Hour, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetReminderPolicy(ReminderPolicy{
		ChannelID: &channel.ID, Severity: "critical",
		Interval: 15 * time.Minute, Enabled: true}); err != nil {
		t.Fatal(err)
	}

	applied, err := s.ReminderPolicyFor(channel.ID, "critical")
	if err != nil {
		t.Fatal(err)
	}
	if applied.Interval != 15*time.Minute {
		t.Fatalf("interval = %s, want 15m — the override did not win", applied.Interval)
	}

	// The other channel falls back to the default.
	fallback, err := s.ReminderPolicyFor(other.ID, "critical")
	if err != nil {
		t.Fatal(err)
	}
	if fallback.Interval != time.Hour {
		t.Fatalf("intervalle = %s, want 1h", fallback.Interval)
	}
}

func TestNoPolicyMeansNoReminder(t *testing.T) {
	s := newTestStore(t)
	owner, _ := s.CreateLocalUser("thomas", "", "hash", true)
	channel, _ := s.CreateChannel("alerts", "", "", owner.ID)

	if _, err := s.ReminderPolicyFor(channel.ID, "critical"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	// A disabled policy, or one with a zero interval, does not remind either.
	for _, policy := range []ReminderPolicy{
		{Severity: "warning", Interval: time.Hour, Enabled: false},
		{Severity: "info", Interval: 0, Enabled: true},
	} {
		if policy.Reminds() {
			t.Errorf("%+v should not remind", policy)
		}
	}
}

func TestSetPolicyReplacesRatherThanDuplicates(t *testing.T) {
	s := newTestStore(t)
	owner, _ := s.CreateLocalUser("thomas", "", "hash", true)
	channel, _ := s.CreateChannel("alerts", "", "", owner.ID)

	for _, interval := range []time.Duration{time.Hour, 10 * time.Minute} {
		if err := s.SetReminderPolicy(ReminderPolicy{
			Severity: "critical", Interval: interval, Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}

	policies, err := s.ReminderPoliciesOf(channel.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(policies) != 1 {
		t.Fatalf("%d policies, want 1 — the second call duplicated", len(policies))
	}
	if policies[0].Interval != 10*time.Minute {
		t.Fatalf("intervalle = %s", policies[0].Interval)
	}
}

// Quiet hours postpone, they do not drop: an alert nobody has
// acknowledged must resurface when the window closes.
func TestQuietHoursPostponeRatherThanDrop(t *testing.T) {
	nuit := ReminderPolicy{
		Severity: "warning", Interval: 30 * time.Minute, Enabled: true,
		QuietFrom: "23:00", QuietTo: "07:00",
	}

	// A reminder falling at 02:30: pushed to 07:00 the same day.
	at := time.Date(2026, 9, 10, 2, 0, 0, 0, time.UTC)
	next := nuit.NextAfter(at)
	if next.Hour() != 7 || next.Minute() != 0 || next.Day() != 10 {
		t.Fatalf("next = %s, want the 10th at 07:00", next)
	}

	// A reminder falling at 23:30: pushed to 07:00 the next day.
	at = time.Date(2026, 9, 10, 23, 0, 0, 0, time.UTC)
	next = nuit.NextAfter(at)
	if next.Hour() != 7 || next.Day() != 11 {
		t.Fatalf("next = %s, want the 11th at 07:00", next)
	}

	// In the middle of the day, the window does not interfere.
	at = time.Date(2026, 9, 10, 14, 0, 0, 0, time.UTC)
	next = nuit.NextAfter(at)
	if !next.Equal(at.Add(30 * time.Minute)) {
		t.Fatalf("next = %s, want 14:30", next)
	}
}

// A window inside the day must not behave like a night.
func TestQuietWindowWithinTheDay(t *testing.T) {
	reunion := ReminderPolicy{
		Severity: "info", Interval: 10 * time.Minute, Enabled: true,
		QuietFrom: "09:00", QuietTo: "12:00",
	}

	at := time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC)
	if next := reunion.NextAfter(at); next.Hour() != 12 {
		t.Fatalf("next = %s, want 12:00", next)
	}

	at = time.Date(2026, 9, 10, 13, 0, 0, 0, time.UTC)
	if next := reunion.NextAfter(at); next.Hour() != 13 || next.Minute() != 10 {
		t.Fatalf("next = %s, want 13:10", next)
	}
}

func TestMalformedQuietHoursAreIgnored(t *testing.T) {
	policy := ReminderPolicy{
		Severity: "warning", Interval: time.Hour, Enabled: true,
		QuietFrom: "anything at all", QuietTo: "07:00",
	}
	at := time.Date(2026, 9, 10, 2, 0, 0, 0, time.UTC)
	if next := policy.NextAfter(at); !next.Equal(at.Add(time.Hour)) {
		t.Fatalf("next = %s — an unreadable window must not shift it", next)
	}
}

// Due reminders are read from the database, not from memory: that is what
// makes them survive a restart of the service.
func TestDueRemindersSelectsOnlyWhatIsDue(t *testing.T) {
	s := newTestStore(t)
	owner, _ := s.CreateLocalUser("thomas", "", "hash", true)
	channel, _ := s.CreateChannel("alerts", "", "", owner.ID)
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)

	create := func(fingerprint string) Alert {
		alert, err := s.CreateAlert(NewAlert{
			ChannelID: channel.ID, Fingerprint: fingerprint,
			Severity: "critical", StartedAt: now.Add(-time.Hour)})
		if err != nil {
			t.Fatal(err)
		}
		return alert
	}

	due := create("due")
	future := create("future")
	acked := create("acked")
	resolved := create("resolved")

	past := now.Add(-time.Minute)
	later := now.Add(time.Hour)
	schedule := []struct {
		alert Alert
		at    *time.Time
	}{
		{due, &past}, {future, &later}, {acked, &past}, {resolved, &past},
	}
	for _, entry := range schedule {
		if err := s.SetNextReminder(entry.alert.ID, entry.at, 0); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.AckAlert(acked.ID, owner.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.ResolveAlert(resolved.ID, now); err != nil {
		t.Fatal(err)
	}

	alerts, err := s.DueReminders(now, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 1 || alerts[0].ID != due.ID {
		t.Fatalf("due = %+v, want only the alert named due", alerts)
	}
}

// Acknowledging cancels the reminder: that is the whole difference between
// "I am on it" and "leave me alone".
func TestAckCancelsTheReminder(t *testing.T) {
	s := newTestStore(t)
	owner, _ := s.CreateLocalUser("thomas", "", "hash", true)
	channel, _ := s.CreateChannel("alerts", "", "", owner.ID)

	alert, err := s.CreateAlert(NewAlert{
		ChannelID: channel.ID, Fingerprint: "abc", Severity: "critical"})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Add(time.Minute)
	if err := s.SetNextReminder(alert.ID, &at, 3); err != nil {
		t.Fatal(err)
	}

	if _, err := s.AckAlert(alert.ID, owner.ID); err != nil {
		t.Fatal(err)
	}

	refreshed, err := s.AlertByID(alert.ID)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.NextReminderAt != nil {
		t.Fatal("the reminder survives the acknowledgement")
	}
	if !refreshed.IsOpen() {
		t.Fatal("acknowledging closed the alert")
	}
}
