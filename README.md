# Home Genie

A self-hosted notification server and its Android application, for a homelab.

Two things live in it, and they are not alike:

- **Alerts** have a lifecycle. Alertmanager opens them, somebody takes them, they
  remind as long as nobody deals with them, and they close when the condition goes
  away. They concern everyone at once.
- **Notifications** are a feed. The media tools, the image watcher, a script: they
  announce a fact already accomplished. Each person reads them for themselves, and one
  person's "read" state changes nothing for the others.

The project replaces an ntfy paired with an ntfy-alertmanager bridge, where an alert
was just one more message and acknowledgement did not exist.

## What it does

**On the alert side.** Native ingest of the Alertmanager v4 webhook, with dedup on the
`fingerprint`: an alert re-delivered forty times stays one alert, whose occurrences are
counted. Local acknowledgement — nothing is written back to Alertmanager, so the usual
dashboards keep showing it and the phone never writes into the chain it watches.
Reminders at a cadence chosen per severity, overridable per channel. A timeline per
alert: opening, notifications, reminders, repeats, acknowledgement, resolution.

**On the notification side.** **ntfy-compatible** publishing: the `Title`, `Priority`,
`Tags` and `Click` headers and the JSON body are accepted as they are, so a producer
already configured for ntfy changes only its URL and its token. A "read" state per
user, and a delivery timeline per message — who it was sent to, on which device, when
it was received and read.

**Silence.** Two distinct mechanisms, and the difference matters:

| | Effect | Scope | What is lost |
|---|---|---|---|
| **Quiet hours** | the notification arrives without noise | global, overridable per channel and per severity | nothing — a reminder comes back at the end of the window |
| **Mute** | no notification at all | yours alone, every channel, bounded in time | the notifications of that period |

A quiet window that does not name a severity **never** covers a critical alert:
silencing one is legitimate on a homelab, but it is asked for by naming `critical`,
not inherited from a setting meant for downloads.

A mute, on the other hand, goes above everything — criticals included — but it
silences only the person who sets it. It is a deliberate and personal gesture: "be
quiet, I am the one making the noise". The other members keep being notified, and do
not even see that somebody went quiet.

**Channels and rights.** A channel carries members (read, publish, administer),
publish tokens — one per producer, revocable without touching the others — and its own
reminder cadences.

## How it works

```
Alertmanager ─┐
media tools  ─┼─→ hgenied ──WebSocket──→ Android application
scripts      ─┘      │
                     └─ SQLite (state, history, hashed tokens)
```

The phone keeps an **open socket** in a foreground service. Every event carries a
monotonic sequence number; on reconnection the client asks for what follows the last
number it received, so an outage only costs time, never a message. An application
heartbeat crosses the socket in both directions: the server knows a device is still
listening, which an open TCP socket does not prove.

No FCM: notifications pass through no third party, and the server is not reachable
from the outside to emit them.

## Installing

### Server

A Debian package for `amd64` and `arm64`, published in the GitHub releases. It
installs `hgenied`, its systemd unit and its system user.

```sh
dpkg -i home-genie_<version>_arm64.deb
$EDITOR /etc/home-genie/config.yaml
systemctl enable --now home-genie
```

The configuration fits in a few lines — see [`config.example.yaml`](config.example.yaml).
The service listens in cleartext: put it behind a reverse proxy that terminates TLS
and knows how to relay a WebSocket.

Create the first account, the **break-glass** one, the account that works when the
identity provider no longer answers:

```sh
hgenied admin create -config /etc/home-genie/config.yaml -username <name>
```

### Application

The APK is published with each release. It installs by sideload: on first launch, give
the server URL, then sign in — with the break-glass account, or through OIDC if one is
configured.

The home screen only shows a warning when a system setting is genuinely missing:
battery optimisation exemption, notification permission, Do Not Disturb access. When
everything is in order, it says nothing.

## Authentication

Two paths, for two situations:

- **OIDC** (Authorization Code + PKCE, public client) — the normal path. The
  application opens a Custom Tab, gets an ID token back, and the server verifies it
  against the provider's JWKS. No client secret is embedded in the APK, because a
  native application cannot keep one.
- **Local account** — the break-glass door, protected by argon2id. It exists for the
  day the identity provider is down, which is exactly the day the alerts matter.

In both cases the application receives a **device token** of its own. The server stores
only its SHA-256 fingerprint; the cleartext token appears once.

## Decisions, and why

**A WebSocket, not FCM.** The risk of this project is not the network, it is Android's
battery arbitration. A night of continuous observation settled it: the socket held for
7 h 56 without interruption under a vendor Android skin. FCM stays a way out if the
figures degrade, not a prerequisite.

**Alerts and notifications are two objects.** A notification is read or unread, per
person, and nothing else ever happens to it. An alert opens, is taken, reminds and
closes, for everyone at once. Mixing them forced each to borrow the other's
vocabulary.

**Two channels are enough.** Splitting `alerts-critical` / `alerts-warning` re-encodes
in the channel what the alert already carries — its `severity` label — and everything
that could use it already relies on the severity. What a channel really separates is
**who can read**, **what a token may publish**, and **what can be silenced**.

**Silence rather than hold back.** Holding a message until morning would make it
arrive out of order in a feed whose "unread" state already keeps it for the morning;
holding an alert back is losing it. During quiet hours everything arrives — the phone
keeps quiet.

**Acknowledgement is local.** Setting a silence in Alertmanager from a phone would mix
the notification chain with the alerting chain. The alert stays `firing`: only its
reminders stop, and the author of the acknowledgement is visible.

**A reminder replaces its notification.** Fifteen reminders do not make fifteen lines
in the shade. The reminder's rank travels beside the message and is shown in the
header, not in the title, which would be truncated on the lock screen.

**SQLite, without CGO.** `modernc.org/sqlite` in pure Go: cross-compiling to an
ARM server then needs no toolchain, and the server needs no service running beside
it.

## Development

Everything goes through ephemeral nix shells; nothing is installed on the machine.

```sh
nix-shell shell.nix --run 'make test'              # server: tests and coverage
nix-shell shell.nix --run 'make all'               # binary for the current architecture
nix-shell nix/android-sdk.nix --run 'make apk'     # debug APK
nix-shell nix/android-emulator.nix --run 'make emulator'   # emulator for the tests
```

The development shell does not carry the emulator: the system image weighs close to
two gigabytes and the daily build does not need it. An emulator does not replace a
real phone — it is an AOSP with no vendor skin, so it says nothing about a
manufacturer's battery arbitration — but it does well what the phone does badly:
replay a complete journey, hands-free, reproducibly.

The server contract is described in [`docs/protocol.md`](docs/protocol.md), precisely
enough for an iOS client to be written without reading the Go code.

## Licence

A personal project, published as is.
