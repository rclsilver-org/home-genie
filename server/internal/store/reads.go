package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// Message event kinds. The log is append-only: a message's history is
// lengthened, never rewritten.
const (
	MessageQueued    = "queued"    // recorded for a recipient, fanned out
	MessageSent      = "sent"      // written to a device's socket
	MessageDelivered = "delivered" // the device acknowledged it
	MessageRead      = "read"      // the user opened it
	MessageAcked     = "acked"     // the user acknowledged an alert
	MessageDismissed = "dismissed" // the user swiped it away
)

// MessageEvent is one step of a message's life, for one user and possibly
// one device.
type MessageEvent struct {
	ID        int64
	MessageID int64
	UserID    int64
	Username  string
	DeviceID  *int64
	Device    string
	Kind      string
	At        time.Time
}

// RecordMessageEvent appends to the timeline. deviceID may be nil for the
// steps that concern a user rather than one of their devices.
func (s *Store) RecordMessageEvent(messageID, userID int64, deviceID *int64, kind string) error {
	if _, err := s.db.Exec(
		`INSERT INTO message_events (message_id, user_id, device_id, kind, at)
		 VALUES (?, ?, ?, ?, ?)`,
		messageID, userID, deviceID, kind, s.timestamp()); err != nil {
		return fmt.Errorf("recording the message event: %w", err)
	}
	return nil
}

// TimelineOf returns a message's whole history, oldest first, with the
// names resolved: this is what the distribution panel shows.
func (s *Store) TimelineOf(messageID int64) ([]MessageEvent, error) {
	rows, err := s.db.Query(
		`SELECT e.id, e.message_id, e.user_id, u.username,
		        e.device_id, IFNULL(d.name, ''), e.kind, e.at
		   FROM message_events e
		   JOIN users u ON u.id = e.user_id
		   LEFT JOIN devices d ON d.id = e.device_id
		  WHERE e.message_id = ?
		  ORDER BY e.id`, messageID)
	if err != nil {
		return nil, fmt.Errorf("reading the timeline: %w", err)
	}
	defer rows.Close()

	events := []MessageEvent{}
	for rows.Next() {
		var (
			event    MessageEvent
			deviceID sql.NullInt64
			at       string
		)
		if err := rows.Scan(&event.ID, &event.MessageID, &event.UserID, &event.Username,
			&deviceID, &event.Device, &event.Kind, &at); err != nil {
			return nil, fmt.Errorf("reading a timeline entry: %w", err)
		}
		if deviceID.Valid {
			value := deviceID.Int64
			event.DeviceID = &value
		}
		event.At, _ = parseTime(at)
		events = append(events, event)
	}
	return events, rows.Err()
}

