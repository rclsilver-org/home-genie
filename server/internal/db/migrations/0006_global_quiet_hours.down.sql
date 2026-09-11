DELETE FROM quiet_hours WHERE channel_id IS NULL;

CREATE TABLE quiet_hours_old (
    channel_id INTEGER NOT NULL REFERENCES channels (id) ON DELETE CASCADE,
    severity   TEXT    NOT NULL DEFAULT '',
    quiet_from TEXT    NOT NULL,
    quiet_to   TEXT    NOT NULL,
    PRIMARY KEY (channel_id, severity)
);

INSERT INTO quiet_hours_old (channel_id, severity, quiet_from, quiet_to)
SELECT channel_id, severity, quiet_from, quiet_to FROM quiet_hours;

DROP INDEX idx_quiet_hours_scope;
DROP TABLE quiet_hours;
ALTER TABLE quiet_hours_old RENAME TO quiet_hours;
