# Development shell for the server.
#
#   nix-shell
#   make test && make binaries
#
# Go 1.25 rather than the nixpkgs default (1.24): modernc.org/sqlite, the
# pure-Go SQLite driver the whole CGO-free cross-compilation depends on,
# requires Go >= 1.25. Going through Go's automatic toolchain mechanism would
# download a version from outside nixpkgs on every clean build; better to
# provide it explicitly.
#
# gcc is here only for `make test-race`: the race detector goes through cgo.
# It never builds a shipped binary — those stay at CGO_ENABLED=0.
#
# For the Android application, see nix/android-sdk.nix.

{ pkgs ? import <nixpkgs> { } }:

pkgs.mkShell {
  buildInputs = [
    pkgs.go_1_25
    pkgs.gcc
    pkgs.sqlite # the CLI, to inspect the database during an incident
  ];

  # No toolchain download: the shell's version has to be enough.
  GOTOOLCHAIN = "local";

  # The banner goes to stderr: on stdout it pollutes the output of any
  # `nix-shell --run` whose result is read through a pipe — which has already
  # turned a test into a false positive.
  shellHook = ''
    echo "Go: $(go version)" >&2
  '';
}
