// Package cli holds the administrative subcommands of hgenied. They exist so
// that the break-glass account can be created without a running server and
// without an HTTP client — typically when the identity provider is down.
package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/rclsilver-org/home-genie/server/internal/auth"
	"github.com/rclsilver-org/home-genie/server/internal/store"
)

// minPasswordLength is a floor, not a policy. The account is meant to be the
// last way in when everything else is down, so a two-character password
// would be a real hazard; anything beyond a length check belongs to the
// person choosing it.
const minPasswordLength = 12

// CreateAdmin creates a local administrator account. The password is read
// from the terminal without echo, or from stdin when the command is piped,
// so it never lands in the shell history.
func CreateAdmin(s *store.Store, username, displayName string, out io.Writer) error {
	username = strings.TrimSpace(username)
	if username == "" {
		return errors.New("a username is required")
	}

	if _, err := s.UserByUsername(username); err == nil {
		return fmt.Errorf("the account %q already exists", username)
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}

	password, err := readPassword(out)
	if err != nil {
		return err
	}
	if len([]rune(password)) < minPasswordLength {
		return fmt.Errorf("the password must be at least %d characters", minPasswordLength)
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}

	user, err := s.CreateLocalUser(username, displayName, hash, true)
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "Local administrator account %q created (id %d).\n", user.Username, user.ID)
	fmt.Fprintf(out, "It is the break-glass account: it works when the identity provider does not.\n")
	return nil
}

// readPassword prompts twice on a terminal, and reads a single line when
// stdin is a pipe — which is how the CI and any automation will call it.
func readPassword(out io.Writer) (string, error) {
	fd := int(os.Stdin.Fd())

	if !term.IsTerminal(fd) {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", fmt.Errorf("reading the password: %w", err)
		}
		return strings.TrimRight(line, "\r\n"), nil
	}

	fmt.Fprint(out, "Password: ")
	first, err := term.ReadPassword(fd)
	fmt.Fprintln(out)
	if err != nil {
		return "", fmt.Errorf("reading the password: %w", err)
	}

	fmt.Fprint(out, "Confirm: ")
	second, err := term.ReadPassword(fd)
	fmt.Fprintln(out)
	if err != nil {
		return "", fmt.Errorf("reading the password: %w", err)
	}

	if string(first) != string(second) {
		return "", errors.New("the two passwords differ")
	}
	return string(first), nil
}
