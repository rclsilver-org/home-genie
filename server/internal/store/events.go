package store

import (
	"encoding/json"
	"fmt"
	"time"
)

// Event is one entry of a user's ordered log. Its Seq is the cursor clients
// resynchronise on: a socket killed by the system loses nothing, it replays
// from the last Seq it saw.
type Event struct {
	Seq       int64
	UserID    int64
	Kind      string
	Payload   json.RawMessage
	CreatedAt time.Time
}

// Event kinds. They are part of the client contract, so they are named here
// rather than spelled out at each call site.
const (
	EventChannelCreated = "channel.created"
	EventChannelUpdated = "channel.updated"
	EventChannelDeleted = "channel.deleted"
	EventMemberChanged  = "member.changed"
	EventMemberRemoved  = "member.removed"
)

// AppendEvent records an event for one user and returns its Seq.
func (s *Store) AppendEvent(userID int64, kind string, payload any) (Event, error) {
	encoded := json.RawMessage("{}")
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return Event{}, fmt.Errorf("encoding the payload: %w", err)
		}
		encoded = raw
	}

	created := s.timestamp()
	result, err := s.db.Exec(
		`INSERT INTO events (user_id, kind, payload, created_at) VALUES (?, ?, ?, ?)`,
		userID, kind, string(encoded), created)
	if err != nil {
		return Event{}, fmt.Errorf("recording the event: %w", err)
	}

	seq, err := result.LastInsertId()
	if err != nil {
		return Event{}, fmt.Errorf("reading the sequence: %w", err)
	}
	at, _ := parseTime(created)

	return Event{Seq: seq, UserID: userID, Kind: kind, Payload: encoded, CreatedAt: at}, nil
}

// AppendEventTo records the same event for several users, which is the shape
// every channel-wide change takes.
func (s *Store) AppendEventTo(userIDs []int64, kind string, payload any) ([]Event, error) {
	events := make([]Event, 0, len(userIDs))
	for _, userID := range userIDs {
		event, err := s.AppendEvent(userID, kind, payload)
		if err != nil {
			return events, err
		}
		events = append(events, event)
	}
	return events, nil
}

// EventsSince returns what a user missed after sinceSeq, oldest first. The
// limit bounds a very long absence; the caller replays again from the last
// Seq it got until the page comes back short.
func (s *Store) EventsSince(userID, sinceSeq int64, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 500
	}

	rows, err := s.db.Query(
		`SELECT seq, user_id, kind, payload, created_at
		   FROM events WHERE user_id = ? AND seq > ?
		  ORDER BY seq LIMIT ?`, userID, sinceSeq, limit)
	if err != nil {
		return nil, fmt.Errorf("reading the events: %w", err)
	}
	defer rows.Close()

	events := []Event{}
	for rows.Next() {
		var (
			event   Event
			payload string
			created string
		)
		if err := rows.Scan(&event.Seq, &event.UserID, &event.Kind, &payload, &created); err != nil {
			return nil, fmt.Errorf("reading an event: %w", err)
		}
		event.Payload = json.RawMessage(payload)
		event.CreatedAt, _ = parseTime(created)
		events = append(events, event)
	}

	return events, rows.Err()
}

// LatestSeq returns a user's most recent Seq, or 0 when they have no events.
func (s *Store) LatestSeq(userID int64) (int64, error) {
	var seq *int64
	if err := s.db.QueryRow(
		`SELECT MAX(seq) FROM events WHERE user_id = ?`, userID).Scan(&seq); err != nil {
		return 0, fmt.Errorf("reading the last sequence: %w", err)
	}
	if seq == nil {
		return 0, nil
	}
	return *seq, nil
}

// MemberUserIDs lists who should be told about a channel change.
func (s *Store) MemberUserIDs(channelID int64) ([]int64, error) {
	rows, err := s.db.Query(
		`SELECT user_id FROM channel_members WHERE channel_id = ?`, channelID)
	if err != nil {
		return nil, fmt.Errorf("listing the members: %w", err)
	}
	defer rows.Close()

	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("reading a member: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// PurgeEventsBefore drops a user's events up to and including seq. Kept for
// the retention job; it never touches what a device has not acknowledged.
func (s *Store) PurgeEventsBefore(olderThan time.Time) (int64, error) {
	result, err := s.db.Exec(
		`DELETE FROM events WHERE created_at < ?`, olderThan.UTC().Format(time.RFC3339))
	if err != nil {
		return 0, fmt.Errorf("purging the events: %w", err)
	}
	return result.RowsAffected()
}

// MarkDeviceConnected records that a live socket is attached. It is what
// makes a delivery to a device without a socket countable as a miss.
func (s *Store) MarkDeviceConnected(deviceID int64) error {
	now := s.timestamp()
	if _, err := s.db.Exec(
		`UPDATE devices SET ws_connected_at = ?, last_seen_at = ? WHERE id = ?`,
		now, now, deviceID); err != nil {
		return fmt.Errorf("marking the device connected: %w", err)
	}
	return nil
}

// MarkDeviceDisconnected clears the live-socket marker.
func (s *Store) MarkDeviceDisconnected(deviceID int64) error {
	if _, err := s.db.Exec(
		`UPDATE devices SET ws_connected_at = NULL, last_seen_at = ? WHERE id = ?`,
		s.timestamp(), deviceID); err != nil {
		return fmt.Errorf("marking the device disconnected: %w", err)
	}
	return nil
}

// DisconnectAllDevices clears every live-socket marker. Called at startup:
// after a restart no socket survives, and a stale marker would make the
// reliability figures lie.
func (s *Store) DisconnectAllDevices() error {
	if _, err := s.db.Exec(
		`UPDATE devices SET ws_connected_at = NULL WHERE ws_connected_at IS NOT NULL`); err != nil {
		return fmt.Errorf("clearing the connections: %w", err)
	}
	return nil
}

// ConnectedDeviceCount reports how many devices hold a live socket, which is
// the metric the monitoring will watch.
func (s *Store) ConnectedDeviceCount() (int, error) {
	var count int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM devices WHERE ws_connected_at IS NOT NULL`).Scan(&count); err != nil {
		return 0, fmt.Errorf("counting the connections: %w", err)
	}
	return count, nil
}
