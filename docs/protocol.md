# Client ↔ server protocol

The contract between `hnotifd` and its clients. Versioned here so that an iOS port
(out of scope for v1) can start from an explicit reference rather than from the
Android code.

**Status: skeleton.** Request and response schemas are filled in as step 1 progresses.

## Authentication

| Bearer | Token | Scope |
|---|---|---|
| Device (human) | opaque device token, `Authorization: Bearer hnd_…` | the channels the user is a member of |
| Machine (producer) | publish token, `Authorization: Bearer hnp_…` | **write only**, on a single channel |

Tokens are drawn from 32 random bytes and stored hashed with SHA-256 — not with
argon2: they carry 256 bits of entropy, so there is no dictionary to defend against,
and they are verified on every request. The break-glass account's password, on the
other hand, is argon2id.

The cleartext value is returned once only, at creation.

### `POST /api/v1/auth/login` *(implemented)*

Enrols a device against the **local break-glass account**. OIDC enrolment sits beside
it and returns the same kind of token, which is precisely what keeps the identity
provider off the path of every request.

```json
{ "username": "thomas", "password": "…", "device_name": "phone", "platform": "android" }
```

→ `201` with `{ "token": "hnd_…", "user": {…}, "device": {…} }`.

A missing account and a wrong password return the same `401`, the same body, and cost
the same time: verification runs against a decoy digest when the account is unknown.

Unknown fields in the body are refused with `400`, so that a client-side typo is an
error rather than a setting silently ignored.

### `GET /api/v1/me` *(implemented)*

Returns the caller and their devices, each with its connection state, its last
activity, and a `current` flag on the one calling. This is what the application's
connection diagnostics reads.

## Channels and rights *(implemented)*

A channel carries a `slug`, which is **the publish path**: `POST /{slug}`. It is
therefore constrained to `[a-z0-9-]`, 1 to 64 characters, with no leading or trailing
dash, and some names are reserved (`api`, `healthz`, `metrics`, `admin`…) because they
would shadow the API.

Roles on a channel: `owner` (everything, members and tokens included), `writer`
(publish from the application), `reader` (read and acknowledge). The creator becomes
`owner`, in the same transaction as the creation — a channel with no owner could only
be repaired by hand in the database.

**A non-member gets 404, never 403.** Answering "forbidden" would confirm the
existence of the channel to somebody who has no business knowing. The 403 is reserved
for members who lack the sufficient role: for them the channel is not a secret, only
its administration is.

| Route | Right required |
|---|---|
| `GET /api/v1/channels` | authenticated — lists only their channels, with their role |
| `POST /api/v1/channels` | authenticated — becomes `owner` |
| `GET /api/v1/channels/{id}` | member |
| `PATCH /api/v1/channels/{id}` | `owner` — `name`, `description`, `muted_until` |
| `DELETE /api/v1/channels/{id}` | `owner` — cascades to members, tokens, alerts, messages |
| `GET /api/v1/channels/{id}/members` | member |
| `PUT /api/v1/channels/{id}/members/{username}` | `owner` |
| `DELETE /api/v1/channels/{id}/members/{username}` | `owner` |
| `GET /api/v1/channels/{id}/tokens` | `owner` |
| `POST /api/v1/channels/{id}/tokens` | `owner` |
| `DELETE /api/v1/channels/{id}/tokens/{tokenID}` | `owner` |

On a `PATCH`, an absent `muted_until` leaves the mute unchanged, an empty string clears
it, an RFC3339 instant sets it. An unreadable instant is a `400`, not an ignored field.

Removing the **last** `owner` returns `409`: the channel would become unadministrable.

### Publish tokens

Created on a channel, **write only** and bound to that channel alone. The cleartext
value appears only at creation; the list never shows it again. Revocation is immediate
— a revoked token no longer resolves at all — but the row is kept, so that the history
of who could publish, and of their last publication, stays visible.

A publish token and a device token are **never** interchangeable: two distinct
middlewares, not one condition inside a single one. A machine does not read, a device
does not publish on behalf of a producer.

## Ingest

### `POST /{canal}` — compatible ntfy

Accepts both ntfy shapes: a plain text body with headers (`Title`, `Priority`, `Tags`,
`Click`, `Actions`) or a JSON body. This is the path of the existing producers.

### `POST /api/v1/ingest/alertmanager/{channel}`

Alertmanager v4 webhook. One alert entity per `fingerprint`.

## API cliente

- `GET /api/v1/messages`
- `POST /api/v1/messages/{id}/read`, `POST /api/v1/channels/{id}/read?up_to_seq=N`
- `GET /api/v1/messages/{id}/timeline`
- `GET /api/v1/alerts`, `POST /api/v1/alerts/{id}/ack`
- `POST /api/v1/devices`

The endpoints marked *(implemented)* above are available; the others land as step 1
progresses.

## WebSocket

`GET /api/v1/ws?since_seq=N` — a replay of everything after `since_seq`, then real
time. A periodic heartbeat to detect a socket that is dead but not closed.

## Divers

- `GET /healthz` — status and build identity
- `GET /metrics` — Prometheus format (step 1)
