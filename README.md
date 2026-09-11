# home-genie

A replacement for ntfy in a homelab: a Go server (`hgenied`) and a native Android
application. Alertmanager alerts have a real lifecycle in it — opening, acknowledgement,
reminders, closing — and the notifications of the homelab's applications read as a
shared feed with a "seen" state per user.

## Status

Step 0 of the plan: build chain, Debian package, CI. The server loads its configuration
and answers on `/healthz`; the channels, the ingest and the WebSocket land at step 1.

## Serveur

Development happens in a nix shell, which provides **Go 1.25** — and not the nixpkgs
default 1.24, because `modernc.org/sqlite` requires it. Going through Go's automatic
toolchain mechanism would download a version from outside nixpkgs on every clean build.

```sh
nix-shell
make            # binary for the current platform, in dist/
make binaries   # linux/amd64 + linux/arm64
make test
make test-race  # with the race detector (needs cgo, hence gcc)
make vet
make version
```

`CGO_ENABLED=0` is enforced so that cross-compiling to arm64 needs no toolchain.
**A consequence not to work around**: the SQLite driver has to be the pure-Go
`modernc.org/sqlite`, never `mattn/go-sqlite3`. Same rule for the migrations, which use
golang-migrate's `sqlite` driver and not its `sqlite3`.

`golang-migrate` is held at v4.19.1: v4.20.1 requires Go ≥ 1.25.11, which nixpkgs 25.05
does not provide. The reason is written down in `go.mod` so that nobody undoes it by
mistake.

The shell's `gcc` only serves the race detector; the shipped binaries stay CGO-free.

To run it locally:

```sh
cp config.example.yaml config.yaml
# set listen to 0.0.0.0:8080 so that the phone reaches the server over the LAN
./dist/hgenied-linux-amd64 -config config.yaml
```

The SQLite database is created and migrated at startup; `/healthz` returns the schema
version and switches to 503 if the database becomes unreachable.
## Android application

The SDK is provisioned by `androidenv`, in an **ephemeral shell**: nothing is installed
on the system, the SDK is realised in `/nix/store` and the variables exist only inside
the shell.

```sh
nix-shell nix/android-sdk.nix
make apk          # debug APK, in android/app/build/outputs/apk/debug/
make android-test
```

Two NixOS specifics are already handled; there is nothing to remember:

- **`buildToolsVersion` is pinned** in `android/app/build.gradle.kts`. Without it AGP
  resolves its own default version and tries to install it into the SDK, which fails
  since the store is read-only.
- **The SDK's own `aapt2` is forced** by the Makefile's `apk` target. The one AGP
  downloads from Maven is a generic, unpatchelfed binary whose dynamic loader does not
  exist on NixOS. The flag is only added when `ANDROID_HOME` points inside the store,
  so CI on Ubuntu is unaffected.

AGP marks `android.aapt2FromMavenOverride` as **experimental**. Debt worth knowing: if
it is removed, the Android build on NixOS will break and will have to go through
`nix-ld` or a hand-rolled patchelf.

The three build-tools versions — Makefile, `build.gradle.kts`, `nix/android-sdk.nix` —
have to stay in step.

The SDK is built with no garbage-collector root: a `nix-collect-garbage` will take it
back and it will have to be downloaded again.

### Identifiants

```
applicationId   io.github.rclsilver.home_genie
OAuth scheme    io.github.rclsilver.home-genie://oauth2redirect
```

The scheme carries a **dash** where the `applicationId` carries an underscore, and that
is not a typo: an underscore is legal in a Java package, but RFC 3986 forbids it in a
URI scheme and a browser may refuse to redirect to it. The three places that carry this
scheme — the manifest, `OidcClient.kt` and the provider client's `valid_redirect_uris`
— have to stay in step, or the provider refuses the redirect.

Changing the `applicationId` makes the application a **different application** as far as
Android is concerned: the old one stays installed with its own session, and has to be
uninstalled so that two services do not each hold a socket.

### On the phone

The test is run **on a real phone, not on an emulator**: an AOSP emulator reproduces
neither a vendor skin's battery management nor its "sleeping apps" list, which is
precisely what decides the reliability of the foreground service. That is why
`nix/android-sdk.nix` does not include the emulator.

```sh
adb pair <phone-ip>:<port>   # port given by the wireless debugging screen
adb connect <phone-ip>:5555
adb install -r android/app/build/outputs/apk/debug/app-debug.apk
adb logcat -s HomeNotifications
```

`adb` is **only** for debugging. The data path goes through the LAN address of the
development server, never through `adb reverse`: adb-over-WiFi drops with the phone's
deep sleep, and a broken tunnel would be indistinguishable from a server outage during
the overnight survival test.
## Checks before the host

Three things cannot be proved by a unit test, and are therefore checked locally without
putting anything on the target host.

**The WebSocket proxy.** An nginx in a container carrying *exactly* the configuration
the reverse proxy serves — `websocket` is `true` by default, hence the
`Upgrade`/`Connection` headers, `proxy_buffering off` and `proxy_read_timeout 3600s`.
A socket opened through that proxy over TLS does receive the live events, not only the
replay. The `hgenied` vhost is therefore a copy of the ntfy one.

Beware of a coupling nothing signals in the nginx configuration: the heartbeat interval
must stay well below `proxy_read_timeout`, since every beat rearms that counter.
Spacing the heartbeats out beyond it to save battery would have nginx cut the socket.

**The arm64 binary.** Run under `qemu-aarch64` — provided by nixpkgs, so with no
privilege and no change to `binfmt_misc`:

```sh
nix-shell -p qemu --run 'qemu-aarch64 ./dist/hgenied-linux-arm64 -version'
```

It starts, migrates its database and answers on `/healthz`. That is what proves the
pure-Go SQLite bet holds on the target architecture.

**The arm64 package.** Built and installed inside an amd64 Debian container with
`--force-architecture`: the `postinst` creates the user and the directory tree, the
permissions are right, and a purge keeps the state.

The Debian container must be started with `--platform linux/amd64` if an arm64 image of
the same tag is lying around in the Docker cache.
## Debian package

Built by CI (`jiro4989/build-deb-action`) and published in the release with the
binaries: `latest` as a prerelease on every push to `master`, a tagged release on
`v*.*.*`.

The package installs `/usr/bin/hgenied`, the conffile `/etc/home-genie/config.yaml`
and the systemd unit. The `postinst` creates the system user and
`/var/lib/home-genie`, **enables** the service but does not start it on a first
installation: configuration management lays down the real configuration and then starts
the service. On an upgrade, the service is restarted if it was running.

`/var/lib/home-genie` is deliberately kept, even on a purge: it holds the alert history
and the device tokens.

Validating the package locally, without touching the target host:

```sh
make binaries
# assembly and installation inside a throwaway Debian container
```

## Deployment

The configuration-management module lives elsewhere, not here. The host is installed
only once, in its final configuration, at step 5 of the plan.
