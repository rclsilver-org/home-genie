// Package version exposes the build identity of the binary. Both values are
// injected at link time by the Makefile; the fallbacks below are what a plain
// `go build` produces.
package version

import "fmt"

var (
	version = "dev"
	commit  = "unknown"
)

// Version returns the release version, as computed by generate-version.sh.
func Version() string { return version }

// Commit returns the git commit the binary was built from.
func Commit() string { return commit }

// String returns a single human-readable build identifier.
func String() string { return fmt.Sprintf("%s (%s)", version, commit) }
