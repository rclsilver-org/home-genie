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

### `GET /api/v1/auth/config` *(implemented)*

What the sign-in screen needs before showing anything: whether OIDC is enabled, and if
so the issuer and the public client id. Unauthenticated, because it is read before
there is any session, and it carries nothing that is not already public in an
authorization URL.

### `POST /api/v1/auth/oidc` *(implemented)*

Enrols a device from an **OIDC ID token**, which the application obtains for itself
with **Authorization Code + PKCE** in a Custom Tab.

```json
{ "id_token": "eyJ…", "device_name": "phone", "platform": "android" }
```

→ `201`, with the same `{token, user, device}` as the local sign-in. The device token
returned is identical in nature: **the identity provider leaves the path of every
request** once enrolment is done, so it can go down without disconnecting anyone from
their alerting tool.

That split has a practical consequence: **no client secret**. A public client with
PKCE does not need one, the application never sees a provider password, and the server
only needs the issuer URL.

Accounts are matched on the **`sub`**, never on the name or the address:

- a rename at the provider finds the same account again and refreshes its name;
- another `sub` carrying the same name **never** inherits the existing account;
- an OIDC sign-in cannot absorb the **local break-glass account** of the same name —
  it gets a derived name (`thomas-oidc`). That account exists precisely to work when
  the provider does not; letting OIDC take it over would destroy the net through the
  very mechanism it rescues.

The audience is verified: a token addressed to another client of the same provider is
refused, otherwise every application of the realm would become a way in here.

An unreachable issuer breaks neither startup nor existing sessions — only new
enrolments fail, and the failure is remembered for a few seconds so that each attempt
does not turn into a long wait.

With no `issuer` configured the endpoint answers `501`: a deployment without an
identity provider fails clearly rather than mysteriously.

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

### `POST /{slug}` — ntfy compatible *(implemented)*

Authenticated by a **publish token** (`Authorization: Bearer hnp_…`), which is only
valid for one channel. This is what makes it possible to switch the media tools and
the image watcher over by changing only a URL and a token.

Two shapes, like ntfy:

**Plain text body + headers.** Every header accepts ntfy's spellings:
`Title` / `X-Title` / `t`, `Priority` / `X-Priority` / `prio` / `p`,
`Tags` / `X-Tags` / `ta`, `Click` / `X-Click`.

```sh
curl -H "Authorization: Bearer hnp_…" \
     -H "Title: Sonarr" -H "Priority: high" -H "Tags: movie,download" \
     -d "Dune has been downloaded" https://example.invalid/notifications
```

**JSON body** (`Content-Type: application/json`) with `title`, `message`, `priority`,
`tags`, `click`, `actions`. Headers win over the body, as they do in ntfy.

`priority` accepts names (`min`, `low`, `default`, `high`, `max`, `urgent`) and the
numbers 1 to 5. A value out of range or unreadable falls back to `default` rather than
failing the notification — losing an alert over a typo would be worse.

If the JSON body carries a `topic`, it must match the slug in the URL: a misconfigured
producer fails with `400` instead of publishing to the channel its token owns.

A message with neither title **nor** body is refused with `400`.

The response mimics ntfy's (`id`, `time`, `event`, `topic`, `title`, `message`,
`priority`, `tags`) so that a producer reading it is not surprised.

**Known limitation:** ntfy's `Actions` header, in its compact syntax, is not
interpreted yet — it is ignored with a warning in the log rather than stored half
understood. None of the producers to migrate uses it, and our own alerts set their
actions natively in JSON.

The Alertmanager webhook has its own section below.

## Client API

- `GET /api/v1/channels/{id}/messages?limit=&before_id=` *(implemented)* — a channel's
  feed, newest first, paginated backwards with `before_id`
- `GET /api/v1/messages?unread=&limit=&before_id=` *(implemented)* — the notification
  feed, across every channel. Alerts are **kept out of it server-side**: they have a
  console of their own, a lifecycle that "read / unread" does not describe, and their
  reminders would drown a view whose whole point is to empty itself
- `GET /api/v1/messages/unread` *(implemented)* — what the view would show, counted
  without sending it: a badge wants a number, not a list, and the list is capped where
  the count must not be
- `POST /api/v1/messages/read` *(implemented)* — empties the feed in one gesture, for
  this user alone
- `POST /api/v1/devices`

The endpoints marked *(implemented)* above are available; the others land as step 1
progresses.

