package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// QuietHours is a window during which a channel must not make noise.
//
// A row with no channel is the global default; one naming a channel overrides
// it. Within each level, a row naming a severity beats the one that does not.
// The most specific wins — the same rule as the reminder cadences, and for
// the same reason: one sets the night once, and argues about a channel only
// when that channel deserves it.
type QuietHours struct {
	// ChannelID nil means the global default.
	ChannelID *int64
	Severity  string
	// From and To bound the window as "HH:MM" in local time. It may straddle
	// midnight, which is the normal case for a night.
	From string
	To   string
}

// IsDefault reports whether the window is the global one.
func (q QuietHours) IsDefault() bool { return q.ChannelID == nil }

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
		 ON CONFLICT (IFNULL(channel_id, -1), severity) DO UPDATE SET
		   quiet_from = excluded.quiet_from,
		   quiet_to = excluded.quiet_to`,
		window.ChannelID, window.Severity, window.From, window.To); err != nil {
		return fmt.Errorf("recording the quiet hours: %w", err)
	}
	return nil
}

// DeleteQuietHours removes a window. Removing one that does not exist is not
// an error: the caller asked for silence to stop, and it has.
func (s *Store) DeleteQuietHours(channelID *int64, severity string) error {
	if _, err := s.db.Exec(
		`DELETE FROM quiet_hours
		  WHERE IFNULL(channel_id, -1) = IFNULL(?, -1) AND severity = ?`,
		channelID, severity); err != nil {
		return fmt.Errorf("deleting the quiet hours: %w", err)
	}
	return nil
}

// QuietHoursFor resolves the window that applies to a channel and a severity,
// most specific first: this channel and this severity, then this channel,
// then the global default for this severity, then the global default.
//
// With one exception: a window that does not name a severity never covers a
// critical alert. Silencing a critical is a legitimate thing to want — on a
// homelab a disk filling up at three in the morning can wait until seven —
// but it must be asked for by name, not inherited from a window set with the
// *arr suite in mind. The broad gesture stays safe; the dangerous one stays
// deliberate.
//
// Returns a zero value rather than ErrNotFound: having no quiet hours is the
// ordinary case, not a failure to look one up.
func (s *Store) QuietHoursFor(channelID int64, severity string) (QuietHours, error) {
	severities := []any{severity, ""}
	condition := "(severity = ? OR severity = ?)"
	if severity == SeverityCritical {
		severities = []any{severity}
		condition = "severity = ?"
	}

	args := append([]any{channelID}, severities...)
	row := s.db.QueryRow(
		`SELECT channel_id, severity, quiet_from, quiet_to
		   FROM quiet_hours
		  WHERE (channel_id = ? OR channel_id IS NULL) AND `+condition+`
		  ORDER BY channel_id IS NULL, severity = ''
		  LIMIT 1`, args...)

	var (
		window  QuietHours
		channel sql.NullInt64
	)
	err := row.Scan(&channel, &window.Severity, &window.From, &window.To)
	if errors.Is(err, sql.ErrNoRows) {
		return QuietHours{}, nil
	}
	if err != nil {
		return QuietHours{}, fmt.Errorf("reading the quiet hours: %w", err)
	}
	if channel.Valid {
		value := channel.Int64
		window.ChannelID = &value
	}
	return window, nil
}

// QuietHoursOf lists the windows visible for a channel — its own and the
// global defaults it would otherwise inherit — or only the global ones when
// given nil, which is what the settings screen shows.
func (s *Store) QuietHoursOf(channelID *int64) ([]QuietHours, error) {
	sqlText := `SELECT channel_id, severity, quiet_from, quiet_to
	              FROM quiet_hours WHERE channel_id IS NULL`
	args := []any{}
	if channelID != nil {
		sqlText = `SELECT channel_id, severity, quiet_from, quiet_to
		             FROM quiet_hours WHERE channel_id = ? OR channel_id IS NULL`
		args = append(args, *channelID)
	}
	sqlText += ` ORDER BY channel_id IS NULL, severity = '' DESC, severity`

	rows, err := s.db.Query(sqlText, args...)
	if err != nil {
		return nil, fmt.Errorf("listing the quiet hours: %w", err)
	}
	defer rows.Close()

	windows := []QuietHours{}
	for rows.Next() {
		var (
			window  QuietHours
			channel sql.NullInt64
		)
		if err := rows.Scan(&channel, &window.Severity,
			&window.From, &window.To); err != nil {
			return nil, fmt.Errorf("reading a window: %w", err)
		}
		if channel.Valid {
			value := channel.Int64
			window.ChannelID = &value
		}
		windows = append(windows, window)
	}
	return windows, rows.Err()
}
