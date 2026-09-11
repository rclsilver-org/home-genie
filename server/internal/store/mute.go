package store

import (
	"database/sql"
	"fmt"
	"time"
)

// MutedUntil returns when a user's own silence expires, or nil when they are
// not muted.
//
// Personal and not installation-wide: setting a mute means "do not disturb
// me", never "do not disturb anyone". Outside the channels, though: one does
// not fall silent per channel, one falls silent.
func (s *Store) MutedUntil(userID int64) (*time.Time, error) {
	var raw sql.NullString
	if err := s.db.QueryRow(`SELECT muted_until FROM users WHERE id = ?`, userID).
		Scan(&raw); err != nil {
		return nil, fmt.Errorf("reading the mute: %w", err)
	}
	return optionalTime(raw), nil
}

// IsMuted reports whether a user is silenced at the given instant. An expired
// value is not cleaned up: it says when the silence ended, which is worth
// more than a tidy column.
func (s *Store) IsMuted(userID int64, at time.Time) (bool, error) {
	until, err := s.MutedUntil(userID)
	if err != nil || until == nil {
		return false, err
	}
	return at.Before(*until), nil
}

// MutedUsers returns, among the given accounts, those silenced at that
// instant.
//
// Read in one query rather than one per recipient: a message is fanned out to
// every member of a channel, and each of them may be muted or not — that is
// the whole point of a personal mute.
func (s *Store) MutedUsers(userIDs []int64, at time.Time) (map[int64]bool, error) {
	muted := map[int64]bool{}
	if len(userIDs) == 0 {
		return muted, nil
	}

	rows, err := s.db.Query(
		`SELECT id, muted_until FROM users WHERE muted_until IS NOT NULL`)
	if err != nil {
		return nil, fmt.Errorf("reading the mutes: %w", err)
	}
	defer rows.Close()

	wanted := map[int64]bool{}
	for _, id := range userIDs {
		wanted[id] = true
	}
	for rows.Next() {
		var (
			id  int64
			raw sql.NullString
		)
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, fmt.Errorf("reading a mute: %w", err)
		}
		if !wanted[id] {
			continue
		}
		if until := optionalTime(raw); until != nil && at.Before(*until) {
			muted[id] = true
		}
	}
	return muted, rows.Err()
}

// SetMute silences a user until the given instant, or lifts their silence
// when given nil.
func (s *Store) SetMute(userID int64, until *time.Time) error {
	var value any
	if until != nil {
		value = until.UTC().Format(time.RFC3339)
	}
	if _, err := s.db.Exec(`UPDATE users SET muted_until = ? WHERE id = ?`,
		value, userID); err != nil {
		return fmt.Errorf("recording the mute: %w", err)
	}
	return nil
}
