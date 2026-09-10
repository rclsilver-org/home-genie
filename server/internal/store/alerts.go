package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Alert status, mirroring Alertmanager's own vocabulary.
const (
	AlertFiring   = "firing"
	AlertResolved = "resolved"
)

// Alert is an Alertmanager alert as a stateful entity, keyed by the
// fingerprint Alertmanager itself computes. Keeping the entity rather than
// a stream of messages is what allows acknowledgement and reminders — the
// thing ntfy could never do.
type Alert struct {
	ID             int64
	ChannelID      int64
	Fingerprint    string
	Status         string
	Severity       string
	Labels         map[string]string
	Annotations    map[string]string
	GeneratorURL   string
	StartedAt      time.Time
	ResolvedAt     *time.Time
	AckedBy        *int64
	AckedByName    string
	AckedAt        *time.Time
	ReminderCount  int
	NextReminderAt *time.Time
}

// IsOpen reports whether the alert is still firing.
func (a Alert) IsOpen() bool { return a.Status == AlertFiring }

// IsAcked reports whether somebody took it.
func (a Alert) IsAcked() bool { return a.AckedAt != nil }

// Title builds a human title from the labels, preferring what Alertmanager
// conventionally carries.
func (a Alert) Title() string {
	if summary := a.Annotations["summary"]; summary != "" {
		return summary
	}
	if name := a.Labels["alertname"]; name != "" {
		return name
	}
	return "Alerte"
}

// Body prefers the description, falling back to the instance so a message
// is never empty.
func (a Alert) Body() string {
	if description := a.Annotations["description"]; description != "" {
		return description
	}
	if instance := a.Labels["instance"]; instance != "" {
		return instance
	}
	return a.Labels["alertname"]
}

// NewAlert is what the Alertmanager webhook yields for one alert.
type NewAlert struct {
	ChannelID    int64
	Fingerprint  string
	Severity     string
	Labels       map[string]string
	Annotations  map[string]string
	GeneratorURL string
	StartedAt    time.Time
}

// OpenAlert returns the firing alert for a fingerprint on a channel, or
// ErrNotFound. Resolved rows are kept for history, so the lookup is scoped
// to the open one.
func (s *Store) OpenAlert(channelID int64, fingerprint string) (Alert, error) {
	return s.scanAlert(s.db.QueryRow(alertColumns+`
		  WHERE a.channel_id = ? AND a.fingerprint = ? AND a.status = ?
		  ORDER BY a.started_at DESC LIMIT 1`,
		channelID, fingerprint, AlertFiring))
}

// AlertByID reads one alert.
func (s *Store) AlertByID(id int64) (Alert, error) {
	return s.scanAlert(s.db.QueryRow(alertColumns+` WHERE a.id = ?`, id))
}

