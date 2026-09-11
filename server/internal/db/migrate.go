package db

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

// Migrations are embedded so the binary is self-sufficient: the Debian
// package ships one file and nothing has to be deployed alongside it.
//
//go:embed migrations/*.sql
var migrations embed.FS

// Migrate brings the schema to the latest version. It is idempotent, so it
// runs unconditionally at startup.
//
// The driver is golang-migrate's `sqlite`, not `sqlite3`: the latter goes
// through mattn/go-sqlite3 and would drag CGO back in.
func Migrate(handle *sql.DB, name string) error {
	source, err := iofs.New(migrations, "migrations")
	if err != nil {
		return fmt.Errorf("reading the embedded migrations: %w", err)
	}

	driver, err := sqlite.WithInstance(handle, &sqlite.Config{})
	if err != nil {
		return fmt.Errorf("preparing the migration driver: %w", err)
	}

	migrator, err := migrate.NewWithInstance("iofs", source, name, driver)
	if err != nil {
		return fmt.Errorf("preparing the migrator: %w", err)
	}

	if err := migrator.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrating the schema: %w", err)
	}

	return nil
}

// Version reports the current schema version, and whether the last migration
// left the database dirty — which means a migration failed halfway and needs
// a human.
func Version(handle *sql.DB, name string) (version int, dirty bool, err error) {
	driver, err := sqlite.WithInstance(handle, &sqlite.Config{})
	if err != nil {
		return 0, false, fmt.Errorf("preparing the migration driver: %w", err)
	}
	return driver.Version()
}

// MigrateTo brings the schema to a given version, up or down. It exists for
// the tests, which need to populate an old schema and then migrate over it:
// a migration that silently drops data looks exactly like a correct one when
// the database is empty.
func MigrateTo(handle *sql.DB, name string, version uint) error {
	source, err := iofs.New(migrations, "migrations")
	if err != nil {
		return fmt.Errorf("reading the embedded migrations: %w", err)
	}
	driver, err := sqlite.WithInstance(handle, &sqlite.Config{})
	if err != nil {
		return fmt.Errorf("preparing the migration driver: %w", err)
	}
	migrator, err := migrate.NewWithInstance("iofs", source, name, driver)
	if err != nil {
		return fmt.Errorf("preparing the migrator: %w", err)
	}
	if err := migrator.Migrate(version); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrating to %d: %w", version, err)
	}
	return nil
}
