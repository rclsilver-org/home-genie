package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// PublishToken is the credential a machine carries. It is write-only and
// bound to a single channel: *arr, diun and the Alertmanager receiver each
// get their own, revocable without touching anything else.
type PublishToken struct {
	ID         int64
	ChannelID  int64
	Name       string
	LastUsedAt *time.Time
	RevokedAt  *time.Time
	CreatedAt  time.Time
}

// IsRevoked reports whether the token has been withdrawn.
func (t PublishToken) IsRevoked() bool { return t.RevokedAt != nil }

// CreatePublishToken records a token for a channel. The clear value never
// reaches the database; the caller shows it once.
func (s *Store) CreatePublishToken(channelID int64, name, tokenHash string) (PublishToken, error) {
	if tokenHash == "" {
		return PublishToken{}, fmt.Errorf("a token is required")
	}

	created := s.timestamp()
	result, err := s.db.Exec(
		`INSERT INTO publish_tokens (channel_id, name, token_hash, created_at)
		 VALUES (?, ?, ?, ?)`, channelID, name, tokenHash, created)
	if err != nil {
		return PublishToken{}, fmt.Errorf("creating the token: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return PublishToken{}, fmt.Errorf("reading the new id: %w", err)
	}
	at, _ := parseTime(created)

	return PublishToken{ID: id, ChannelID: channelID, Name: name, CreatedAt: at}, nil
}

// PublishTokenByHash resolves a bearer token to its token record and target
// channel. A revoked token does not resolve: revocation is immediate, not a
// flag the caller has to remember to check.
func (s *Store) PublishTokenByHash(tokenHash string) (PublishToken, Channel, error) {
	var (
		token    PublishToken
		channel  Channel
		lastUsed sql.NullString
		created  string
		muted    sql.NullString
		chCreate string
	)

	err := s.db.QueryRow(
		`SELECT t.id, t.channel_id, t.name, t.last_used_at, t.created_at,
		        c.id, c.slug, c.name, c.description, c.muted_until, c.created_at
		   FROM publish_tokens t
		   JOIN channels c ON c.id = t.channel_id
		  WHERE t.token_hash = ? AND t.revoked_at IS NULL`, tokenHash).
		Scan(&token.ID, &token.ChannelID, &token.Name, &lastUsed, &created,
			&channel.ID, &channel.Slug, &channel.Name, &channel.Description,
			&muted, &chCreate)
	if errors.Is(err, sql.ErrNoRows) {
		return PublishToken{}, Channel{}, ErrNotFound
	}
	if err != nil {
		return PublishToken{}, Channel{}, fmt.Errorf("reading the token: %w", err)
	}

	token.LastUsedAt = optionalTime(lastUsed)
	token.CreatedAt, _ = parseTime(created)
	channel.MutedUntil = optionalTime(muted)
	channel.CreatedAt, _ = parseTime(chCreate)

	return token, channel, nil
}

// TouchPublishToken records that a token was used, which is what makes a
// forgotten producer visible in the channel's token list.
func (s *Store) TouchPublishToken(id int64) error {
	if _, err := s.db.Exec(
		`UPDATE publish_tokens SET last_used_at = ? WHERE id = ?`,
		s.timestamp(), id); err != nil {
		return fmt.Errorf("updating the token: %w", err)
	}
	return nil
}

// PublishTokensOf lists a channel's tokens, revoked ones included so that
// the history of who could publish stays visible.
func (s *Store) PublishTokensOf(channelID int64) ([]PublishToken, error) {
	rows, err := s.db.Query(
		`SELECT id, channel_id, name, last_used_at, revoked_at, created_at
		   FROM publish_tokens WHERE channel_id = ?
		  ORDER BY revoked_at IS NOT NULL, created_at DESC`, channelID)
	if err != nil {
		return nil, fmt.Errorf("listing the tokens: %w", err)
	}
	defer rows.Close()

	tokens := []PublishToken{}
	for rows.Next() {
		var (
			token    PublishToken
			lastUsed sql.NullString
			revoked  sql.NullString
			created  string
		)
		if err := rows.Scan(&token.ID, &token.ChannelID, &token.Name,
			&lastUsed, &revoked, &created); err != nil {
			return nil, fmt.Errorf("reading a token: %w", err)
		}
		token.LastUsedAt = optionalTime(lastUsed)
		token.RevokedAt = optionalTime(revoked)
		token.CreatedAt, _ = parseTime(created)
		tokens = append(tokens, token)
	}

	return tokens, rows.Err()
}

// RevokePublishToken withdraws a token. The row is kept, so the token list
// still shows that a producer once had access and when it last published.
func (s *Store) RevokePublishToken(channelID, tokenID int64) error {
	result, err := s.db.Exec(
		`UPDATE publish_tokens SET revoked_at = ?
		  WHERE id = ? AND channel_id = ? AND revoked_at IS NULL`,
		s.timestamp(), tokenID, channelID)
	if err != nil {
		return fmt.Errorf("revoking the token: %w", err)
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

// DeletePublishToken removes a token for good. Only a revoked one: the row is
// what stops the token from publishing, so dropping a live one would look
// like a revocation while silently leaving nothing to check against... and it
// would in fact still be refused, since the lookup finds nothing. The real
// reason is different: a token that disappears without having been revoked
// leaves no trace that it ever existed, and the point of keeping revoked rows
// is precisely to be able to say which producer was cut off and when.
func (s *Store) DeletePublishToken(channelID, tokenID int64) error {
	result, err := s.db.Exec(
		`DELETE FROM publish_tokens
		  WHERE id = ? AND channel_id = ? AND revoked_at IS NOT NULL`,
		tokenID, channelID)
	if err != nil {
		return fmt.Errorf("deleting the token: %w", err)
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
