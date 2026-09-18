package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Alert status, mirroring Alertmanager's own vocabulary.
const (
	AlertFiring   = "firing"
	AlertResolved = "resolved"
)

// SeverityCritical is the one severity the rest of the system treats
// specially: it is the level that may be silenced only on purpose.
const SeverityCritical = "critical"

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
	// Occurrences counts the deliveries Alertmanager made for this alert:
	// one that beats forty times does not call for the same reaction as one
	// that fired once.
	Occurrences int
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

// Body prefers the description. Without one it names the rule and the
// machine rather than the machine alone: a notification whose second line
// reads "laptop" says nothing about what is wrong there.
func (a Alert) Body() string {
	if description := a.Annotations["description"]; description != "" {
		return description
	}
	name, instance := a.Labels["alertname"], a.Labels["instance"]
	switch {
	case name != "" && instance != "":
		return name + " — " + instance
	case name != "":
		return name
	default:
		return instance
	}
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

	// occurrences + 1: this is what turns a silent refresh into a piece of
	// information rather than a plain non-event.
	if _, err := s.db.Exec(
		`UPDATE alerts SET severity = ?, labels = ?, annotations = ?, generator_url = ?,
		                  occurrences = occurrences + 1
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
	// OnlyUnacked is the set that actually demands an action: still firing
	// and nobody has taken it.
	OnlyUnacked bool
	// OnlyClosed is the opposite view: what is over, read as history.
	OnlyClosed bool
	// Severity filters on the Alertmanager label, empty meaning any.
	Severity string
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
	       a.reminder_count, a.next_reminder_at, a.occurrences
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
		&alert.ReminderCount, &nextAt, &alert.Occurrences)
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

// AlertsForUser lists the alerts across every channel the user belongs to,
// most recent first.
//
// A single query rather than one per channel: the alert console is the
// application's home screen, so it is fetched on every launch and on every
// event, and fanning out would make its cost grow with the number of
// channels for no reason.
func (s *Store) AlertsForUser(userID int64, query AlertQuery) ([]Alert, error) {
	if query.Limit <= 0 || query.Limit > 200 {
		query.Limit = 100
	}

	sqlText := alertColumns + `
	     JOIN channel_members m ON m.channel_id = a.channel_id AND m.user_id = ?`
	args := []any{userID}

	conditions := []string{}
	if query.OnlyOpen || query.OnlyUnacked {
		conditions = append(conditions, "a.status = ?")
		args = append(args, AlertFiring)
	}
	if query.OnlyUnacked {
		conditions = append(conditions, "a.acked_at IS NULL")
	}
	if query.OnlyClosed {
		conditions = append(conditions, "a.status = ?")
		args = append(args, AlertResolved)
	}
	if query.Severity != "" {
		conditions = append(conditions, "a.severity = ?")
		args = append(args, query.Severity)
	}
	if len(conditions) > 0 {
		sqlText += " WHERE " + strings.Join(conditions, " AND ")
	}

	// A closed list is history, and history reads from its end: what was
	// resolved last is what one is looking for. Open alerts keep their own
	// order, where the oldest untreated one is the one that matters.
	if query.OnlyClosed {
		sqlText += ` ORDER BY a.resolved_at DESC, a.id DESC LIMIT ?`
	} else {
		sqlText += ` ORDER BY a.started_at DESC, a.id DESC LIMIT ?`
	}
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

// AlertLogEntry is one line of an alert's history.
type AlertLogEntry struct {
	At   time.Time
	Kind string
	// Detail carries what the line is about — a message title, who
	// acknowledged, how many deliveries.
	Detail string
}

// Alert log kinds, part of the client contract.
const (
	AlertLogOpened   = "opened"
	AlertLogNotified = "notified"
	AlertLogReminded = "reminded"
	AlertLogRepeated = "repeated"
	AlertLogAcked    = "acked"
	AlertLogResolved = "resolved"
)

// LogOf builds an alert's history from what is already recorded, rather than
// keeping a second journal in parallel: the messages it produced, and its own
// lifecycle fields. Two sources of truth for the same events would be one too
// many, and they would drift.
func (s *Store) LogOf(alert Alert) ([]AlertLogEntry, error) {
	entries := []AlertLogEntry{{
		At:     alert.StartedAt,
		Kind:   AlertLogOpened,
		Detail: fmt.Sprintf("severity %s", orUnknown(alert.Severity)),
	}}

	rows, err := s.db.Query(
		`SELECT title, created_at FROM messages WHERE alert_id = ? ORDER BY id`, alert.ID)
	if err != nil {
		return nil, fmt.Errorf("reading the alert's messages: %w", err)
	}
	defer rows.Close()

	type message struct {
		title string
		at    time.Time
	}
	var messages []message
	for rows.Next() {
		var title, created string
		if err := rows.Scan(&title, &created); err != nil {
			return nil, fmt.Errorf("reading a message: %w", err)
		}
		at, _ := parseTime(created)
		messages = append(messages, message{title: title, at: at})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// A resolved alert's last message is its resolution, and the resolution
	// already has an entry of its own further down. Counting it as a reminder
	// showed the closure twice — once as a reminder that never fired, once as
	// the closure — and made it look as though a closed alert kept insisting.
	if alert.ResolvedAt != nil && len(messages) > 0 {
		messages = messages[:len(messages)-1]
	}

	for i, m := range messages {
		kind := AlertLogReminded
		if i == 0 {
			kind = AlertLogNotified
		}
		entries = append(entries, AlertLogEntry{At: m.at, Kind: kind, Detail: m.title})
	}

	// Alertmanager repeats are not messages — that is the whole point of the
	// silent refresh — so they only appear as a count.
	if alert.Occurrences > 1 {
		entries = append(entries, AlertLogEntry{
			At:     alert.StartedAt,
			Kind:   AlertLogRepeated,
			Detail: fmt.Sprintf("%d deliveries from Alertmanager", alert.Occurrences),
		})
	}

	if alert.AckedAt != nil {
		entries = append(entries, AlertLogEntry{
			At: *alert.AckedAt, Kind: AlertLogAcked,
			Detail: "by " + orUnknown(alert.AckedByName),
		})
	}
	if alert.ResolvedAt != nil {
		entries = append(entries, AlertLogEntry{
			At: *alert.ResolvedAt, Kind: AlertLogResolved, Detail: "by Alertmanager",
		})
	}

	sort.SliceStable(entries, func(i, j int) bool { return entries[i].At.Before(entries[j].At) })
	return entries, nil
}

func orUnknown(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}

// UnackAlert takes an acknowledgement back. Reporting whether anything
// changed lets the caller stay quiet about an alert that was not
// acknowledged to begin with, rather than announcing an event that did not
// happen.
func (s *Store) UnackAlert(id int64) (bool, error) {
	result, err := s.db.Exec(
		`UPDATE alerts SET acked_by = NULL, acked_at = NULL
		  WHERE id = ? AND acked_at IS NOT NULL`, id)
	if err != nil {
		return false, fmt.Errorf("taking the acknowledgement back: %w", err)
	}
	affected, _ := result.RowsAffected()
	return affected > 0, nil
}

// HistoryBucket is one slice of the alert history: how many alerts started
// within it, and how many of those were critical.
type HistoryBucket struct {
	At       time.Time
	Total    int
	Critical int
}

// HistoryLimits bound what a caller may ask of the history.
//
// A window of a month and ninety-six slices is far past anything a phone can
// draw legibly; the point is only that a mistyped query cannot ask the
// database to group a decade into ten thousand columns.
const (
	MinHistoryBuckets = 4
	MaxHistoryBuckets = 96
	MaxHistoryWindow  = 30 * 24 * time.Hour
)

// AlertHistoryFor returns how many alerts opened per slice of time over the
// window ending now, across the channels the user belongs to.
//
// Counted in the database rather than by handing the rows over and letting
// the caller add them up. The listing endpoints are capped, and a chart built
// from a capped list stops growing without saying so — the very failure the
// filter counts had to work around. Here the aggregation is the answer, so
// volume changes the cost and never the result.
//
// Empty slices come back as zeros rather than being left out. A chart has to
// draw the quiet hours, and a caller filling the gaps itself would be a
// second place for the arithmetic to go wrong.
func (s *Store) AlertHistoryFor(
	userID int64, window time.Duration, buckets int,
) ([]HistoryBucket, error) {
	if buckets < MinHistoryBuckets {
		buckets = MinHistoryBuckets
	}
	if buckets > MaxHistoryBuckets {
		buckets = MaxHistoryBuckets
	}
	if window <= 0 || window > MaxHistoryWindow {
		window = MaxHistoryWindow
	}

	width := window / time.Duration(buckets)
	if width <= 0 {
		return nil, fmt.Errorf("a window of %s cannot be cut into %d", window, buckets)
	}
	// Aligned on the slice width so the columns do not shift under the chart
	// between two refreshes a few seconds apart — but rounded *up*, so the
	// last slice is the one in progress. Truncating alone put the end of the
	// window as much as one slice in the past, which dropped the alerts that
	// opened since: the most recent ones, and the reason anyone looks.
	end := s.now().UTC().Truncate(width).Add(width)
	start := end.Add(-window)

	counted := make([]HistoryBucket, buckets)
	for i := range counted {
		counted[i] = HistoryBucket{At: start.Add(time.Duration(i) * width)}
	}

	rows, err := s.db.Query(
		`SELECT CAST((julianday(a.started_at) - julianday(?)) * 86400 / ? AS INTEGER) AS slice,
		        COUNT(*),
		        SUM(CASE WHEN a.severity = ? THEN 1 ELSE 0 END)
		   FROM alerts a
		   JOIN channel_members m ON m.channel_id = a.channel_id AND m.user_id = ?
		  WHERE a.started_at >= ? AND a.started_at < ?
		  GROUP BY slice`,
		start.Format(time.RFC3339), int64(width.Seconds()), SeverityCritical, userID,
		start.Format(time.RFC3339), end.Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("counting the alert history: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var slice, total, critical int
		if err := rows.Scan(&slice, &total, &critical); err != nil {
			return nil, fmt.Errorf("reading a slice: %w", err)
		}
		// A row landing outside is a clock that moved between the bounds and
		// the grouping, not something to drop the whole answer for.
		if slice >= 0 && slice < buckets {
			counted[slice].Total = total
			counted[slice].Critical = critical
		}
	}
	return counted, rows.Err()
}

// LabelCount is one row of the ranking: a label's value and how often it
// appeared over the window.
type LabelCount struct {
	Value string
	Total int
}

// TopAlertLabelsFor ranks the values of one label over a window, across the
// channels the user belongs to.
//
// Counted in the database for the same reason as the history: a ranking built
// from the capped listing would quietly become "the top of the last two
// hundred", and it would start lying exactly during the incident storm that
// makes anyone open it.
//
// Alerts carrying no such label are left out rather than grouped under an
// empty name. "What breaks most" has no answer for a row that does not say
// what it is, and a blank leading the ranking would only look like a bug.
func (s *Store) TopAlertLabelsFor(
	userID int64, label string, window time.Duration, limit int,
) ([]LabelCount, error) {
	if label == "" {
		return nil, fmt.Errorf("a label is required")
	}
	if limit <= 0 || limit > 50 {
		limit = 5
	}
	if window <= 0 || window > MaxHistoryWindow {
		window = MaxHistoryWindow
	}
	since := s.now().UTC().Add(-window).Format(time.RFC3339)

	// The labels are a JSON object in the row; SQLite reads one out without
	// the server having to decode every alert to count them.
	rows, err := s.db.Query(
		`SELECT json_extract(a.labels, '$.' || ?) AS value, COUNT(*) AS total
		   FROM alerts a
		   JOIN channel_members m ON m.channel_id = a.channel_id AND m.user_id = ?
		  WHERE a.started_at >= ? AND value IS NOT NULL AND value <> ''
		  GROUP BY value
		  ORDER BY total DESC, value ASC
		  LIMIT ?`, label, userID, since, limit)
	if err != nil {
		return nil, fmt.Errorf("ranking the alerts: %w", err)
	}
	defer rows.Close()

	ranked := []LabelCount{}
	for rows.Next() {
		var entry LabelCount
		if err := rows.Scan(&entry.Value, &entry.Total); err != nil {
			return nil, fmt.Errorf("reading a rank: %w", err)
		}
		ranked = append(ranked, entry)
	}
	return ranked, rows.Err()
}
