// Package db opens the SQLite database and keeps its schema up to date.
//
// The driver is modernc.org/sqlite, in pure Go: it is what lets the whole
// build stay CGO-free and cross-compile to arm64 without a toolchain.
package db

import (
	"database/sql"
	"fmt"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// pragmas applied to every connection.
//
// WAL keeps a reader from blocking the reminder scheduler while it writes.
// busy_timeout turns the "database is locked" race into a short wait, which
// is the sane behaviour for a single-process server. foreign_keys is off by
// default in SQLite and has to be asked for.
var pragmas = []string{
	"PRAGMA journal_mode = WAL",
	"PRAGMA synchronous = NORMAL",
	"PRAGMA foreign_keys = ON",
	"PRAGMA busy_timeout = 5000",
}

// Open opens the database at path, applies the pragmas and migrates the
// schema to the latest version.
func Open(path string) (*sql.DB, error) {
	handle, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}

	// One writer at a time. SQLite allows a single write transaction anyway,
	// and serialising here avoids spurious busy errors under concurrency.
	handle.SetMaxOpenConns(1)

	for _, pragma := range pragmas {
		if _, err := handle.Exec(pragma); err != nil {
			handle.Close()
			return nil, fmt.Errorf("%s: %w", pragma, err)
		}
	}

	if err := Migrate(handle, path); err != nil {
		handle.Close()
		return nil, err
	}

	return handle, nil
}

// OpenInMemory opens a throwaway migrated database, for tests.
func OpenInMemory() (*sql.DB, error) {
	return Open("file::memory:?cache=shared")
}

// Path returns the absolute path of a database file, for logging.
func Path(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}
