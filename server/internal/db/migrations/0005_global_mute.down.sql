-- The mutes in force are lost, which costs nothing: they never did anything.
ALTER TABLE channels ADD COLUMN muted_until TEXT;

DROP TABLE settings;
