// Package agent implements the lanmon agent CLI entry point.
package agent

import (
	"fmt"

	"lanmon/internal/beacon"
	"lanmon/pkg/config"
	"lanmon/pkg/logger"
)

// Run starts the beacon agent.
func Run(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	log := logger.Init(cfg.Node.LogLevel)

	interval, err := cfg.Node.ParseInterval()
	if err != nil {
		return fmt.Errorf("parsing interval: %w", err)
	}

	if cfg.Node.SharedSecret == "" || cfg.Node.SharedSecret == "CHANGE_ME" {
		return fmt.Errorf("shared_secret must be set in config (not 'CHANGE_ME')")
	}

	log.Info().
		Str("network_range", cfg.Node.NetworkRange).
		Int("port", cfg.Node.Port).
		Msg("Starting legacy LANBeacon agent (deprecated)")

	return beacon.StartBeacon(
		"", // Interface auto-detected in beacon if empty? No, need to check broadcast.go
		"239.255.0.1",
		"",
		cfg.Node.Port,
		interval,
		cfg.Node.SharedSecret,
		log,
	)

}