// MarkRead records that a user read a message. Idempotent, and per user:
// one member reading leaves it unread for the others, which is the whole
// point of the shared-stream model.
func (s *Store) MarkRead(messageID, userID int64, deviceID *int64) (bool, error) {
	now := s.timestamp()

	result, err := s.db.Exec(
		`INSERT INTO message_reads (message_id, user_id, read_at) VALUES (?, ?, ?)
		 ON CONFLICT (message_id, user_id) DO NOTHING`, messageID, userID, now)
	if err != nil {
		return false, fmt.Errorf("marking as read: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("reading the result: %w", err)
	}
	if affected == 0 {
		// Already read: no second entry in the timeline either.
		return false, nil
	}

	if err := s.RecordMessageEvent(messageID, userID, deviceID, MessageRead); err != nil {
		return true, err
	}
	return true, nil
}

// MarkChannelRead marks everything up to and including uptoID. Returns the
// ids actually newly marked, so the caller can tell the user's other
// devices exactly what changed.
func (s *Store) MarkChannelRead(channelID, userID, uptoID int64, deviceID *int64) ([]int64, error) {
	sqlText := `SELECT m.id FROM messages m
	             WHERE m.channel_id = ?
	               AND NOT EXISTS (SELECT 1 FROM message_reads r
	                                WHERE r.message_id = m.id AND r.user_id = ?)`
	args := []any{channelID, userID}
	if uptoID > 0 {
		sqlText += ` AND m.id <= ?`
		args = append(args, uptoID)
	}

	rows, err := s.db.Query(sqlText, args...)
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

// UnreadCounts returns, per channel the user belongs to, how many messages
// they have not read. Channels with nothing unread are absent.
func (s *Store) UnreadCounts(userID int64) (map[int64]int, error) {
	rows, err := s.db.Query(
		`SELECT m.channel_id, COUNT(*)
		   FROM messages m
		   JOIN channel_members c ON c.channel_id = m.channel_id AND c.user_id = ?
		  WHERE NOT EXISTS (SELECT 1 FROM message_reads r
		                     WHERE r.message_id = m.id AND r.user_id = ?)
		  GROUP BY m.channel_id`, userID, userID)
	if err != nil {
		return nil, fmt.Errorf("counting the unread messages: %w", err)
	}
	defer rows.Close()

	counts := map[int64]int{}
	for rows.Next() {
		var channelID int64
		var count int
		if err := rows.Scan(&channelID, &count); err != nil {
			return nil, fmt.Errorf("reading a count: %w", err)
		}
		counts[channelID] = count
	}
	return counts, rows.Err()
}

// ReadMessageIDs returns which of the given messages the user has read, so
// a listing can be annotated in one query rather than one per message.
func (s *Store) ReadMessageIDs(userID int64, messageIDs []int64) (map[int64]bool, error) {
	read := map[int64]bool{}
	if len(messageIDs) == 0 {
		return read, nil
	}

	placeholders := make([]byte, 0, len(messageIDs)*2)
	args := []any{userID}
	for i, id := range messageIDs {
		if i > 0 {
			placeholders = append(placeholders, ',')
		}
		placeholders = append(placeholders, '?')
		args = append(args, id)
	}

	rows, err := s.db.Query(
		`SELECT message_id FROM message_reads WHERE user_id = ? AND message_id IN (`+
			string(placeholders)+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("reading the read state: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("reading an entry: %w", err)
		}
		read[id] = true
	}
	return read, rows.Err()
}

// AckedSeq reports how far a device has acknowledged the stream.
func (s *Store) AckedSeq(deviceID int64) (int64, error) {
	var seq int64
	if err := s.db.QueryRow(
		`SELECT acked_seq FROM devices WHERE id = ?`, deviceID).Scan(&seq); err != nil {
		return 0, fmt.Errorf("reading the acknowledged sequence: %w", err)
	}
	return seq, nil
}

// RecordDelivered closes the loop for everything the device acknowledged
// beyond its previous mark: the messages in that range become delivered on
// that device. A sent without a delivered is what the reliability figure
// counts as a miss.
func (s *Store) RecordDelivered(userID, deviceID, throughSeq int64) (int, error) {
	previous, err := s.AckedSeq(deviceID)
	if err != nil {
		return 0, err
	}
	if throughSeq <= previous {
		return 0, nil
	}

	rows, err := s.db.Query(
		`SELECT payload FROM events
		  WHERE user_id = ? AND kind = 'message.new' AND seq > ? AND seq <= ?`,
		userID, previous, throughSeq)
	if err != nil {
		return 0, fmt.Errorf("reading the acknowledged events: %w", err)
	}

	messageIDs := []int64{}
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			rows.Close()
			return 0, fmt.Errorf("reading an event: %w", err)
		}
		var envelope struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal([]byte(payload), &envelope); err == nil && envelope.ID != 0 {
			messageIDs = append(messageIDs, envelope.ID)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	for _, messageID := range messageIDs {
		if err := s.RecordMessageEvent(messageID, userID, &deviceID, MessageDelivered); err != nil {
			return 0, err
		}
	}

	if _, err := s.db.Exec(
		`UPDATE devices SET acked_seq = ? WHERE id = ?`, throughSeq, deviceID); err != nil {
		return len(messageIDs), fmt.Errorf("updating the acknowledged sequence: %w", err)
	}

	return len(messageIDs), nil
}
