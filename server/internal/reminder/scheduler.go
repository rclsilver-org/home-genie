// Package reminder re-notifies alerts nobody acknowledged.
//
// The due alerts are read from the database on a tick rather than held as
// in-memory timers. Second-level precision is irrelevant for a reminder,
// whereas surviving a restart of the service is not: an alert that fired at
// 3 a.m. must keep insisting even if the server was upgraded at 4.
package reminder

import (
	"context"
	"log/slog"
	"time"

	"github.com/rclsilver-org/home-genie/server/internal/store"
)

// tick is how often the due alerts are looked up. Well under the shortest
// cadence anyone would set, and cheap: an indexed query on a table that
// holds a handful of open alerts.
const tick = 15 * time.Second

// batch bounds one pass, so a backlog after a long outage is worked
// through over several ticks instead of in one burst.
const batch = 20

// Notifier is what the scheduler calls to actually remind. The API layer
// implements it; keeping it an interface is what lets the scheduler be
// tested without an HTTP server.
type Notifier interface {
	RemindAlert(alert store.Alert, count int) error
}

// Scheduler drives the reminders.
type Scheduler struct {
	store    *store.Store
	notifier Notifier
	logger   *slog.Logger
	now      func() time.Time
}

// New builds a scheduler reading the wall clock.
func New(s *store.Store, notifier Notifier, logger *slog.Logger) *Scheduler {
	return &Scheduler{
		store: s, notifier: notifier, logger: logger,
		now: func() time.Time { return time.Now().UTC() },
	}
}

// WithClock fixes the notion of time, for tests.
func (s *Scheduler) WithClock(now func() time.Time) *Scheduler {
	s.now = now
	return s
}

// Run ticks until the context is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(tick)
	defer ticker.Stop()

	s.logger.Info("reminder scheduler started", "tick", tick)

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("reminder scheduler stopped")
			return
		case <-ticker.C:
			s.RunOnce()
		}
	}
}

// RunOnce processes the alerts whose reminder is due. Exported so a test
// can drive it without waiting on a ticker.
func (s *Scheduler) RunOnce() {
	now := s.now()

	alerts, err := s.store.DueReminders(now, batch)
	if err != nil {
		s.logger.Error("reading the due reminders", "error", err)
		return
	}

	for _, alert := range alerts {
		s.remind(alert, now)
	}
}

func (s *Scheduler) remind(alert store.Alert, now time.Time) {
	policy, err := s.store.ReminderPolicyFor(alert.ChannelID, alert.Severity)
	if err != nil || !policy.Reminds() {
		// The policy was removed or disabled while the alert was open.
		// Cancelling the schedule is right: leaving next_reminder_at in the
		// past would make this alert reappear on every single tick.
		if err := s.store.SetNextReminder(alert.ID, nil, alert.ReminderCount); err != nil {
			s.logger.Error("cancelling the reminder", "error", err, "alert_id", alert.ID)
		}
		return
	}

	count := alert.ReminderCount + 1
	if err := s.notifier.RemindAlert(alert, count); err != nil {
		s.logger.Error("sending the reminder", "error", err, "alert_id", alert.ID)
		// Reschedule anyway: dropping the alert on a transient failure
		// would silence it for good, which is the one outcome an alerting
		// system must never produce.
	}

	// The quiet window pushes the next reminder out; it does not cancel it.
	quiet, err := s.store.QuietHoursFor(alert.ChannelID, alert.Severity)
	if err != nil {
		s.logger.Error("reading the quiet hours", "error", err, "alert_id", alert.ID)
	}
	next := policy.NextAfter(now, quiet)
	if err := s.store.SetNextReminder(alert.ID, &next, count); err != nil {
		s.logger.Error("rescheduling the reminder", "error", err, "alert_id", alert.ID)
	}
}
