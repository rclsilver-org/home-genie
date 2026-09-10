package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFillsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("listen: \"0.0.0.0:9000\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Listen != "0.0.0.0:9000" {
		t.Errorf("Listen = %q, want 0.0.0.0:9000", cfg.Listen)
	}
	if cfg.LogLevel != Default().LogLevel {
		t.Errorf("LogLevel = %q, want the default %q", cfg.LogLevel, Default().LogLevel)
	}
	if cfg.Database != Default().Database {
		t.Errorf("Database = %q, want the default %q", cfg.Database, Default().Database)
	}
}

func TestLoadRejectsMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "absent.yaml")); err == nil {
		t.Fatal("Load on a missing file should fail")
	}
}

func TestLoadRejectsEmptyListen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("listen: \"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("an empty listen address should be rejected")
	}
}
