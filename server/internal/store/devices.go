package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Transport names the way notifications reach a device. v1 only ever writes
// "websocket"; the column exists so adding FCM, then APNs, does not require
// a migration on a live database.
const TransportWebSocket = "websocket"

// Device is an enrolled application instance. It is the unit the delivery
// timeline records against, not the user: knowing a message reached the
// phone but not the tablet is the point.
type Device struct {
	ID            int64
	UserID        int64
	Name          string
	Platform      string
	Transport     string
	WSConnectedAt *time.Time
	LastSeenAt    *time.Time
	CreatedAt     time.Time
}

// IsConnected reports whether a live socket is attached. A delivery to a
// device that is not connected is what the reliability metric counts as a
// miss.
func (d Device) IsConnected() bool { return d.WSConnectedAt != nil }

// CreateDevice enrols a device and stores the hash of its token. The clear
// token is the caller's to return once and never again.
func (s *Store) CreateDevice(userID int64, name, platform, tokenHash string) (Device, error) {
	if tokenHash == "" {
		return Device{}, fmt.Errorf("a device needs a token")
	}

	created := s.timestamp()
	result, err := s.db.Exec(
		`INSERT INTO devices (user_id, name, platform, transport, token_hash, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		userID, name, platform, TransportWebSocket, tokenHash, created)
	if err != nil {
		return Device{}, fmt.Errorf("enrolling the device: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return Device{}, fmt.Errorf("reading the new id: %w", err)
	}
	at, _ := parseTime(created)

	return Device{
		ID:        id,
		UserID:    userID,
		Name:      name,
		Platform:  platform,
		Transport: TransportWebSocket,
		CreatedAt: at,
	}, nil
}

// DeviceByTokenHash resolves a bearer token to its device and owner in one
// round trip, since every authenticated request needs both.
func (s *Store) DeviceByTokenHash(tokenHash string) (Device, User, error) {
	var (
		device    Device
		user      User
		password  sql.NullString
		subject   sql.NullString
		isAdmin   int
		connected sql.NullString
		lastSeen  sql.NullString
		created   string
		userMade  string
	)

	err := s.db.QueryRow(
		`SELECT d.id, d.user_id, d.name, d.platform, d.transport,
		        d.ws_connected_at, d.last_seen_at, d.created_at,
		        u.id, u.username, u.display_name, u.password_hash,
		        u.oidc_subject, u.is_admin, u.created_at
		   FROM devices d
		   JOIN users u ON u.id = d.user_id
		  WHERE d.token_hash = ?`, tokenHash).
		Scan(&device.ID, &device.UserID, &device.Name, &device.Platform, &device.Transport,
			&connected, &lastSeen, &created,
			&user.ID, &user.Username, &user.DisplayName, &password,
			&subject, &isAdmin, &userMade)
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, User{}, ErrNotFound
	}
	if err != nil {
		return Device{}, User{}, fmt.Errorf("reading the device: %w", err)
	}

	device.WSConnectedAt = optionalTime(connected)
	device.LastSeenAt = optionalTime(lastSeen)
	device.CreatedAt, _ = parseTime(created)

	user.PasswordHash = nullString(password)
	user.OIDCSubject = nullString(subject)
	user.IsAdmin = isAdmin == 1
	user.CreatedAt, _ = parseTime(userMade)

	return device, user, nil
}

// TouchDevice records that the device was seen. Called on authenticated
// requests, it is what makes a stale device visible in the diagnostics.
func (s *Store) TouchDevice(id int64) error {
	if _, err := s.db.Exec(
		`UPDATE devices SET last_seen_at = ? WHERE id = ?`, s.timestamp(), id); err != nil {
		return fmt.Errorf("updating the device: %w", err)
	}
	return nil
}

// DevicesByUser lists a user's devices, most recently seen first.
func (s *Store) DevicesByUser(userID int64) ([]Device, error) {
	rows, err := s.db.Query(
		`SELECT id, user_id, name, platform, transport,
		        ws_connected_at, last_seen_at, created_at
		   FROM devices WHERE user_id = ?
		  ORDER BY IFNULL(last_seen_at, created_at) DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("listing the devices: %w", err)
	}
	defer rows.Close()

	var devices []Device
	for rows.Next() {
		var (
			device    Device
			connected sql.NullString
			lastSeen  sql.NullString
			created   string
		)
		if err := rows.Scan(&device.ID, &device.UserID, &device.Name, &device.Platform,
			&device.Transport, &connected, &lastSeen, &created); err != nil {
			return nil, fmt.Errorf("reading a device: %w", err)
		}
		device.WSConnectedAt = optionalTime(connected)
		device.LastSeenAt = optionalTime(lastSeen)
		device.CreatedAt, _ = parseTime(created)
		devices = append(devices, device)
	}

	return devices, rows.Err()
}

func optionalTime(value sql.NullString) *time.Time {
	if !value.Valid || value.String == "" {
		return nil
	}
	parsed, err := parseTime(value.String)
	if err != nil {
		return nil
	}
	return &parsed
}
