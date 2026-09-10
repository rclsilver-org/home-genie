// Command hnotifd is the home-notifications server.
//
// Without a subcommand it runs the server. The `admin` subcommand creates
// the local break-glass account, which has to be possible without a running
// server and without the identity provider.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rclsilver-org/home-notifications/server/internal/api"
	"github.com/rclsilver-org/home-notifications/server/internal/cli"
	"github.com/rclsilver-org/home-notifications/server/internal/config"
	"github.com/rclsilver-org/home-notifications/server/internal/db"
	"github.com/rclsilver-org/home-notifications/server/internal/store"
	"github.com/rclsilver-org/home-notifications/server/internal/version"
)

func main() {
	if err := dispatch(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "hnotifd: %v\n", err)
		os.Exit(1)
	}
}

func dispatch(args []string) error {
	if len(args) > 0 && args[0] == "admin" {
		return runAdmin(args[1:])
	}
	return runServer(args)
}

// runAdmin implements `hnotifd admin create`.
func runAdmin(args []string) error {
	if len(args) == 0 || args[0] != "create" {
		return errors.New("usage: hnotifd admin create -username <name> [-display-name <name>] [-config <file>]")
	}

	flags := flag.NewFlagSet("admin create", flag.ContinueOnError)
	var (
		configPath  = flags.String("config", config.DefaultConfigFile(), "path to the configuration file")
		username    = flags.String("username", "", "login name of the account to create")
		displayName = flags.String("display-name", "", "name shown in the application")
	)
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}

	handle, err := db.Open(cfg.Database)
	if err != nil {
		return err
	}
	defer handle.Close()

	return cli.CreateAdmin(store.New(handle), *username, *displayName, os.Stdout)
}

func runServer(args []string) error {
	flags := flag.NewFlagSet("hnotifd", flag.ContinueOnError)
	var (
		configPath  = flags.String("config", config.DefaultConfigFile(), "path to the configuration file")
		showVersion = flags.Bool("version", false, "print the version and exit")
	)
	if err := flags.Parse(args); err != nil {
		return err
	}

	if *showVersion {
		fmt.Println(version.String())
		return nil
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}

	logger := newLogger(cfg.LogLevel)
	logger.Info("starting", "version", version.Version(), "commit", version.Commit(),
		"listen", cfg.Listen, "database", db.Path(cfg.Database))

	// Opening also migrates: the schema is brought up to date on every start,
	// which is idempotent and keeps deployment down to installing the package.
	handle, err := db.Open(cfg.Database)
	if err != nil {
		return err
	}
	defer handle.Close()

	schemaVersion, dirty, err := db.Version(handle, cfg.Database)
	if err != nil {
		return err
	}
	if dirty {
		return fmt.Errorf("the schema is dirty at version %d: a migration failed halfway and needs a look", schemaVersion)
	}
	logger.Info("schema ready", "version", schemaVersion)

	repository := store.New(handle)

	if count, err := repository.CountUsers(); err == nil && count == 0 {
		logger.Warn("no account exists yet; create the break-glass one with " +
			"`hnotifd admin create -username <name>`")
	}

	mux := api.New(repository, logger).Routes()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		status := "ok"
		if err := handle.PingContext(r.Context()); err != nil {
			status = "degraded"
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":         status,
			"version":        version.Version(),
			"commit":         version.Commit(),
			"schema_version": schemaVersion,
		})
	})

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// SIGTERM is what systemd sends on stop and restart.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		logger.Info("shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl}))
}
