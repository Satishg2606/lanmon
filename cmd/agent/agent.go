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

	log := logger.Init(cfg.Server.LogLevel)

	interval, err := cfg.Agent.ParseInterval()
	if err != nil {
		return fmt.Errorf("parsing interval: %w", err)
	}

	if cfg.Agent.SharedSecret == "" || cfg.Agent.SharedSecret == "CHANGE_ME" {
		return fmt.Errorf("shared_secret must be set in config (not 'CHANGE_ME')")
	}

	log.Info().
		Str("interface", cfg.Agent.Interface).
		Str("multicast_group", cfg.Agent.MulticastGroup).
		Int("port", cfg.Agent.Port).
		Msg("Starting LANBeacon agent")

	return beacon.StartBeacon(
		cfg.Agent.MulticastGroup,
		cfg.Agent.Port,
		interval,
		cfg.Agent.SharedSecret,
		log,
	)
}
