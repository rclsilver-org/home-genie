-- A publish token is already the producer's identity: one per software, with
-- a name someone chose. It only lacked a face and a way back from a message.
--
-- The icon lives here rather than on the message, and the message points at
-- the token. Copying it onto every message would freeze it: changing Sonarr's
-- logo would leave three months of notifications wearing the old one, and the
-- fix would be a backfill. Joined at read time, a new icon repaints the whole
-- history at once.
--
-- The bytes go in the database rather than beside it. A handful of small
-- images is exactly what a BLOB is for, and it keeps one file to back up
-- instead of a directory that can drift out of step with the rows naming it.
ALTER TABLE publish_tokens ADD COLUMN icon BLOB;

-- Served back verbatim, so the type travels with the bytes rather than being
-- guessed from them on every request.
ALTER TABLE publish_tokens ADD COLUMN icon_type TEXT NOT NULL DEFAULT '';

-- Which producer published a message. ON DELETE SET NULL rather than CASCADE:
-- deleting a token must not take the notifications it sent with it. The
-- message loses its face, not its existence.
ALTER TABLE messages ADD COLUMN publish_token_id INTEGER
    REFERENCES publish_tokens (id) ON DELETE SET NULL;

CREATE INDEX idx_messages_publish_token ON messages (publish_token_id);
