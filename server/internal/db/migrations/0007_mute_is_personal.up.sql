-- A mute belongs to whoever sets it.
--
-- It was recorded as an installation-wide setting: whoever set one silenced
-- the house for everybody. That is not what one asks for by muting — one asks
-- not to be disturbed, oneself, for an hour. Silencing other people is a
-- decision that does not need to exist.
--
-- It stays above everything and outside the channels: while muted, its author
-- receives nothing, from any channel, not even a critical.
ALTER TABLE users ADD COLUMN muted_until TEXT;

-- The installation-wide settings table only ever carried the mute, and has
-- nothing left: the quiet hours have their own table, with their default row.
-- An empty table kept "just in case" is a table one forgets to fill correctly
-- the day it is used.
DROP TABLE settings;
