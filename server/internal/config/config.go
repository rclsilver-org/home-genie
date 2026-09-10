// Package config loads the server configuration. The default paths are
// injected at link time so that the Debian package points at /etc and
// /var/lib while a plain `go build` stays usable from a working copy.
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

var (
	defaultConfigFile   = "config.yaml"
	defaultDatabaseFile = "hnotifd.db"
)

// DefaultConfigFile is the configuration path used when none is given on the
// command line.
func DefaultConfigFile() string { return defaultConfigFile }

// Config is the whole server configuration.
type Config struct {
	// Listen is the address the HTTP server binds to. In production nginx
	// terminates TLS, so this stays on the loopback; in development it must
	// be 0.0.0.0 for the phone to reach it over the LAN.
	Listen string `yaml:"listen"`

	// BaseURL is the externally reachable URL, used to build the links sent
	// in notifications and the OIDC redirect URI.
	BaseURL string `yaml:"base_url"`

	// Database is the path to the SQLite file.
	Database string `yaml:"database"`

	// LogLevel is one of debug, info, warn, error.
	LogLevel string `yaml:"log_level"`

	// OIDC wires the identity provider. Leaving it empty disables OIDC, which
	// is a valid deployment: the local break-glass account still works.
	OIDC OIDCConfig `yaml:"oidc"`
}

// OIDCConfig points at the identity provider.
//
// No client secret: the application is a public client using Authorization
// Code + PKCE, and the server only ever verifies the identity token it is
// handed. One less secret to deploy and rotate.
type OIDCConfig struct {
	// Issuer is the provider's issuer URL, e.g. https://sso.example.net/realms/home.
	Issuer string `yaml:"issuer"`
	// ClientID is what the identity token must be addressed to.
	ClientID string `yaml:"client_id"`
}

// Default returns the configuration used when the file omits a field.
func Default() Config {
	return Config{
		Listen:   "127.0.0.1:8080",
		BaseURL:  "http://127.0.0.1:8080",
		Database: defaultDatabaseFile,
		LogLevel: "info",
	}
}

// Load reads the configuration from path, filling anything absent with the
// defaults. A missing file is an error: the package ships one, and running
// with implicit defaults in production would be a trap.
func Load(path string) (Config, error) {
	cfg := Default()

	raw, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("reading %s: %w", path, err)
	}

	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return cfg, fmt.Errorf("parsing %s: %w", path, err)
	}

	if cfg.Listen == "" {
		return cfg, fmt.Errorf("%s: listen must not be empty", path)
	}
	if cfg.Database == "" {
		return cfg, fmt.Errorf("%s: database must not be empty", path)
	}

	return cfg, nil
}
