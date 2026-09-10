# home-notifications

A replacement for ntfy in a homelab: a Go server (`hnotifd`) and a native Android
application. Alertmanager alerts have a real lifecycle in it — opening, acknowledgement,
reminders, closing — and the notifications of the homelab's applications read as a
shared feed with a "seen" state per user.

## Status

Step 0 of the plan: build chain, Debian package, CI. The server loads its configuration
and answers on `/healthz`; the channels, the ingest and the WebSocket land at step 1.

## Serveur

```sh
make            # binary for the current platform, in dist/
make binaries   # linux/amd64 + linux/arm64
make test       # tests
make test-race  # tests with the race detector — needs a C compiler
make vet
make version
```

`CGO_ENABLED=0` is enforced so that cross-compiling to arm64 needs no toolchain.
**A consequence not to work around**: the SQLite driver has to be the pure-Go
`modernc.org/sqlite`, never `mattn/go-sqlite3`.

To run it locally:

```sh
cp config.example.yaml config.yaml
# set listen to 0.0.0.0:8080 so that the phone reaches the server over the LAN
./dist/hnotifd-linux-amd64 -config config.yaml
```

## Application Android

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
## Paquet Debian

Built by CI (`jiro4989/build-deb-action`) and published in the release with the
binaries: `latest` as a prerelease on every push to `master`, a tagged release on
`v*.*.*`.

The package installs `/usr/bin/hnotifd`, the conffile
`/etc/home-notifications/config.yaml` and the systemd unit. The `postinst` creates the
system user and `/var/lib/home-notifications`, **enables** the service but does not
start it on a first installation: configuration management lays down the real
configuration and then starts the service. On an upgrade, the service is restarted if
it was running.

`/var/lib/home-notifications` is deliberately kept, even on a purge: it holds the alert
history and the device tokens.

Validating the package locally, without touching the target host:

```sh
make binaries
# assembly and installation inside a throwaway Debian container
```

## Deployment

The configuration-management module lives elsewhere, not here. The host is installed
only once, in its final configuration, at step 5 of the plan.
