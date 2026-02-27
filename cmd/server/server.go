// Package server implements the lanmon server CLI entry point.
package server

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"lanmon/internal/listener"
	"lanmon/internal/rpc"
	"lanmon/internal/store"
	"lanmon/pkg/config"
	"lanmon/pkg/logger"
)

// Run starts the discovery server (listener + RPC + expiry).
func Run(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	log := logger.Init(cfg.Node.LogLevel)

	if cfg.Node.SharedSecret == "" || cfg.Node.SharedSecret == "CHANGE_ME" {
		return fmt.Errorf("shared_secret must be set in config (not 'CHANGE_ME')")
	}

	// Ensure database directory exists
	dbDir := filepath.Dir(cfg.Node.DBPath)
	if err := os.MkdirAll(dbDir, 0700); err != nil {
		return fmt.Errorf("creating database directory %s: %w", dbDir, err)
	}

	// Ensure RPC socket directory exists
	sockDir := filepath.Dir(cfg.Node.RPCSocket)
	if err := os.MkdirAll(sockDir, 0700); err != nil {
		return fmt.Errorf("creating socket directory %s: %w", sockDir, err)
	}

	// Open store
	db, err := store.New(cfg.Node.DBPath, log)
	if err != nil {
		return fmt.Errorf("opening store: %w", err)
	}
	defer db.Close()

	// Start stale host expiry
	staleThreshold, err := cfg.Node.ParseStaleThreshold()
	if err != nil {
		return fmt.Errorf("parsing stale threshold: %w", err)
	}
	db.RunExpiry(5*time.Minute, staleThreshold)

	// Start RPC server
	if err := rpc.StartServer(cfg.Node.RPCSocket, db, log); err != nil {
		return fmt.Errorf("starting RPC server: %w", err)
	}

	log.Info().
		Str("db_path", cfg.Node.DBPath).
		Str("rpc_socket", cfg.Node.RPCSocket).
		Dur("stale_threshold", staleThreshold).
		Msg("Starting legacy LANListener server (deprecated)")

	// Start listener in a goroutine so we can handle signals
	errCh := make(chan error, 1)
	go func() {
		errCh <- listener.StartListener(
			"",
			"239.255.0.1",
			cfg.Node.Port,
			cfg.Node.SharedSecret,
			db,
			log,
		)
	}()


	// Wait for shutdown signal or listener error
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		return fmt.Errorf("listener error: %w", err)
	case sig := <-sigCh:
		log.Info().Str("signal", sig.String()).Msg("Shutting down")
		// Cleanup: remove RPC socket
		os.Remove(cfg.Node.RPCSocket)
		return nil
	}
}
