package store

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Role is a user's standing on a channel.
type Role string

const (
	RoleOwner  Role = "owner"  // everything, including members and tokens
	RoleWriter Role = "writer" // publish from the app
	RoleReader Role = "reader" // read and acknowledge
)

// CanPublish reports whether the role may post to the channel.
func (r Role) CanPublish() bool { return r == RoleOwner || r == RoleWriter }

// CanAdminister reports whether the role may change members and tokens.
func (r Role) CanAdminister() bool { return r == RoleOwner }

// Valid reports whether the role is one the schema accepts.
func (r Role) Valid() bool {
	return r == RoleOwner || r == RoleWriter || r == RoleReader
}

// slugPattern constrains what can appear in a publish URL. Producers write
// this by hand in their configuration, so it stays to the characters that
// survive a shell, a YAML file and a URL unescaped.
var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}[a-z0-9]$|^[a-z0-9]$`)

// reservedSlugs must never become channels: the ntfy-compatible ingest
// endpoint lives at the root, so a channel named "api" would shadow the
// whole API.
var reservedSlugs = map[string]bool{
	"api": true, "healthz": true, "metrics": true, "docs": true,
	"static": true, "assets": true, "v1": true, "admin": true,
}

// ErrInvalidSlug is returned for a name that cannot be a publish path.
var ErrInvalidSlug = errors.New("invalid channel slug")

// ValidateSlug checks a candidate publish path.
func ValidateSlug(slug string) error {
	if !slugPattern.MatchString(slug) {
		return fmt.Errorf("%w: %q must be 1 to 64 characters of a-z, 0-9 and hyphens, "+
			"starting and ending with a letter or a digit", ErrInvalidSlug, slug)
	}
	if reservedSlugs[slug] {
		return fmt.Errorf("%w: %q is reserved", ErrInvalidSlug, slug)
	}
	return nil
}

// Channel is a stream producers publish to and members read.
type Channel struct {
	ID          int64
	Slug        string
	Name        string
	Description string
	CreatedAt   time.Time
}

// Membership pairs a channel with the caller's standing on it.
type Membership struct {
	Channel Channel
	Role    Role
}

// Member is a user's standing on a channel, for the member list.
type Member struct {
	UserID      int64
	Username    string
	DisplayName string
	Role        Role
	CreatedAt   time.Time
}

// CreateChannel creates a channel and makes owner its first owner, in one
// transaction: a channel with no owner would be unadministrable.
func (s *Store) CreateChannel(slug, name, description string, ownerID int64) (Channel, error) {
	if err := ValidateSlug(slug); err != nil {
		return Channel{}, err
	}
	if strings.TrimSpace(name) == "" {
		name = slug
	}

	tx, err := s.db.Begin()
	if err != nil {
		return Channel{}, fmt.Errorf("opening the transaction: %w", err)
	}
	defer tx.Rollback()

	created := s.timestamp()
	result, err := tx.Exec(
		`INSERT INTO channels (slug, name, description, created_at) VALUES (?, ?, ?, ?)`,
		slug, name, description, created)
	if err != nil {
		if isUniqueViolation(err) {
			return Channel{}, fmt.Errorf("%w: channel %q", ErrConflict, slug)
		}
		return Channel{}, fmt.Errorf("creating the channel: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return Channel{}, fmt.Errorf("reading the new id: %w", err)
	}

	if _, err := tx.Exec(
		`INSERT INTO channel_members (channel_id, user_id, role, created_at)
		 VALUES (?, ?, ?, ?)`, id, ownerID, RoleOwner, created); err != nil {
		return Channel{}, fmt.Errorf("assigning the owner: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Channel{}, fmt.Errorf("committing: %w", err)
	}

	at, _ := parseTime(created)
	return Channel{ID: id, Slug: slug, Name: name, Description: description, CreatedAt: at}, nil
}

// ChannelBySlug resolves the publish path. Used by the ingest endpoints,
// which authenticate with a token rather than a membership.
func (s *Store) ChannelBySlug(slug string) (Channel, error) {
	return s.scanChannel(s.db.QueryRow(
		`SELECT id, slug, name, description, created_at
		   FROM channels WHERE slug = ?`, slug))
}

// ChannelByID resolves a channel by identifier.
func (s *Store) ChannelByID(id int64) (Channel, error) {
	return s.scanChannel(s.db.QueryRow(
		`SELECT id, slug, name, description, created_at
		   FROM channels WHERE id = ?`, id))
}

// MembershipsOf lists the channels a user belongs to, with their role.
func (s *Store) MembershipsOf(userID int64) ([]Membership, error) {
	rows, err := s.db.Query(
		`SELECT c.id, c.slug, c.name, c.description, c.created_at, m.role
		   FROM channels c
		   JOIN channel_members m ON m.channel_id = c.id
		  WHERE m.user_id = ?
		  ORDER BY c.slug`, userID)
	if err != nil {
		return nil, fmt.Errorf("listing the channels: %w", err)
	}
	defer rows.Close()

	memberships := []Membership{}
	for rows.Next() {
		var (
			membership Membership
			created    string
			role       string
		)
		if err := rows.Scan(&membership.Channel.ID, &membership.Channel.Slug,
			&membership.Channel.Name, &membership.Channel.Description,
			&created, &role); err != nil {
			return nil, fmt.Errorf("reading a channel: %w", err)
		}
		membership.Channel.CreatedAt, _ = parseTime(created)
		membership.Role = Role(role)
		memberships = append(memberships, membership)
	}

	return memberships, rows.Err()
}

// RoleOn returns the user's role on a channel, or ErrNotFound when they are
// not a member. Callers turn that into a 404 rather than a 403, so that a
// channel's existence does not leak to people who cannot see it.
func (s *Store) RoleOn(channelID, userID int64) (Role, error) {
	var role string
	err := s.db.QueryRow(
		`SELECT role FROM channel_members WHERE channel_id = ? AND user_id = ?`,
		channelID, userID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("reading the role: %w", err)
	}
	return Role(role), nil
}

// SetMember adds a member or changes their role.
func (s *Store) SetMember(channelID, userID int64, role Role) error {
	if !role.Valid() {
		return fmt.Errorf("unknown role %q", role)
	}
	if _, err := s.db.Exec(
		`INSERT INTO channel_members (channel_id, user_id, role, created_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT (channel_id, user_id) DO UPDATE SET role = excluded.role`,
		channelID, userID, role, s.timestamp()); err != nil {
		return fmt.Errorf("assigning the member: %w", err)
	}
	return nil
}

// RemoveMember drops a membership. It refuses to remove the last owner: a
// channel nobody can administer would need a hand on the database to fix.
func (s *Store) RemoveMember(channelID, userID int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("opening the transaction: %w", err)
	}
	defer tx.Rollback()

	var role string
	err = tx.QueryRow(
		`SELECT role FROM channel_members WHERE channel_id = ? AND user_id = ?`,
		channelID, userID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("reading the role: %w", err)
	}

	if Role(role) == RoleOwner {
		var owners int
		if err := tx.QueryRow(
			`SELECT COUNT(*) FROM channel_members WHERE channel_id = ? AND role = ?`,
			channelID, RoleOwner).Scan(&owners); err != nil {
			return fmt.Errorf("counting the owners: %w", err)
		}
		if owners <= 1 {
			return fmt.Errorf("%w: removing the last owner would leave the channel unadministrable",
				ErrConflict)
		}
	}

	if _, err := tx.Exec(
		`DELETE FROM channel_members WHERE channel_id = ? AND user_id = ?`,
		channelID, userID); err != nil {
		return fmt.Errorf("removing the member: %w", err)
	}

	return tx.Commit()
}

// MembersOf lists a channel's members.
func (s *Store) MembersOf(channelID int64) ([]Member, error) {
	rows, err := s.db.Query(
		`SELECT u.id, u.username, u.display_name, m.role, m.created_at
		   FROM channel_members m
		   JOIN users u ON u.id = m.user_id
		  WHERE m.channel_id = ?
		  ORDER BY u.username`, channelID)
	if err != nil {
		return nil, fmt.Errorf("listing the members: %w", err)
	}
	defer rows.Close()

	members := []Member{}
	for rows.Next() {
		var (
			member  Member
			role    string
			created string
		)
		if err := rows.Scan(&member.UserID, &member.Username, &member.DisplayName,
			&role, &created); err != nil {
			return nil, fmt.Errorf("reading a member: %w", err)
		}
		member.Role = Role(role)
		member.CreatedAt, _ = parseTime(created)
		members = append(members, member)
	}

	return members, rows.Err()
}

// UpdateChannel changes the editable fields. A nil pointer leaves the field
// alone, which is what lets a PATCH clear muted_until explicitly.
func (s *Store) UpdateChannel(id int64, name, description *string) error {
	sets := []string{}
	args := []any{}

	if name != nil {
		sets = append(sets, "name = ?")
		args = append(args, *name)
	}
	if description != nil {
		sets = append(sets, "description = ?")
		args = append(args, *description)
	}
	if len(sets) == 0 {
		return nil
	}

	args = append(args, id)
	if _, err := s.db.Exec(
		`UPDATE channels SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...); err != nil {
		return fmt.Errorf("updating the channel: %w", err)
	}
	return nil
}

// DeleteChannel removes a channel; the schema cascades its members, tokens,
// alerts and messages.
func (s *Store) DeleteChannel(id int64) error {
	result, err := s.db.Exec(`DELETE FROM channels WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting the channel: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("reading the result: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) scanChannel(row *sql.Row) (Channel, error) {
	var (
		channel Channel
		created string
	)
	err := row.Scan(&channel.ID, &channel.Slug, &channel.Name, &channel.Description,
		&created)
	if errors.Is(err, sql.ErrNoRows) {
		return Channel{}, ErrNotFound
	}
	if err != nil {
		return Channel{}, fmt.Errorf("reading the channel: %w", err)
	}
	channel.CreatedAt, _ = parseTime(created)
	return channel, nil
}
