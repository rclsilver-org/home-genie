-- Quiet hours get their global level back.
--
-- The design called for that shape from the start — a default, and a
-- per-channel override — and the reminder policies already had it, with their
-- `channel_id NULL` row. I lost it when moving the windows out of the
-- policies: `quiet_hours.channel_id` was NOT NULL, so the same night had to be
-- set on every channel, and one would be forgotten.
--
-- SQLite cannot make a column nullable in place, so the table is rebuilt.
-- Uniqueness goes through an index on IFNULL(channel_id, -1), as for the
-- reminder policies: a primary key would not tell two global rows apart, NULL
-- never being equal to itself.
CREATE TABLE quiet_hours_new (
    channel_id INTEGER REFERENCES channels (id) ON DELETE CASCADE,
    severity   TEXT    NOT NULL DEFAULT '',
    quiet_from TEXT    NOT NULL,
    quiet_to   TEXT    NOT NULL
);

INSERT INTO quiet_hours_new (channel_id, severity, quiet_from, quiet_to)
SELECT channel_id, severity, quiet_from, quiet_to FROM quiet_hours;

DROP TABLE quiet_hours;
ALTER TABLE quiet_hours_new RENAME TO quiet_hours;

CREATE UNIQUE INDEX idx_quiet_hours_scope
    ON quiet_hours (IFNULL(channel_id, -1), severity);
