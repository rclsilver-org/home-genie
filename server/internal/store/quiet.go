package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// QuietHours is a window during which a channel must not make noise.
//
// A row with an empty severity covers the whole channel; one naming a
// severity overrides it for that severity alone. The most specific wins, as
// for the reminder cadences.
type QuietHours struct {
	ChannelID int64
	Severity  string
	// From and To bound the window as "HH:MM" in local time. It may straddle
	// midnight, which is the normal case for a night.
	From string
	To   string
}

// Set reports whether the window is usable at all.
func (q QuietHours) Set() bool { return q.From != "" && q.To != "" }

// Covers reports whether an instant falls inside the window.
func (q QuietHours) Covers(at time.Time) bool {
	if !q.Set() {
		return false
	}
	_, inside := quietWindowEnd(at, q.From, q.To)
	return inside
}

// EndAfter returns when the window containing at closes, and whether at was
// inside one at all.
func (q QuietHours) EndAfter(at time.Time) (time.Time, bool) {
	if !q.Set() {
		return time.Time{}, false
	}
	return quietWindowEnd(at, q.From, q.To)
}

// SetQuietHours creates or replaces the window for a scope.
func (s *Store) SetQuietHours(window QuietHours) error {
	if _, ok := parseClock(window.From); !ok {
		return fmt.Errorf("quiet_from must be HH:MM")
	}
	if _, ok := parseClock(window.To); !ok {
		return fmt.Errorf("quiet_to must be HH:MM")
	}
	if _, err := s.db.Exec(
		`INSERT INTO quiet_hours (channel_id, severity, quiet_from, quiet_to)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT (channel_id, severity) DO UPDATE SET
		   quiet_from = excluded.quiet_from,
		   quiet_to = excluded.quiet_to`,
		window.ChannelID, window.Severity, window.From, window.To); err != nil {
		return fmt.Errorf("recording the quiet hours: %w", err)
	}
	return nil
}

// DeleteQuietHours removes a window. Removing one that does not exist is not
// an error: the caller asked for silence to stop, and it has.
func (s *Store) DeleteQuietHours(channelID int64, severity string) error {
	if _, err := s.db.Exec(
		`DELETE FROM quiet_hours WHERE channel_id = ? AND severity = ?`,
		channelID, severity); err != nil {
		return fmt.Errorf("deleting the quiet hours: %w", err)
	}
	return nil
}

// QuietHoursFor resolves the window that applies: the one naming the
// severity if there is one, otherwise the channel-wide one, otherwise none.
//
// With one exception: a channel-wide window does not cover critical alerts.
// Silencing a critical is a legitimate thing to want — on a homelab a disk
// filling up at three in the morning can wait until seven — but it must be
// asked for by name, not inherited from a window set with the *arr suite in
// mind. The broad gesture stays safe; the dangerous one stays deliberate.
//
// Returns a zero value rather than ErrNotFound: having no quiet hours is the
// ordinary case, not a failure to look one up.
func (s *Store) QuietHoursFor(channelID int64, severity string) (QuietHours, error) {
	condition, args := "(severity = ? OR severity = '')", []any{channelID, severity}
	if severity == SeverityCritical {
		condition, args = "severity = ?", []any{channelID, severity}
	}

	row := s.db.QueryRow(
		`SELECT channel_id, severity, quiet_from, quiet_to
		   FROM quiet_hours
		  WHERE channel_id = ? AND `+condition+`
		  ORDER BY severity = ''
		  LIMIT 1`, args...)

	var window QuietHours
	err := row.Scan(&window.ChannelID, &window.Severity, &window.From, &window.To)
	if errors.Is(err, sql.ErrNoRows) {
		return QuietHours{}, nil
	}
	if err != nil {
		return QuietHours{}, fmt.Errorf("reading the quiet hours: %w", err)
	}
	return window, nil
}

// QuietHoursOf lists a channel's windows, the channel-wide one first.
func (s *Store) QuietHoursOf(channelID int64) ([]QuietHours, error) {
	rows, err := s.db.Query(
		`SELECT channel_id, severity, quiet_from, quiet_to
		   FROM quiet_hours WHERE channel_id = ?
		  ORDER BY severity = '' DESC, severity`, channelID)
	if err != nil {
		return nil, fmt.Errorf("listing the quiet hours: %w", err)
	}
	defer rows.Close()

	windows := []QuietHours{}
	for rows.Next() {
		var window QuietHours
		if err := rows.Scan(&window.ChannelID, &window.Severity,
			&window.From, &window.To); err != nil {
			return nil, fmt.Errorf("reading a window: %w", err)
		}
		windows = append(windows, window)
	}
	return windows, rows.Err()
}