## Alertmanager alerts *(implemented)*

### `POST /api/v1/ingest/alertmanager/{channel}`

Alertmanager v4 webhook, authenticated by a **publish token** like any producer.
Consumed directly rather than translated into an ntfy message: the payload carries the
`fingerprint`, the full label set, the status and the timestamps — exactly what an
entity with a lifecycle needs, and exactly what a translation destroys.

Per alert received:

| Case | Effect |
|---|---|
| `firing`, unknown fingerprint | Opening, message notified, first reminder scheduled |
| `firing`, alert already open | **Silent refresh**, no message |
| `resolved`, alert open | Closing, discreet resolution message |
| `resolved`, never seen open | Ignored — one does not invent an alert in order to close it |

The silent refresh is what makes it possible to keep `repeat_interval` at a
**moderate** value on the Alertmanager side instead of neutralising it: the repeat
becomes a free state resynchronisation after a restart, and not a second reminder
engine. Verified against a real Alertmanager: three deliveries of the same alert, a
single message.

The same fingerprint firing again after a resolution opens a **new** entity, it does
not resurrect the old one.

The severity drives the priority: `critical` → 5, `warning` → 4, `info` → 2, the rest
→ 3. A resolution goes out at priority 2 — good news arriving after having woken
somebody up must not shout.

### `GET /api/v1/alerts?open=&unacked=&closed=&severity=&limit=`

The caller's alerts, **across every channel**: this is the application's console.
`unacked=1` is the subset that demands an action; `closed=1` is the history, and it is
ordered by resolution date — a story is read from its end, what one looks for there is
what has just been resolved.

### `GET /api/v1/alerts/{id}`

The alert and its **timeline**, rebuilt from what is already recorded: opening,
notifications, reminders, repeats, acknowledgement, resolution. No second log written
in parallel — two recordings of the same events would end up diverging, and the one
consulted at three in the morning must not be the one that drifted.

`occurrences` counts the times Alertmanager re-delivered the same alert. Without it, a
rule that has beaten forty times looks like a stable rule, and that difference is
precisely what decides whether one goes back to sleep.

### `GET /api/v1/channels/{id}/alerts?open=1`

The alerts of a single channel, `open=1` restricting to those still open.

### `POST /api/v1/alerts/{id}/ack` and `DELETE /api/v1/alerts/{id}/ack`

**Purely local** acknowledgement. Nothing is written back to Alertmanager: the alert
stays visible in the dashboards, and the phone never writes into the chain it watches.
Acknowledging is not resolving — the alert stays `firing`, only its reminders stop.
The author and the time are kept.

The `DELETE` takes the acknowledgement back: the gesture is made half asleep, on the
wrong alert about as often as on the right one, and with no way back one would have to
wait for a reminder the acknowledgement has just removed. The alert resumes its
cadence without resetting its reminder counter — the numbering is the story of what
the alert has cost in interruptions.

## Reminders *(implemented)*

Cadence **per severity, overridable per channel**: the most specific wins. No policy
means no reminder — silence is the default, a channel only insists if it has been
asked to.

- `GET /api/v1/channels/{id}/reminders` — member; lists the channel's overrides and
  the defaults it inherits, each marked `scope: channel` or `default`
- `PUT /api/v1/channels/{id}/reminders` — `owner`; sets the channel's override

```json
{ "severity": "critical", "interval_seconds": 900, "enabled": true }
```

Reminders carry their rank **beside** the message (`reminder_count`) and not in the
title: prefixing "Reminder 3 — " truncated the title on the lock screen, exactly where
it has to be read at a glance. A reminder **replaces** the notification it repeats,
rather than stacking one more per round.

The scheduler **re-reads the database** every 15 s rather than holding timers in
memory. Second-level precision is of no interest for a reminder; surviving a restart
of the service is — an alert that went out at 3 a.m. must keep insisting even if the
server was updated at 4.

A failed send reschedules all the same: making an alert mute over a transient failure
is the one result an alerting system must never produce.

## Quiet hours *(implemented)*

A window belongs to the **channel**, optionally named by severity. The most specific
wins, as with the cadences.

- `GET /api/v1/channels/{id}/quiet-hours` — member
- `PUT /api/v1/channels/{id}/quiet-hours` — `owner`; two empty bounds remove the
  window, which is what an emptied form means

```json
{ "severity": "critical", "from": "23:00", "to": "07:00" }
```

