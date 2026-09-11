package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Priority follows ntfy's scale, so producers already configured against it
// keep working unchanged.
const (
	PriorityMin     = 1
	PriorityLow     = 2
	PriorityDefault = 3
	PriorityHigh    = 4
	PriorityMax     = 5
)

// Message is one entry of a channel's stream. Read state is deliberately
// not a field: it belongs to the reader, not to the message.
type Message struct {
	ID        int64
	ChannelID int64
	AlertID   *int64
	Title     string
	Body      string
	Priority  int
	Tags      []string
	ClickURL  string
	Actions   json.RawMessage
	CreatedAt time.Time
}

// NewMessage is what a producer submits.
type NewMessage struct {
	ChannelID int64
	AlertID   *int64
	Title     string
	Body      string
	Priority  int
	Tags      []string
	ClickURL  string
	Actions   json.RawMessage
}

// CreateMessage records a message.
func (s *Store) CreateMessage(input NewMessage) (Message, error) {
	if input.Priority < PriorityMin || input.Priority > PriorityMax {
		input.Priority = PriorityDefault
	}
	if input.Tags == nil {
		input.Tags = []string{}
	}
	tags, err := json.Marshal(input.Tags)
	if err != nil {
		return Message{}, fmt.Errorf("encoding the tags: %w", err)
	}
	actions := input.Actions
	if len(actions) == 0 {
		actions = json.RawMessage("[]")
	}

	created := s.timestamp()
	result, err := s.db.Exec(
		`INSERT INTO messages (channel_id, alert_id, title, body, priority, tags, click_url, actions, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		input.ChannelID, input.AlertID, input.Title, input.Body, input.Priority,
		string(tags), input.ClickURL, string(actions), created)
	if err != nil {
		return Message{}, fmt.Errorf("recording the message: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return Message{}, fmt.Errorf("reading the new id: %w", err)
	}
	at, _ := parseTime(created)

	return Message{
		ID: id, ChannelID: input.ChannelID, AlertID: input.AlertID,
		Title: input.Title, Body: input.Body, Priority: input.Priority,
		Tags: input.Tags, ClickURL: input.ClickURL, Actions: actions, CreatedAt: at,
	}, nil
}

// MessageByID reads one message.
func (s *Store) MessageByID(id int64) (Message, error) {
	return s.scanMessage(s.db.QueryRow(
		`SELECT id, channel_id, alert_id, title, body, priority, tags, click_url, actions, created_at
		   FROM messages WHERE id = ?`, id))
}

// MessageQuery bounds a listing.
type MessageQuery struct {
	ChannelID int64
	// BeforeID pages backwards through the stream; 0 starts at the newest.
	BeforeID int64
	Limit    int
}

// MessagesOf returns a channel's messages, newest first.
func (s *Store) MessagesOf(query MessageQuery) ([]Message, error) {
	if query.Limit <= 0 || query.Limit > 200 {
		query.Limit = 50
	}

	sqlText := `SELECT id, channel_id, alert_id, title, body, priority, tags, click_url, actions, created_at
	              FROM messages WHERE channel_id = ?`
	args := []any{query.ChannelID}
	if query.BeforeID > 0 {
		sqlText += ` AND id < ?`
		args = append(args, query.BeforeID)
	}
	sqlText += ` ORDER BY id DESC LIMIT ?`
	args = append(args, query.Limit)

	rows, err := s.db.Query(sqlText, args...)
	if err != nil {
		return nil, fmt.Errorf("listing the messages: %w", err)
	}
	defer rows.Close()

	messages := []Message{}
	for rows.Next() {
		message, err := scanMessageRow(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, rows.Err()
}

func (s *Store) scanMessage(row *sql.Row) (Message, error) {
	var (
		message Message
		alertID sql.NullInt64
		tags    string
		actions string
		created string
	)
	err := row.Scan(&message.ID, &message.ChannelID, &alertID, &message.Title,
		&message.Body, &message.Priority, &tags, &message.ClickURL, &actions, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return Message{}, ErrNotFound
	}
	if err != nil {
		return Message{}, fmt.Errorf("reading the message: %w", err)
	}
	return hydrate(message, alertID, tags, actions, created)
}

// rowScanner covers both *sql.Row and *sql.Rows.
type rowScanner interface{ Scan(dest ...any) error }

func scanMessageRow(row rowScanner) (Message, error) {
	var (
		message Message
		alertID sql.NullInt64
		tags    string
		actions string
		created string
	)
	if err := row.Scan(&message.ID, &message.ChannelID, &alertID, &message.Title,
		&message.Body, &message.Priority, &tags, &message.ClickURL, &actions, &created); err != nil {
		return Message{}, fmt.Errorf("reading a message: %w", err)
	}
	return hydrate(message, alertID, tags, actions, created)
}

func hydrate(message Message, alertID sql.NullInt64, tags, actions, created string) (Message, error) {
	if alertID.Valid {
		value := alertID.Int64
		message.AlertID = &value
	}
	if err := json.Unmarshal([]byte(tags), &message.Tags); err != nil {
		message.Tags = []string{}
	}
	message.Actions = json.RawMessage(actions)
	message.CreatedAt, _ = parseTime(created)
	return message, nil
}

// FeedQuery bounds the cross-channel listing that feeds the notifications
// view.
type FeedQuery struct {
	// OnlyUnread is the working state of that view: what is left to look at.
	OnlyUnread bool
	// BeforeID pages backwards; 0 starts at the newest.
	BeforeID int64
	Limit    int
}

// MessagesForUser returns, across every channel the user belongs to, the
// messages that are not alerts, newest first.
//
// Alert-backed messages are excluded rather than filtered by the client:
// alerts have a console of their own, with a lifecycle the read state does
// not describe, and their reminders would flood a feed whose whole purpose
// is to be emptied by reading it.
func (s *Store) MessagesForUser(userID int64, query FeedQuery) ([]Message, error) {
	if query.Limit <= 0 || query.Limit > 200 {
		query.Limit = 50
	}

	sqlText := `SELECT m.id, m.channel_id, m.alert_id, m.title, m.body, m.priority,
	                   m.tags, m.click_url, m.actions, m.created_at
	              FROM messages m
	              JOIN channel_members c ON c.channel_id = m.channel_id AND c.user_id = ?
	             WHERE m.alert_id IS NULL`
	args := []any{userID}
	if query.OnlyUnread {
		sqlText += ` AND NOT EXISTS (SELECT 1 FROM message_reads r
		                              WHERE r.message_id = m.id AND r.user_id = ?)`
		args = append(args, userID)
	}
	if query.BeforeID > 0 {
		sqlText += ` AND m.id < ?`
		args = append(args, query.BeforeID)
	}
	sqlText += ` ORDER BY m.id DESC LIMIT ?`
	args = append(args, query.Limit)

	rows, err := s.db.Query(sqlText, args...)
	if err != nil {
		return nil, fmt.Errorf("listing the messages: %w", err)
	}
	defer rows.Close()

	messages := []Message{}
	for rows.Next() {
		message, err := scanMessageRow(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, rows.Err()
}

// MarkFeedRead marks every message the user can read and has not read yet,
// alerts excluded for the same reason they are absent from the feed.
// Returns the ids actually marked.
func (s *Store) MarkFeedRead(userID int64, deviceID *int64) ([]int64, error) {
	rows, err := s.db.Query(
		`SELECT m.id FROM messages m
		   JOIN channel_members c ON c.channel_id = m.channel_id AND c.user_id = ?
		  WHERE m.alert_id IS NULL
		    AND NOT EXISTS (SELECT 1 FROM message_reads r
		                     WHERE r.message_id = m.id AND r.user_id = ?)`,
		userID, userID)
	if err != nil {
		return nil, fmt.Errorf("listing the unread messages: %w", err)
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, fmt.Errorf("reading a message: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for _, id := range ids {
		if _, err := s.MarkRead(id, userID, deviceID); err != nil {
			return ids, err
		}
	}
	return ids, nil
}
