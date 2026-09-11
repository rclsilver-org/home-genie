package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ReminderPolicy is a cadence. A row with no channel is the default for a
// severity; a row with one overrides it. The most specific wins.
type ReminderPolicy struct {
	ID        int64
	ChannelID *int64
	Severity  string
	Interval  time.Duration
	Enabled   bool
}

// Reminds reports whether the policy produces reminders at all.
func (p ReminderPolicy) Reminds() bool { return p.Enabled && p.Interval > 0 }

// NextAfter returns when the next reminder is due, pushed past the quiet
// window when it lands inside one.
//
// Pushed and not dropped: an alert nobody acknowledged must resurface when
// the quiet hours end. Silently skipping the reminder would turn a night
// setting into a way of losing an alert — which is the difference between
// quiet hours and a mute.
func (p ReminderPolicy) NextAfter(from time.Time, quiet QuietHours) time.Time {
	next := from.Add(p.Interval)
	if end, inside := quiet.EndAfter(next); inside {
		return end
	}
	return next
}

// quietWindowEnd reports whether at falls in the quiet window and, if so,
// when the window ends. The window may straddle midnight, which is the
// normal case for a night.
func quietWindowEnd(at time.Time, from, to string) (time.Time, bool) {
	fromMinutes, ok := parseClock(from)
	if !ok {
		return time.Time{}, false
	}
	toMinutes, ok := parseClock(to)
	if !ok {
		return time.Time{}, false
	}
	if fromMinutes == toMinutes {
		return time.Time{}, false
	}

	minutes := at.Hour()*60 + at.Minute()
	end := time.Date(at.Year(), at.Month(), at.Day(),
		toMinutes/60, toMinutes%60, 0, 0, at.Location())

	if fromMinutes < toMinutes {
		// A window inside the day, for instance 09:00-17:00.
		if minutes >= fromMinutes && minutes < toMinutes {
			return end, true
		}
		return time.Time{}, false
	}

	// A window straddling midnight, for instance 23:00-07:00.
	if minutes >= fromMinutes {
		return end.AddDate(0, 0, 1), true
	}
	if minutes < toMinutes {
		return end, true
	}
	return time.Time{}, false
}

func parseClock(value string) (int, bool) {
	var hour, minute int
	if _, err := fmt.Sscanf(value, "%d:%d", &hour, &minute); err != nil {
		return 0, false
	}
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, false
	}
	return hour*60 + minute, true
}

// SetReminderPolicy creates or replaces a policy for a scope.
func (s *Store) SetReminderPolicy(policy ReminderPolicy) error {
	if policy.Severity == "" {
		return fmt.Errorf("a policy applies to a severity")
	}
	if _, err := s.db.Exec(
		`INSERT INTO reminder_policies (channel_id, severity, interval_seconds, enabled)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT (IFNULL(channel_id, -1), severity) DO UPDATE SET
		   interval_seconds = excluded.interval_seconds,
		   enabled = excluded.enabled`,
		policy.ChannelID, policy.Severity, int(policy.Interval.Seconds()),
		boolToInt(policy.Enabled)); err != nil {
		return fmt.Errorf("recording the policy: %w", err)
	}
	return nil
}

// ReminderPolicyFor resolves the cadence that applies to an alert: the
// channel override if there is one, otherwise the severity default,
// otherwise nothing.
func (s *Store) ReminderPolicyFor(channelID int64, severity string) (ReminderPolicy, error) {
	// ORDER BY puts the channel-specific row first, so LIMIT 1 implements
	// "the most specific wins" without a second query.
	row := s.db.QueryRow(
		`SELECT id, channel_id, severity, interval_seconds, enabled
		   FROM reminder_policies
		  WHERE severity = ? AND (channel_id = ? OR channel_id IS NULL)
		  ORDER BY channel_id IS NULL
		  LIMIT 1`, severity, channelID)

	var (
		policy  ReminderPolicy
		channel sql.NullInt64
		seconds int
		enabled int
	)
	err := row.Scan(&policy.ID, &channel, &policy.Severity, &seconds, &enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return ReminderPolicy{}, ErrNotFound
	}
	if err != nil {
		return ReminderPolicy{}, fmt.Errorf("reading the policy: %w", err)
	}

	if channel.Valid {
		value := channel.Int64
		policy.ChannelID = &value
	}
	policy.Interval = time.Duration(seconds) * time.Second
	policy.Enabled = enabled == 1

	return policy, nil
}

// ReminderPoliciesOf lists the policies visible for a channel: its own
// overrides and the defaults.
func (s *Store) ReminderPoliciesOf(channelID int64) ([]ReminderPolicy, error) {
	rows, err := s.db.Query(
		`SELECT id, channel_id, severity, interval_seconds, enabled
		   FROM reminder_policies
		  WHERE channel_id = ? OR channel_id IS NULL
		  ORDER BY channel_id IS NULL, severity`, channelID)
	if err != nil {
		return nil, fmt.Errorf("listing the policies: %w", err)
	}
	defer rows.Close()

	policies := []ReminderPolicy{}
	for rows.Next() {
		var (
			policy  ReminderPolicy
			channel sql.NullInt64
			seconds int
			enabled int
		)
		if err := rows.Scan(&policy.ID, &channel, &policy.Severity, &seconds,
			&enabled); err != nil {
			return nil, fmt.Errorf("reading a policy: %w", err)
		}
		if channel.Valid {
			value := channel.Int64
			policy.ChannelID = &value
		}
		policy.Interval = time.Duration(seconds) * time.Second
		policy.Enabled = enabled == 1
		policies = append(policies, policy)
	}
	return policies, rows.Err()
}

// DueReminders returns the firing, unacknowledged alerts whose reminder is
// due. Reading them from the database rather than holding timers in memory
// is what makes reminders survive a restart of the service.
func (s *Store) DueReminders(now time.Time, limit int) ([]Alert, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(alertColumns+`
		  WHERE a.status = ? AND a.acked_at IS NULL
		    AND a.next_reminder_at IS NOT NULL AND a.next_reminder_at <= ?
		  ORDER BY a.next_reminder_at
		  LIMIT ?`, AlertFiring, now.UTC().Format(time.RFC3339), limit)
	if err != nil {
		return nil, fmt.Errorf("listing the due reminders: %w", err)
	}
	defer rows.Close()

	alerts := []Alert{}
	for rows.Next() {
		alert, err := scanAlertRow(rows)
		if err != nil {
			return nil, err
		}
		alerts = append(alerts, alert)
	}
	return alerts, rows.Err()
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}
