BINARY = hgenied
PKG    = github.com/rclsilver-org/home-genie

SOURCE_FILES = $(shell find server -type f -name '*.go' -not -name '*_test.go')

VERSION_PKG = $(PKG)/server/internal/version
CONFIG_PKG  = $(PKG)/server/internal/config

# Overridden by the CI so the packaged binary points at /etc and /var/lib.
DEFAULT_CONF_FILE ?= config.yaml
DEFAULT_DB_FILE   ?= hgenied.db

VERSION    ?= $(shell ./generate-version.sh)
LAST_COMMIT = $(shell git rev-parse HEAD)

DIST_DIR = ./dist
ARCHS    = amd64 arm64

TEST_LOCATION ?= ./...
TEST_CMD       = go test -cover

LD_FLAGS = -ldflags "-w -s \
	-X $(VERSION_PKG).version=$(VERSION) \
	-X $(VERSION_PKG).commit=$(LAST_COMMIT) \
	-X $(CONFIG_PKG).defaultConfigFile=$(DEFAULT_CONF_FILE) \
	-X $(CONFIG_PKG).defaultDatabaseFile=$(DEFAULT_DB_FILE)"

.PHONY: all
all: $(DIST_DIR)/$(BINARY)-$(shell go env GOOS)-$(shell go env GOARCH)

# Every architecture shipped as a package. CGO stays disabled so that
# cross-compiling to arm64 needs no toolchain: that is why the SQLite driver
# must be the pure-Go modernc.org/sqlite.
.PHONY: binaries
binaries: $(addprefix $(DIST_DIR)/$(BINARY)-linux-,$(ARCHS))

$(DIST_DIR)/$(BINARY)-linux-%: $(SOURCE_FILES) go.mod
	@mkdir -p $(DIST_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=$* go build $(LD_FLAGS) -o $@ ./server/cmd/$(BINARY)

.PHONY: test
test:
	$(TEST_CMD) $(COVER_OPTS) $(TEST_LOCATION)

# The race detector needs cgo, hence a C compiler. Kept out of the default
# target so `make test` works outside the dev shell; inside `nix-shell` and in
# the CI, gcc is available and this one runs.
.PHONY: test-race
test-race:
	CGO_ENABLED=1 $(TEST_CMD) -race $(COVER_OPTS) $(TEST_LOCATION)

.PHONY: vet
vet:
	go vet ./...

.PHONY: version
version:
	@echo $(VERSION)

.PHONY: clean
clean:
	rm -rf $(DIST_DIR) .debpkg-*

# --- Application Android ---------------------------------------------------
#
# Requires an SDK: `nix-shell nix/android-sdk.nix` (see the README).

ANDROID_DIR = ./android

# Must stay in step with buildToolsVersion in android/app/build.gradle.kts
# and nix/android-sdk.nix.
BUILD_TOOLS = 36.0.0

# On NixOS the aapt2 downloaded from Maven is a generic, unpatchelfed binary
# that will not start, and the SDK in the store is read-only, so the SDK's own
# aapt2 is forced. Elsewhere — CI on Ubuntu — the flag is not added: the
# condition only matches an ANDROID_HOME inside the nix store.
GRADLE_AAPT2_OVERRIDE = $(if $(filter /nix/store/%,$(ANDROID_HOME)),-Pandroid.aapt2FromMavenOverride=$(ANDROID_HOME)/build-tools/$(BUILD_TOOLS)/aapt2)

GRADLE_FLAGS = -PappVersionName=$(VERSION) $(GRADLE_AAPT2_OVERRIDE)

.PHONY: apk
apk:
	cd $(ANDROID_DIR) && gradle assembleDebug $(GRADLE_FLAGS)

# The artifact a release ships: minified by R8 and signed with the stable key.
# Without ANDROID_KEYSTORE_PATH in the environment it still builds, unsigned —
# which is what makes it runnable locally without holding the release key.
.PHONY: apk-release
apk-release:
	cd $(ANDROID_DIR) && gradle assembleRelease $(GRADLE_FLAGS)

.PHONY: android-test
android-test:
	cd $(ANDROID_DIR) && gradle test $(GRADLE_FLAGS)

.PHONY: android-clean
android-clean:
	cd $(ANDROID_DIR) && gradle clean $(GRADLE_FLAGS)

# --- Emulator, for test campaigns ------------------------------------------
#
# The development SDK carries no emulator (the system image weighs ~2 GiB):
# these targets are used inside nix-shell nix/android-emulator.nix.

AVD_NAME ?= home-genie-test

.PHONY: avd
avd:
	@if ! avdmanager list avd -c | grep -qx '$(AVD_NAME)'; then \
		echo no | avdmanager create avd -n '$(AVD_NAME)' \
			-k 'system-images;android-36;google_apis;x86_64' --force; \
	fi

.PHONY: emulator
emulator: avd
	@emulator -avd '$(AVD_NAME)' -no-window -no-audio -no-boot-anim -gpu swiftshader_indirect &
	@adb wait-for-device
	@until [ "$$(adb shell getprop sys.boot_completed 2>/dev/null | tr -d '\r')" = "1" ]; do sleep 2; done
	@echo "emulator ready"
