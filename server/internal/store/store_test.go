package store

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/rclsilver-org/home-genie/server/internal/db"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	handle, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("opening the database: %v", err)
	}
	t.Cleanup(func() { handle.Close() })
	return New(handle)
}

func TestCreateAndFetchLocalUser(t *testing.T) {
	s := newTestStore(t)

	created, err := s.CreateLocalUser("thomas", "Thomas", "hash-argon2", true)
	if err != nil {
		t.Fatalf("CreateLocalUser: %v", err)
	}
	if created.ID == 0 {
		t.Fatal("no identifier was assigned")
	}
	if !created.IsLocal() {
		t.Fatal("an account with a password is not reported as local")
	}
	if !created.IsAdmin {
		t.Fatal("the admin flag was lost")
	}

	fetched, err := s.UserByUsername("thomas")
	if err != nil {
		t.Fatalf("UserByUsername: %v", err)
	}
	if fetched.ID != created.ID || fetched.PasswordHash != "hash-argon2" || !fetched.IsAdmin {
		t.Fatalf("the account came back altered: %+v", fetched)
	}
}

func TestUserNotFound(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.UserByUsername("nobody"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestDuplicateUsernameIsAConflict(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateLocalUser("thomas", "", "hash", true); err != nil {
		t.Fatal(err)
	}
	_, err := s.CreateLocalUser("thomas", "", "other-hash", false)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestLocalUserNeedsAPassword(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateLocalUser("thomas", "", "", true); err == nil {
		t.Fatal("a local account without a password was accepted")
	}
}

func TestCountUsers(t *testing.T) {
	s := newTestStore(t)
	count, err := s.CountUsers()
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("a fresh database reports %d users", count)
	}

	if _, err := s.CreateLocalUser("thomas", "", "hash", true); err != nil {
		t.Fatal(err)
	}
	if count, _ = s.CountUsers(); count != 1 {
		t.Fatalf("after one creation, %d users", count)
	}
}

// A device resolves to its owner in one lookup, because every authenticated
// request needs both.
func TestDeviceByTokenHashReturnsItsOwner(t *testing.T) {
	s := newTestStore(t)
	user, err := s.CreateLocalUser("thomas", "Thomas", "hash", true)
	if err != nil {
		t.Fatal(err)
	}

	created, err := s.CreateDevice(user.ID, "phone", "android", "token-hash")
	if err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}
	if created.Transport != TransportWebSocket {
		t.Fatalf("transport = %q, want %q", created.Transport, TransportWebSocket)
	}
	if created.IsConnected() {
		t.Fatal("a freshly enrolled device is reported as connected")
	}

	device, owner, err := s.DeviceByTokenHash("token-hash")
	if err != nil {
		t.Fatalf("DeviceByTokenHash: %v", err)
	}
	if device.ID != created.ID {
		t.Fatal("the wrong device came back")
	}
	if owner.ID != user.ID || owner.Username != "thomas" {
		t.Fatalf("the wrong owner came back: %+v", owner)
	}
}

func TestUnknownTokenIsNotFound(t *testing.T) {
	s := newTestStore(t)
	if _, _, err := s.DeviceByTokenHash("unknown"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestTouchDeviceRecordsLastSeen(t *testing.T) {
	s := newTestStore(t)
	user, _ := s.CreateLocalUser("thomas", "", "hash", true)
	device, err := s.CreateDevice(user.ID, "phone", "android", "token-hash")
	if err != nil {
		t.Fatal(err)
	}
	if device.LastSeenAt != nil {
		t.Fatal("a new device already has a last-seen date")
	}

	if err := s.TouchDevice(device.ID); err != nil {
		t.Fatalf("TouchDevice: %v", err)
	}

	refreshed, _, err := s.DeviceByTokenHash("token-hash")
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.LastSeenAt == nil {
		t.Fatal("the last-seen date was not recorded")
	}
}

// The injectable clock is what lets the reminder scheduler and its tests
// avoid waiting for wall time.
func TestWithClockDrivesTimestamps(t *testing.T) {
	fixed := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	s := newTestStore(t).WithClock(func() time.Time { return fixed })

	user, err := s.CreateLocalUser("thomas", "", "hash", true)
	if err != nil {
		t.Fatal(err)
	}
	if !user.CreatedAt.Equal(fixed) {
		t.Fatalf("created_at = %s, want %s", user.CreatedAt, fixed)
	}
}
