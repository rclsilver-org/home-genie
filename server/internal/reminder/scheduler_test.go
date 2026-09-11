package reminder

import (
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/rclsilver-org/home-genie/server/internal/db"
	"github.com/rclsilver-org/home-genie/server/internal/store"
)

// recorder captures the reminders instead of sending them.
type recorder struct {
	sent []struct {
		alertID int64
		count   int
	}
	fail error
}

func (r *recorder) RemindAlert(alert store.Alert, count int) error {
	r.sent = append(r.sent, struct {
		alertID int64
		count   int
	}{alert.ID, count})
	return r.fail
}

func newFixture(t *testing.T, now time.Time) (*store.Store, *recorder, *Scheduler, store.Channel) {
	t.Helper()

	handle, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { handle.Close() })

	repository := store.New(handle)
	owner, err := repository.CreateLocalUser("thomas", "", "hash", true)
	if err != nil {
		t.Fatal(err)
	}
	channel, err := repository.CreateChannel("alerts", "", "", owner.ID)
	if err != nil {
		t.Fatal(err)
	}

	notifier := &recorder{}
	scheduler := New(repository, notifier, slog.New(slog.NewTextHandler(io.Discard, nil))).
		WithClock(func() time.Time { return now })

	return repository, notifier, scheduler, channel
}

func openAlert(t *testing.T, s *store.Store, channelID int64, severity string, due *time.Time) store.Alert {
	t.Helper()
	alert, err := s.CreateAlert(store.NewAlert{
		ChannelID: channelID, Fingerprint: severity + "-fp", Severity: severity})
	if err != nil {
		t.Fatal(err)
	}
	if due != nil {
		if err := s.SetNextReminder(alert.ID, due, 0); err != nil {
			t.Fatal(err)
		}
	}
	return alert
}

func TestDueAlertIsRemindedAndRescheduled(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	repository, notifier, scheduler, channel := newFixture(t, now)

	if err := repository.SetReminderPolicy(store.ReminderPolicy{
		Severity: "critical", Interval: 15 * time.Minute, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	past := now.Add(-time.Minute)
	alert := openAlert(t, repository, channel.ID, "critical", &past)

	scheduler.RunOnce()

	if len(notifier.sent) != 1 || notifier.sent[0].alertID != alert.ID {
		t.Fatalf("envois = %+v", notifier.sent)
	}
	if notifier.sent[0].count != 1 {
		t.Fatalf("count = %d, want 1", notifier.sent[0].count)
	}

	refreshed, err := repository.AlertByID(alert.ID)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.ReminderCount != 1 {
		t.Fatalf("reminder_count = %d", refreshed.ReminderCount)
	}
	want := now.Add(15 * time.Minute)
	if refreshed.NextReminderAt == nil || !refreshed.NextReminderAt.Equal(want) {
		t.Fatalf("next reminder = %v, want %s", refreshed.NextReminderAt, want)
	}

	// A second pass at the same hour must send nothing more.
	scheduler.RunOnce()
	if len(notifier.sent) != 1 {
		t.Fatalf("%d sends after rescheduling, want 1", len(notifier.sent))
	}
}

// A policy removed while an alert is open must cancel the schedule, or the
// alert comes back on every tick.
func TestRemovingThePolicyCancelsTheSchedule(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	repository, notifier, scheduler, channel := newFixture(t, now)

	past := now.Add(-time.Minute)
	alert := openAlert(t, repository, channel.ID, "critical", &past)

	scheduler.RunOnce()

	if len(notifier.sent) != 0 {
		t.Fatalf("a reminder was sent with no policy: %+v", notifier.sent)
	}
	refreshed, _ := repository.AlertByID(alert.ID)
	if refreshed.NextReminderAt != nil {
		t.Fatal("the schedule survives the absence of a policy — the alert will come back on every tick")
	}
}

// A failed send must not silence the alert: it is rescheduled anyway,
// because losing an alert is the one unacceptable outcome.
func TestAFailedSendIsStillRescheduled(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	repository, notifier, scheduler, channel := newFixture(t, now)
	notifier.fail = io.ErrUnexpectedEOF

	if err := repository.SetReminderPolicy(store.ReminderPolicy{
		Severity: "critical", Interval: 15 * time.Minute, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	past := now.Add(-time.Minute)
	alert := openAlert(t, repository, channel.ID, "critical", &past)

	scheduler.RunOnce()

	refreshed, _ := repository.AlertByID(alert.ID)
	if refreshed.NextReminderAt == nil {
		t.Fatal("the alert was silenced by a transient failure")
	}
	if refreshed.ReminderCount != 1 {
		t.Fatalf("reminder_count = %d", refreshed.ReminderCount)
	}
}

// Quiet hours push the reminder out of the window.
func TestQuietHoursPushTheNextReminderOut(t *testing.T) {
	now := time.Date(2026, 9, 10, 2, 0, 0, 0, time.UTC)
	repository, notifier, scheduler, channel := newFixture(t, now)

	if err := repository.SetReminderPolicy(store.ReminderPolicy{
		Severity: "warning", Interval: 30 * time.Minute, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := repository.SetQuietHours(store.QuietHours{
		ChannelID: &channel.ID, Severity: "warning",
		From: "23:00", To: "07:00"}); err != nil {
		t.Fatal(err)
	}
	past := now.Add(-time.Minute)
	alert := openAlert(t, repository, channel.ID, "warning", &past)

	scheduler.RunOnce()

	// The due reminder is sent; it is the next one that is pushed out.
	if len(notifier.sent) != 1 {
		t.Fatalf("envois = %+v", notifier.sent)
	}
	refreshed, _ := repository.AlertByID(alert.ID)
	if refreshed.NextReminderAt == nil || refreshed.NextReminderAt.Hour() != 7 {
		t.Fatalf("next reminder = %v, want 07:00", refreshed.NextReminderAt)
	}
}

// The channel override wins over the severity default.
func TestChannelOverrideDrivesTheCadence(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	repository, _, scheduler, channel := newFixture(t, now)

	if err := repository.SetReminderPolicy(store.ReminderPolicy{
		Severity: "critical", Interval: time.Hour, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := repository.SetReminderPolicy(store.ReminderPolicy{
		ChannelID: &channel.ID, Severity: "critical",
		Interval: 5 * time.Minute, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	past := now.Add(-time.Minute)
	alert := openAlert(t, repository, channel.ID, "critical", &past)

	scheduler.RunOnce()

	refreshed, _ := repository.AlertByID(alert.ID)
	want := now.Add(5 * time.Minute)
	if refreshed.NextReminderAt == nil || !refreshed.NextReminderAt.Equal(want) {
		t.Fatalf("next reminder = %v, want %s", refreshed.NextReminderAt, want)
	}
}
