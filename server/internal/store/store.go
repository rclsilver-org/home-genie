// Package store is the only place that speaks SQL. Everything above it works
// on the types declared here, which keeps a later move to another database
// contained.
package store

import (
	"database/sql"
	"errors"
	"time"
)

// ErrNotFound is returned instead of sql.ErrNoRows, so callers do not have
// to import database/sql to tell "absent" from "broken".
var ErrNotFound = errors.New("not found")

// ErrConflict signals a uniqueness violation — a username or a slug already
// taken.
var ErrConflict = errors.New("already exists")

// Store wraps the database handle.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

// New returns a Store reading the wall clock.
func New(db *sql.DB) *Store {
	return &Store{db: db, now: func() time.Time { return time.Now().UTC() }}
}

// WithClock returns a Store whose notion of time is fixed, for tests and for
// the reminder scheduler.
func (s *Store) WithClock(now func() time.Time) *Store {
	return &Store{db: s.db, now: now}
}

// DB exposes the handle for the health check.
func (s *Store) DB() *sql.DB { return s.db }

// timestamp formats an instant the way the schema stores it.
func (s *Store) timestamp() string { return s.now().Format(time.RFC3339) }

func parseTime(value string) (time.Time, error) {
	return time.Parse(time.RFC3339, value)
}

// nullString turns an optional column into a Go string.
func nullString(value sql.NullString) string {
	if value.Valid {
		return value.String
	}
	return ""
}
