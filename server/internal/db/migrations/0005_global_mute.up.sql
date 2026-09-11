-- The mute leaves the channel.
--
-- Attached to a channel, it had to be set as many times as there are channels
-- to get what one actually wants — "be quiet, I am working on it". And it did
-- nothing at all: its value was stored and displayed, and no delivery path
-- ever read it.
--
-- A key/value pair rather than a dedicated table: this will not be the last
-- installation-wide setting, and one table per setting is paid in migrations.
CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- DROP COLUMN rather than SQLite's usual table rebuild: `DROP TABLE channels`
-- runs an implicit DELETE, which fires the ON DELETE CASCADE of everything
-- referencing a channel — messages, alerts, members, tokens, cadences, quiet
-- windows. The rebuild would have emptied the database while claiming to
-- remove one column.
ALTER TABLE channels DROP COLUMN muted_until;
