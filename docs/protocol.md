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
## Ingest

### `POST /{canal}` — compatible ntfy

Accepts both ntfy shapes: a plain text body with headers (`Title`, `Priority`, `Tags`,
`Click`, `Actions`) or a JSON body. This is the path of the existing producers.

### `POST /api/v1/ingest/alertmanager/{channel}`

Alertmanager v4 webhook. One alert entity per `fingerprint`.

## API cliente

- `GET/POST/PATCH/DELETE /api/v1/channels[/{id}[/members|/tokens]]`
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
