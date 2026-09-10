# Client ↔ server protocol

The contract between `hnotifd` and its clients. Versioned here so that an iOS port
(out of scope for v1) can start from an explicit reference rather than from the
Android code.

**Status: skeleton.** Request and response schemas are filled in as step 1 progresses.

## Authentication

| Bearer | Token | Scope |
|---|---|---|
| Device (human) | opaque device token, `Authorization: Bearer …` | the channels the user is a member of |
| Machine (producer) | publish token, `Authorization: Bearer …` | **write only**, on a single channel |

## Ingestion

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

## WebSocket

`GET /api/v1/ws?since_seq=N` — a replay of everything after `since_seq`, then real
time. A periodic heartbeat to detect a socket that is dead but not closed.

## Divers

- `GET /healthz` — status and build identity
- `GET /metrics` — Prometheus format (step 1)