The window **silences, it does not hold back**. At delivery the priority is lowered
*on the way out* and not in the record: holding a message until morning would make it
arrive out of order in a feed whose "unread" state already keeps it for the morning,
and holding an alert back is losing it. Read again the next day, the message still
carries the priority it was published with.

For reminders, the window **pushes** to its end: an unacknowledged alert resurfaces,
and making its reminder disappear would turn a night setting into a way of losing an
alert.

A window set on **the whole channel does not cover criticals**. Silencing them is
legitimate on a homelab — a disk filling up at three in the morning can wait until
seven — but it is asked for by naming `critical`, so that it is not inherited from a
window meant for downloads. The broad gesture stays safe, the dangerous one stays
deliberate.

The window may straddle midnight, which is the normal case. It is given whole or not
at all: a half would silence at an unpredictable hour, so that is a `400`.

## Reading and timeline *(implemented)*

An information channel reads like **a shared RSS feed**: the message is single and
common, but the "seen" state belongs to each user. A member who reads changes nothing
for the others.

- `GET /api/v1/channels` carries an `unread` **specific to the caller**
- `GET /api/v1/channels/{id}/messages` carries a `read` per message, computed in a
  single query for the whole batch
- `POST /api/v1/messages/{id}/read` — idempotent: two calls produce a single timeline
  entry
- `POST /api/v1/channels/{id}/read?upto_id=N` — marks up to and including N, or the
  whole channel if `upto_id` is absent; returns `{"marked": n}`
- `GET /api/v1/messages/{id}/timeline` — the delivery timeline

Marking as read emits a `messages.read` event **to the other devices of the same user
only**, so that reading on the phone clears the badge on the tablet without touching
the other members.

### The timeline

An **append-only** log: the story of a message grows, it is never rewritten.

| `kind` | When |
|---|---|
| `queued` | The message is recorded for this recipient |
| `sent` | It was written into this device's socket |
| `delivered` | This device acknowledged it |
| `read` | The user opened it |

`queued` is written **before** the broadcast, deliberately: a recipient with no live
socket must still show up, since "queued but never sent" is exactly the failure the
reliability measurement is looking for. And "sent without an acknowledgement" is the
other one — that of a socket the system froze without closing.

### Acknowledgement, on the socket

The client sends `{"kind":"ack","seq":N}`. Everything up to `N` becomes `delivered` on
that device. The cursor never rewinds: an older acknowledgement re-delivers nothing.
It is stored in `devices.acked_seq` (migration 0002).

## WebSocket *(implemented)*

`GET /api/v1/ws?since_seq=N`, authenticated by the device token in the `Authorization`
header — the clients are native applications, not browsers.

On connection the server **replays everything after `since_seq`**, then sends a `ready`
frame carrying the last `seq`, then switches to real time. This is the contract that
makes an aggressive Android power manager survivable: a socket killed by the system
costs a reconnection and a delay, never a lost event. The subscription is taken
**before** the replay, so that an event occurring during the catch-up is queued rather
than lost in the gap.

Frames:

| `kind` | `seq` | Role |
|---|---|---|
| `ready` | last known | The catch-up is over |
| `heartbeat` | — | **Application-level** proof of life, every 30 s |
| `channel.created`, `channel.updated`, `channel.deleted` | yes | Channel changes |
| `message.new` | yes | A message published on a channel one is a member of |
| `messages.read` | yes | One of my other devices marked messages as read |
| `alert.opened`, `alert.resolved`, `alert.acked` | yes | An alert's lifecycle |
| `member.changed`, `member.removed` | yes | Membership changes |

The heartbeat is an application frame and not a protocol ping, because the application
has to **see** it: a socket the system silently froze stays open as far as the OS is
concerned, and only a missing heartbeat reveals it. This is what the diagnostics
screen reads.

An event is **recorded before being broadcast**. A socket that misses the broadcast
will replay it; an event only sent live would be lost for good.

A subscriber whose buffer is full is **disconnected** rather than waited for: it
reconnects and replays. Blocking the sender would let a single stuck phone delay the
whole server.

An open socket marks its device connected; this is what will make a delivery to a
device with no socket accountable. The markers are reset at startup: no socket
survives a restart, and a stale marker would skew the figures.

## Miscellaneous

- `GET /healthz` — status and build identity
- `GET /metrics` — Prometheus format (step 1)