// CreateAlert opens an alert.
func (s *Store) CreateAlert(input NewAlert) (Alert, error) {
	labels, err := json.Marshal(orEmpty(input.Labels))
	if err != nil {
		return Alert{}, fmt.Errorf("encoding the labels: %w", err)
	}
	annotations, err := json.Marshal(orEmpty(input.Annotations))
	if err != nil {
		return Alert{}, fmt.Errorf("encoding the annotations: %w", err)
	}
	if input.StartedAt.IsZero() {
		input.StartedAt = s.now()
	}
	started := input.StartedAt.UTC().Format(time.RFC3339)

	result, err := s.db.Exec(
		`INSERT INTO alerts (channel_id, fingerprint, status, severity, labels,
		                     annotations, generator_url, started_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		input.ChannelID, input.Fingerprint, AlertFiring, input.Severity,
		string(labels), string(annotations), input.GeneratorURL, started)
	if err != nil {
		if isUniqueViolation(err) {
			return Alert{}, ErrConflict
		}
		return Alert{}, fmt.Errorf("opening the alert: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return Alert{}, fmt.Errorf("reading the new id: %w", err)
	}
	return s.AlertByID(id)
}

// RefreshAlert updates a still-firing alert in place. It produces no
// message: Alertmanager repeats an alert while it lasts, and re-notifying
// on every repeat is exactly the noise this design removes.
func (s *Store) RefreshAlert(id int64, input NewAlert) error {
	labels, _ := json.Marshal(orEmpty(input.Labels))
	annotations, _ := json.Marshal(orEmpty(input.Annotations))

	if _, err := s.db.Exec(
		`UPDATE alerts SET severity = ?, labels = ?, annotations = ?, generator_url = ?
		  WHERE id = ?`,
		input.Severity, string(labels), string(annotations), input.GeneratorURL, id); err != nil {
		return fmt.Errorf("refreshing the alert: %w", err)
	}
	return nil
}

// ResolveAlert closes an alert and cancels its reminders.
func (s *Store) ResolveAlert(id int64, at time.Time) error {
	if at.IsZero() {
		at = s.now()
	}
	result, err := s.db.Exec(
		`UPDATE alerts SET status = ?, resolved_at = ?, next_reminder_at = NULL
		  WHERE id = ? AND status = ?`,
		AlertResolved, at.UTC().Format(time.RFC3339), id, AlertFiring)
	if err != nil {
		return fmt.Errorf("resolving the alert: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// AckAlert records who took the alert and stops its reminders. It never
// touches Alertmanager: the acknowledgement is local by design, so the
// alert stays visible in the dashboards and the phone never writes to the alerting
// chain it is watching.
func (s *Store) AckAlert(id, userID int64) (bool, error) {
	result, err := s.db.Exec(
		`UPDATE alerts SET acked_by = ?, acked_at = ?, next_reminder_at = NULL
		  WHERE id = ? AND acked_at IS NULL`,
		userID, s.timestamp(), id)
	if err != nil {
		return false, fmt.Errorf("acknowledging the alert: %w", err)
	}
	affected, _ := result.RowsAffected()
	return affected > 0, nil
}

// SetNextReminder schedules — or with a nil instant, cancels — the next
// reminder.
func (s *Store) SetNextReminder(id int64, at *time.Time, count int) error {
	var value any
	if at != nil {
		value = at.UTC().Format(time.RFC3339)
	}
	if _, err := s.db.Exec(
		`UPDATE alerts SET next_reminder_at = ?, reminder_count = ? WHERE id = ?`,
		value, count, id); err != nil {
		return fmt.Errorf("scheduling the reminder: %w", err)
	}
	return nil
}

// AlertQuery bounds a listing.
type AlertQuery struct {
	ChannelID int64
	// OnlyOpen restricts to firing alerts, which is the console view.
	OnlyOpen bool
	Limit    int
}

// AlertsOf lists a channel's alerts, most recent first.
func (s *Store) AlertsOf(query AlertQuery) ([]Alert, error) {
	if query.Limit <= 0 || query.Limit > 200 {
		query.Limit = 50
	}
	sqlText := alertColumns + ` WHERE a.channel_id = ?`
	args := []any{query.ChannelID}
	if query.OnlyOpen {
		sqlText += ` AND a.status = ?`
		args = append(args, AlertFiring)
	}
	sqlText += ` ORDER BY a.started_at DESC, a.id DESC LIMIT ?`
	args = append(args, query.Limit)

	rows, err := s.db.Query(sqlText, args...)
	if err != nil {
		return nil, fmt.Errorf("listing the alerts: %w", err)
	}
	defer rows.Close()

	alerts := []Alert{}
	for rows.Next() {
		alert, err := scanAlertRow(rows)
		if err != nil {
			return nil, err
		}
		alerts = append(alerts, alert)
	}
	return alerts, rows.Err()
}

const alertColumns = `
	SELECT a.id, a.channel_id, a.fingerprint, a.status, a.severity, a.labels,
	       a.annotations, a.generator_url, a.started_at, a.resolved_at,
	       a.acked_by, IFNULL(u.username, ''), a.acked_at,
	       a.reminder_count, a.next_reminder_at
	  FROM alerts a
	  LEFT JOIN users u ON u.id = a.acked_by`

func (s *Store) scanAlert(row *sql.Row) (Alert, error) {
	alert, err := scanAlertRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Alert{}, ErrNotFound
	}
	return alert, err
}

func scanAlertRow(row rowScanner) (Alert, error) {
	var (
		alert       Alert
		labels      string
		annotations string
		started     string
		resolved    sql.NullString
		ackedBy     sql.NullInt64
		ackedAt     sql.NullString
		nextAt      sql.NullString
	)

	err := row.Scan(&alert.ID, &alert.ChannelID, &alert.Fingerprint, &alert.Status,
		&alert.Severity, &labels, &annotations, &alert.GeneratorURL, &started,
		&resolved, &ackedBy, &alert.AckedByName, &ackedAt,
		&alert.ReminderCount, &nextAt)
	if err != nil {
		return Alert{}, err
	}

	_ = json.Unmarshal([]byte(labels), &alert.Labels)
	_ = json.Unmarshal([]byte(annotations), &alert.Annotations)
	alert.Labels = orEmpty(alert.Labels)
	alert.Annotations = orEmpty(alert.Annotations)
	alert.StartedAt, _ = parseTime(started)
	alert.ResolvedAt = optionalTime(resolved)
	alert.AckedAt = optionalTime(ackedAt)
	alert.NextReminderAt = optionalTime(nextAt)
	if ackedBy.Valid {
		value := ackedBy.Int64
		alert.AckedBy = &value
	}

	return alert, nil
}

func orEmpty(values map[string]string) map[string]string {
	if values == nil {
		return map[string]string{}
	}
	return values
}
