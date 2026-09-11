CREATE TABLE reminder_policies_old (
    id               INTEGER PRIMARY KEY,
    channel_id       INTEGER REFERENCES channels (id) ON DELETE CASCADE,
    severity         TEXT    NOT NULL,
    interval_seconds INTEGER NOT NULL CHECK (interval_seconds >= 0),
    quiet_from       TEXT,
    quiet_to         TEXT,
    enabled          INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1))
);

INSERT INTO reminder_policies_old (id, channel_id, severity, interval_seconds,
                                   quiet_from, quiet_to, enabled)
SELECT p.id, p.channel_id, p.severity, p.interval_seconds,
       q.quiet_from, q.quiet_to, p.enabled
  FROM reminder_policies p
  LEFT JOIN quiet_hours q
         ON q.channel_id = p.channel_id AND q.severity = p.severity;

DROP TABLE reminder_policies;
ALTER TABLE reminder_policies_old RENAME TO reminder_policies;

CREATE UNIQUE INDEX idx_reminder_policies_scope
    ON reminder_policies (IFNULL(channel_id, -1), severity);

DROP TABLE quiet_hours;
