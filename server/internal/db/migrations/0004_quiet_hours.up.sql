-- Quiet hours become their own thing, and stop belonging to the reminder
-- cadence.
--
-- They answered one question until now — may we insist at this hour — while
-- the one actually asked is broader: may we make noise at all. A film
-- downloaded at three in the morning has no reminder policy to hang a window
-- on, and it is exactly what one does not want to be woken by.
--
-- A row with severity = '' covers the whole channel; a row naming a severity
-- overrides it for that severity alone, the same "most specific wins" rule as
-- the reminder policies.
CREATE TABLE quiet_hours (
    channel_id INTEGER NOT NULL REFERENCES channels (id) ON DELETE CASCADE,
    severity   TEXT    NOT NULL DEFAULT '',
    quiet_from TEXT    NOT NULL,
    quiet_to   TEXT    NOT NULL,
    PRIMARY KEY (channel_id, severity)
);

-- What the policies already carried, moved rather than lost. Defaults
-- (channel_id NULL) are dropped on the way: a quiet window is a property of a
-- place one is notified about, and there is no channel-less notification.
INSERT INTO quiet_hours (channel_id, severity, quiet_from, quiet_to)
SELECT channel_id, severity, quiet_from, quiet_to
  FROM reminder_policies
 WHERE channel_id IS NOT NULL
   AND quiet_from IS NOT NULL AND quiet_from <> ''
   AND quiet_to IS NOT NULL AND quiet_to <> '';

-- SQLite rewrites the table to drop the two columns; the index goes with it.
CREATE TABLE reminder_policies_new (
    id               INTEGER PRIMARY KEY,
    channel_id       INTEGER REFERENCES channels (id) ON DELETE CASCADE,
    severity         TEXT    NOT NULL,
    interval_seconds INTEGER NOT NULL CHECK (interval_seconds >= 0),
    enabled          INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1))
);

INSERT INTO reminder_policies_new (id, channel_id, severity, interval_seconds, enabled)
SELECT id, channel_id, severity, interval_seconds, enabled FROM reminder_policies;

DROP TABLE reminder_policies;
ALTER TABLE reminder_policies_new RENAME TO reminder_policies;

CREATE UNIQUE INDEX idx_reminder_policies_scope
    ON reminder_policies (IFNULL(channel_id, -1), severity);
