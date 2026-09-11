package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// User is an account. Exactly one of PasswordHash and OIDCSubject is set:
// the local break-glass account has a password, OIDC users have a
// subject.
type User struct {
	ID           int64
	Username     string
	DisplayName  string
	PasswordHash string
	OIDCSubject  string
	IsAdmin      bool
	CreatedAt    time.Time
}

// IsLocal reports whether the account authenticates with a password rather
// than through OIDC. It is the account that still works when the provider
// is down.
func (u User) IsLocal() bool { return u.PasswordHash != "" }

// CreateLocalUser adds an account authenticating with a password.
func (s *Store) CreateLocalUser(username, displayName, passwordHash string, isAdmin bool) (User, error) {
	if strings.TrimSpace(username) == "" {
		return User{}, fmt.Errorf("the username must not be empty")
	}
	if passwordHash == "" {
		return User{}, fmt.Errorf("a local account needs a password")
	}

	created := s.timestamp()
	result, err := s.db.Exec(
		`INSERT INTO users (username, display_name, password_hash, is_admin, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		username, displayName, passwordHash, boolToInt(isAdmin), created)
	if err != nil {
		if isUniqueViolation(err) {
			return User{}, fmt.Errorf("%w: username %q", ErrConflict, username)
		}
		return User{}, fmt.Errorf("creating the user: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return User{}, fmt.Errorf("reading the new id: %w", err)
	}
	at, _ := parseTime(created)

	return User{
		ID:           id,
		Username:     username,
		DisplayName:  displayName,
		PasswordHash: passwordHash,
		IsAdmin:      isAdmin,
		CreatedAt:    at,
	}, nil
}

// UserByUsername looks an account up by its login name.
func (s *Store) UserByUsername(username string) (User, error) {
	return s.scanUser(s.db.QueryRow(
		`SELECT id, username, display_name, password_hash, oidc_subject, is_admin, created_at
		   FROM users WHERE username = ?`, username))
}

// UserByID looks an account up by its identifier.
func (s *Store) UserByID(id int64) (User, error) {
	return s.scanUser(s.db.QueryRow(
		`SELECT id, username, display_name, password_hash, oidc_subject, is_admin, created_at
		   FROM users WHERE id = ?`, id))
}

// CountUsers reports how many accounts exist, which is what tells the
// bootstrap command whether the server has ever been set up.
func (s *Store) CountUsers() (int, error) {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return 0, fmt.Errorf("counting the users: %w", err)
	}
	return count, nil
}

func (s *Store) scanUser(row *sql.Row) (User, error) {
	var (
		user     User
		password sql.NullString
		subject  sql.NullString
		isAdmin  int
		created  string
	)

	err := row.Scan(&user.ID, &user.Username, &user.DisplayName,
		&password, &subject, &isAdmin, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("reading the user: %w", err)
	}

	user.PasswordHash = nullString(password)
	user.OIDCSubject = nullString(subject)
	user.IsAdmin = isAdmin == 1
	user.CreatedAt, _ = parseTime(created)

	return user, nil
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

// isUniqueViolation recognises a UNIQUE constraint failure without importing
// the driver's error types, which modernc.org/sqlite does not export in a
// stable form.
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// CreateOIDCUser adds an account authenticating through the provider. It has no
// password: it is not a break-glass account and must not become one.
func (s *Store) CreateOIDCUser(subject, username, displayName string) (User, error) {
	if subject == "" {
		return User{}, fmt.Errorf("an OIDC account needs a subject")
	}
	if strings.TrimSpace(username) == "" {
		return User{}, fmt.Errorf("the username must not be empty")
	}

	created := s.timestamp()
	result, err := s.db.Exec(
		`INSERT INTO users (username, display_name, oidc_subject, is_admin, created_at)
		 VALUES (?, ?, ?, 0, ?)`, username, displayName, subject, created)
	if err != nil {
		if isUniqueViolation(err) {
			return User{}, fmt.Errorf("%w: subject or username already taken", ErrConflict)
		}
		return User{}, fmt.Errorf("creating the user: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return User{}, fmt.Errorf("reading the new id: %w", err)
	}
	at, _ := parseTime(created)

	return User{
		ID: id, Username: username, DisplayName: displayName,
		OIDCSubject: subject, CreatedAt: at,
	}, nil
}

// UserByOIDCSubject looks an account up by the stable identifier the provider
// issues. The subject, never the username or the email: those can be
// changed or reassigned, and matching on them would let a renamed account
// inherit somebody else's channels.
func (s *Store) UserByOIDCSubject(subject string) (User, error) {
	return s.scanUser(s.db.QueryRow(
		`SELECT id, username, display_name, password_hash, oidc_subject, is_admin, created_at
		   FROM users WHERE oidc_subject = ?`, subject))
}

// UpdateUserProfile refreshes what the provider owns, so a rename there shows
// up here at the next login.
func (s *Store) UpdateUserProfile(id int64, username, displayName string) error {
	if _, err := s.db.Exec(
		`UPDATE users SET username = ?, display_name = ? WHERE id = ?`,
		username, displayName, id); err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("%w: username %q", ErrConflict, username)
		}
		return fmt.Errorf("updating the profile: %w", err)
	}
	return nil
}

// SearchUsers lists the accounts whose username or display name contains the
// given fragment, so a channel owner can pick a member rather than spell one.
//
// Everyone is listed, members of the channel included: filtering them out
// here would make the caller unable to tell an unknown name from one already
// added, and the screen knows who its members are anyway.
func (s *Store) SearchUsers(fragment string, limit int) ([]User, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	pattern := "%" + fragment + "%"
	rows, err := s.db.Query(
		`SELECT id, username, display_name, is_admin
		   FROM users
		  WHERE username LIKE ? OR display_name LIKE ?
		  ORDER BY username
		  LIMIT ?`, pattern, pattern, limit)
	if err != nil {
		return nil, fmt.Errorf("searching the users: %w", err)
	}
	defer rows.Close()

	users := []User{}
	for rows.Next() {
		var user User
		var admin int
		if err := rows.Scan(&user.ID, &user.Username, &user.DisplayName, &admin); err != nil {
			return nil, fmt.Errorf("reading a user: %w", err)
		}
		user.IsAdmin = admin == 1
		users = append(users, user)
	}
	return users, rows.Err()
}
