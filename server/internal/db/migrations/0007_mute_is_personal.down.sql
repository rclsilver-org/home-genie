CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

ALTER TABLE users DROP COLUMN muted_until;
