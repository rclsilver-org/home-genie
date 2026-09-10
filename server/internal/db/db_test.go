package db

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func openTemp(t *testing.T) *sql.DB {
	t.Helper()
	handle, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { handle.Close() })
	return handle
}

func TestOpenMigratesAndIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")

	first, err := Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	version, dirty, err := Version(first, path)
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if dirty {
		t.Fatal("the schema is dirty after a fresh migration")
	}
	if version == 0 {
		t.Fatal("no migration was applied")
	}
	first.Close()

	// Reopening must be a no-op, since Migrate runs on every startup.
	second, err := Open(path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer second.Close()

	again, dirty, err := Version(second, path)
	if err != nil {
		t.Fatalf("Version after reopen: %v", err)
	}
	if dirty {
		t.Fatal("the schema is dirty after reopening")
	}
	if again != version {
		t.Fatalf("version changed on reopen: %d then %d", version, again)
	}
}

// SQLite ignores foreign keys unless asked, so this checks the pragma is
// really in force rather than merely declared in the schema.
func TestForeignKeysAreEnforced(t *testing.T) {
	handle := openTemp(t)

	_, err := handle.Exec(
		`INSERT INTO devices (user_id, name, platform, token_hash, created_at)
		 VALUES (999, 'phone', 'android', 'hash', ?)`, now())
	if err == nil {
		t.Fatal("a device pointing at a missing user was accepted")
	}
}

func TestCascadeDeletesMemberships(t *testing.T) {
	handle := openTemp(t)
	userID := insertUser(t, handle, "thomas")
	channelID := insertChannel(t, handle, "alerts-critical")

	if _, err := handle.Exec(
		`INSERT INTO channel_members (channel_id, user_id, role, created_at)
		 VALUES (?, ?, 'owner', ?)`, channelID, userID, now()); err != nil {
		t.Fatalf("inserting the membership: %v", err)
	}

	if _, err := handle.Exec(`DELETE FROM channels WHERE id = ?`, channelID); err != nil {
		t.Fatalf("deleting the channel: %v", err)
	}

	var remaining int
	if err := handle.QueryRow(`SELECT COUNT(*) FROM channel_members`).Scan(&remaining); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("%d membership(s) survived the channel", remaining)
	}
}

func TestRoleIsConstrained(t *testing.T) {
	handle := openTemp(t)
	userID := insertUser(t, handle, "thomas")
	channelID := insertChannel(t, handle, "homelab-info")

	_, err := handle.Exec(
		`INSERT INTO channel_members (channel_id, user_id, role, created_at)
		 VALUES (?, ?, 'admin', ?)`, channelID, userID, now())
	if err == nil {
		t.Fatal("an unknown role was accepted")
	}
}

func TestMessageEventKindIsConstrained(t *testing.T) {
	handle := openTemp(t)
	userID := insertUser(t, handle, "thomas")
	channelID := insertChannel(t, handle, "mediacenter")
	messageID := insertMessage(t, handle, channelID)

	if _, err := handle.Exec(
		`INSERT INTO message_events (message_id, user_id, kind, at)
		 VALUES (?, ?, 'delivered', ?)`, messageID, userID, now()); err != nil {
		t.Fatalf("a valid event kind was refused: %v", err)
	}

	if _, err := handle.Exec(
		`INSERT INTO message_events (message_id, user_id, kind, at)
		 VALUES (?, ?, 'teleported', ?)`, messageID, userID, now()); err == nil {
		t.Fatal("an unknown event kind was accepted")
	}
}

// Read state is per user: the point of the whole shared-feed model.
func TestReadStateIsPerUser(t *testing.T) {
	handle := openTemp(t)
	thomas := insertUser(t, handle, "thomas")
	other := insertUser(t, handle, "other")
	channelID := insertChannel(t, handle, "mediacenter")
	messageID := insertMessage(t, handle, channelID)

	if _, err := handle.Exec(
		`INSERT INTO message_reads (message_id, user_id, read_at) VALUES (?, ?, ?)`,
		messageID, thomas, now()); err != nil {
		t.Fatalf("marking as read: %v", err)
	}

	var readByOther int
	if err := handle.QueryRow(
		`SELECT COUNT(*) FROM message_reads WHERE message_id = ? AND user_id = ?`,
		messageID, other).Scan(&readByOther); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if readByOther != 0 {
		t.Fatal("the message is marked read for a user who never read it")
	}
}

// The seq the clients resynchronise on must never go backwards, even after
// rows are deleted — hence AUTOINCREMENT rather than a plain rowid.
func TestEventSeqIsMonotonic(t *testing.T) {
	handle := openTemp(t)
	userID := insertUser(t, handle, "thomas")

	insertEvent := func() int64 {
		t.Helper()
		result, err := handle.Exec(
			`INSERT INTO events (user_id, kind, created_at) VALUES (?, 'message.new', ?)`,
			userID, now())
		if err != nil {
			t.Fatalf("inserting the event: %v", err)
		}
		seq, err := result.LastInsertId()
		if err != nil {
			t.Fatalf("LastInsertId: %v", err)
		}
		return seq
	}

	first := insertEvent()
	second := insertEvent()
	if second <= first {
		t.Fatalf("seq did not increase: %d then %d", first, second)
	}

	if _, err := handle.Exec(`DELETE FROM events`); err != nil {
		t.Fatalf("emptying the table: %v", err)
	}

	third := insertEvent()
	if third <= second {
		t.Fatalf("seq was reused after a delete: %d then %d", second, third)
	}
}

// A default policy and a channel override must coexist; two defaults for the
// same severity must not.
func TestReminderPolicyScopeIsUnique(t *testing.T) {
	handle := openTemp(t)
	channelID := insertChannel(t, handle, "alerts-critical")

	insert := func(channel any) error {
		_, err := handle.Exec(
			`INSERT INTO reminder_policies (channel_id, severity, interval_seconds)
			 VALUES (?, 'critical', 900)`, channel)
		return err
	}

	if err := insert(nil); err != nil {
		t.Fatalf("the default policy was refused: %v", err)
	}
	if err := insert(channelID); err != nil {
		t.Fatalf("the channel override was refused: %v", err)
	}
	if err := insert(nil); err == nil {
		t.Fatal("a second default policy for the same severity was accepted")
	}
}

func now() string { return time.Now().UTC().Format(time.RFC3339) }

func insertUser(t *testing.T, handle *sql.DB, username string) int64 {
	t.Helper()
	result, err := handle.Exec(
		`INSERT INTO users (username, created_at) VALUES (?, ?)`, username, now())
	if err != nil {
		t.Fatalf("inserting the user %q: %v", username, err)
	}
	id, _ := result.LastInsertId()
	return id
}

func insertChannel(t *testing.T, handle *sql.DB, slug string) int64 {
	t.Helper()
	result, err := handle.Exec(
		`INSERT INTO channels (slug, name, created_at) VALUES (?, ?, ?)`, slug, slug, now())
	if err != nil {
		t.Fatalf("inserting the channel %q: %v", slug, err)
	}
	id, _ := result.LastInsertId()
	return id
}

func insertMessage(t *testing.T, handle *sql.DB, channelID int64) int64 {
	t.Helper()
	result, err := handle.Exec(
		`INSERT INTO messages (channel_id, title, created_at) VALUES (?, 'titre', ?)`,
		channelID, now())
	if err != nil {
		t.Fatalf("inserting the message: %v", err)
	}
	id, _ := result.LastInsertId()
	return id
}
