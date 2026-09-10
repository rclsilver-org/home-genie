-- Core schema.
--
-- Two conventions hold throughout:
--   * timestamps are stored as RFC3339 UTC strings, so the database stays
--     readable with the sqlite3 CLI during an incident;
--   * secrets (device tokens, publish tokens) are stored hashed, never in
--     clear.

CREATE TABLE users (
    id            INTEGER PRIMARY KEY,
    username      TEXT    NOT NULL UNIQUE,
    display_name  TEXT    NOT NULL DEFAULT '',
    -- Set only for the local break-glass account; OIDC users have none.
    password_hash TEXT,
    -- OIDC subject. Null for the local account.
    oidc_subject  TEXT UNIQUE,
    is_admin      INTEGER NOT NULL DEFAULT 0 CHECK (is_admin IN (0, 1)),
    created_at    TEXT    NOT NULL
);

CREATE TABLE devices (
    id              INTEGER PRIMARY KEY,
    user_id         INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name            TEXT    NOT NULL,
    platform        TEXT    NOT NULL,
    -- The extension point for FCM and later APNs: v1 only ever stores 'websocket'.
    transport       TEXT    NOT NULL DEFAULT 'websocket',
    -- Opaque long-lived device token, hashed.
    token_hash      TEXT    NOT NULL UNIQUE,
    -- Null while no socket is live; that is what makes a delivery a miss.
    ws_connected_at TEXT,
    last_seen_at    TEXT,
    created_at      TEXT    NOT NULL
);

CREATE INDEX idx_devices_user ON devices (user_id);

CREATE TABLE channels (
    id          INTEGER PRIMARY KEY,
    -- The segment producers publish to: POST /{slug}.
    slug        TEXT    NOT NULL UNIQUE,
    name        TEXT    NOT NULL,
    description TEXT    NOT NULL DEFAULT '',
    muted_until TEXT,
    created_at  TEXT    NOT NULL
);

CREATE TABLE channel_members (
    channel_id INTEGER NOT NULL REFERENCES channels (id) ON DELETE CASCADE,
    user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role       TEXT    NOT NULL CHECK (role IN ('owner', 'writer', 'reader')),
    created_at TEXT    NOT NULL,
    PRIMARY KEY (channel_id, user_id)
);

CREATE INDEX idx_channel_members_user ON channel_members (user_id);

-- Machines have no account: they carry a write-only token scoped to one channel.
CREATE TABLE publish_tokens (
    id           INTEGER PRIMARY KEY,
    channel_id   INTEGER NOT NULL REFERENCES channels (id) ON DELETE CASCADE,
    name         TEXT    NOT NULL,
    token_hash   TEXT    NOT NULL UNIQUE,
    last_used_at TEXT,
    revoked_at   TEXT,
    created_at   TEXT    NOT NULL
);

CREATE INDEX idx_publish_tokens_channel ON publish_tokens (channel_id);

-- An Alertmanager alert, as a stateful entity keyed by fingerprint.
CREATE TABLE alerts (
    id               INTEGER PRIMARY KEY,
    channel_id       INTEGER NOT NULL REFERENCES channels (id) ON DELETE CASCADE,
    fingerprint      TEXT    NOT NULL,
    status           TEXT    NOT NULL CHECK (status IN ('firing', 'resolved')),
    severity         TEXT    NOT NULL DEFAULT '',
    labels           TEXT    NOT NULL DEFAULT '{}',
    annotations      TEXT    NOT NULL DEFAULT '{}',
    generator_url    TEXT    NOT NULL DEFAULT '',
    started_at       TEXT    NOT NULL,
    resolved_at      TEXT,
    acked_by         INTEGER REFERENCES users (id) ON DELETE SET NULL,
    acked_at         TEXT,
    reminder_count   INTEGER NOT NULL DEFAULT 0,
    next_reminder_at TEXT,
    -- One live alert per fingerprint and channel; resolved ones keep their row.
    UNIQUE (channel_id, fingerprint, started_at)
);

CREATE INDEX idx_alerts_open ON alerts (status, next_reminder_at);

CREATE TABLE messages (
    id         INTEGER PRIMARY KEY,
    channel_id INTEGER NOT NULL REFERENCES channels (id) ON DELETE CASCADE,
    -- Null for a plain notification; set when the message reports on an alert.
    alert_id   INTEGER REFERENCES alerts (id) ON DELETE CASCADE,
    title      TEXT    NOT NULL DEFAULT '',
    body       TEXT    NOT NULL DEFAULT '',
    priority   INTEGER NOT NULL DEFAULT 3 CHECK (priority BETWEEN 1 AND 5),
    tags       TEXT    NOT NULL DEFAULT '[]',
    click_url  TEXT    NOT NULL DEFAULT '',
    actions    TEXT    NOT NULL DEFAULT '[]',
    created_at TEXT    NOT NULL
);

CREATE INDEX idx_messages_channel ON messages (channel_id, id);

-- Append-only log of what happened to a message, per user and per device.
-- This is the source of truth behind the distribution panel, the diagnostic
-- view and the reliability metrics: a 'sent' never followed by a 'delivered'
-- is a miss.
CREATE TABLE message_events (
    id         INTEGER PRIMARY KEY,
    message_id INTEGER NOT NULL REFERENCES messages (id) ON DELETE CASCADE,
    user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    device_id  INTEGER REFERENCES devices (id) ON DELETE SET NULL,
    kind       TEXT    NOT NULL CHECK (kind IN (
                   'queued', 'sent', 'delivered', 'read', 'acked', 'dismissed')),
    at         TEXT    NOT NULL
);

CREATE INDEX idx_message_events_message ON message_events (message_id, id);

-- Denormalisation of the 'read' events, so unread counts do not aggregate the
-- whole log. Read state is per user: one member reading a message leaves it
-- unread for the others.
CREATE TABLE message_reads (
    message_id INTEGER NOT NULL REFERENCES messages (id) ON DELETE CASCADE,
    user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    read_at    TEXT    NOT NULL,
    PRIMARY KEY (message_id, user_id)
);

CREATE INDEX idx_message_reads_user ON message_reads (user_id);

-- Reminder cadence. A row with channel_id NULL is the default for a severity;
-- a row with a channel_id overrides it. The most specific one wins.
CREATE TABLE reminder_policies (
    id               INTEGER PRIMARY KEY,
    channel_id       INTEGER REFERENCES channels (id) ON DELETE CASCADE,
    severity         TEXT    NOT NULL,
    interval_seconds INTEGER NOT NULL CHECK (interval_seconds >= 0),
    quiet_from       TEXT,
    quiet_to         TEXT,
    enabled          INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1))
);

CREATE UNIQUE INDEX idx_reminder_policies_scope
    ON reminder_policies (IFNULL(channel_id, -1), severity);

-- Per-user ordered log carrying the monotonic seq the clients resynchronise
-- on. A socket killed by the system loses nothing: it replays from its last seq.
CREATE TABLE events (
    seq        INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind       TEXT    NOT NULL,
    payload    TEXT    NOT NULL DEFAULT '{}',
    created_at TEXT    NOT NULL
);

CREATE INDEX idx_events_user_seq ON events (user_id, seq);
