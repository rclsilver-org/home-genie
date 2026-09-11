package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rclsilver-org/home-genie/server/internal/auth"
	"github.com/rclsilver-org/home-genie/server/internal/db"
	"github.com/rclsilver-org/home-genie/server/internal/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	handle, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("opening the database: %v", err)
	}
	t.Cleanup(func() { handle.Close() })
	return store.New(handle)
}

// withStdin replaces stdin with a pipe carrying content, which is the path
// the command takes when it is not attached to a terminal.
func withStdin(t *testing.T, content string) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		io.WriteString(writer, content)
		writer.Close()
	}()

	previous := os.Stdin
	os.Stdin = reader
	t.Cleanup(func() {
		os.Stdin = previous
		reader.Close()
	})
}

func TestCreateAdminCreatesALocalAccount(t *testing.T) {
	s := newTestStore(t)
	withStdin(t, "a-solid-password\n")

	var out bytes.Buffer
	if err := CreateAdmin(s, "thomas", "Thomas", &out); err != nil {
		t.Fatalf("CreateAdmin: %v", err)
	}

	user, err := s.UserByUsername("thomas")
	if err != nil {
		t.Fatalf("the account was not created: %v", err)
	}
	if !user.IsAdmin {
		t.Error("the account is not an administrator")
	}
	if !user.IsLocal() {
		t.Error("the account has no password, so it is not a break-glass one")
	}

	// The password must be verifiable, and stored hashed.
	if user.PasswordHash == "a-solid-password" {
		t.Fatal("the password is stored in clear")
	}
	ok, err := auth.VerifyPassword("a-solid-password", user.PasswordHash)
	if err != nil || !ok {
		t.Fatalf("the stored password does not verify: ok=%v err=%v", ok, err)
	}

	if !strings.Contains(out.String(), "thomas") {
		t.Errorf("the confirmation does not name the account: %q", out.String())
	}
}

// The trailing newline of a pipe must not become part of the password —
// otherwise the account is unusable from the app and nobody understands why.
func TestPasswordIgnoresTheTrailingNewline(t *testing.T) {
	s := newTestStore(t)
	withStdin(t, "a-solid-password\r\n")

	if err := CreateAdmin(s, "thomas", "", io.Discard); err != nil {
		t.Fatal(err)
	}

	user, _ := s.UserByUsername("thomas")
	ok, _ := auth.VerifyPassword("a-solid-password", user.PasswordHash)
	if !ok {
		t.Fatal("the password kept the line ending")
	}
}

func TestCreateAdminRejectsAShortPassword(t *testing.T) {
	s := newTestStore(t)
	withStdin(t, "court\n")

	if err := CreateAdmin(s, "thomas", "", io.Discard); err == nil {
		t.Fatal("a short password was accepted")
	}
	if _, err := s.UserByUsername("thomas"); err == nil {
		t.Fatal("the account was created despite the rejection")
	}
}

func TestCreateAdminRefusesAnExistingAccount(t *testing.T) {
	s := newTestStore(t)
	withStdin(t, "a-solid-password\n")
	if err := CreateAdmin(s, "thomas", "", io.Discard); err != nil {
		t.Fatal(err)
	}

	withStdin(t, "another-password\n")
	if err := CreateAdmin(s, "thomas", "", io.Discard); err == nil {
		t.Fatal("the account was created twice")
	}
}

func TestCreateAdminRequiresAUsername(t *testing.T) {
	s := newTestStore(t)
	if err := CreateAdmin(s, "   ", "", io.Discard); err == nil {
		t.Fatal("an empty username was accepted")
	}
}
