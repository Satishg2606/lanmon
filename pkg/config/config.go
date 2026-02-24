// Package config provides TOML configuration loading for lanmon.
package config

import (
	"fmt"
	"os"
	"time"

	toml "github.com/pelletier/go-toml/v2"
)

// Config is the top-level configuration structure.
type Config struct {
	Agent   AgentConfig   `toml:"agent"`
	Server  ServerConfig  `toml:"server"`
	Connect ConnectConfig `toml:"connect"`
}

// AgentConfig holds settings for the beacon agent.
type AgentConfig struct {
	Interface      string `toml:"interface"`
	MulticastGroup string `toml:"multicast_group"`
	Port           int    `toml:"port"`
	Interval       string `toml:"interval"`
	SharedSecret   string `toml:"shared_secret"`
}

// ServerConfig holds settings for the listener/server.
type ServerConfig struct {
	Interface      string `toml:"interface"`
	MulticastGroup string `toml:"multicast_group"`
	Port           int    `toml:"port"`
	SharedSecret   string `toml:"shared_secret"`
	DBPath         string `toml:"db_path"`
	RPCSocket      string `toml:"rpc_socket"`
	StaleThreshold string `toml:"stale_threshold"`
	LogLevel       string `toml:"log_level"`
}

// ConnectConfig holds settings for the SSH key distributor.
type ConnectConfig struct {
	RPCSocket    string `toml:"rpc_socket"`
	ServerPubKey string `toml:"server_pubkey"`
	KnownHosts   string `toml:"known_hosts"`
}

// ParseInterval parses the agent beacon interval string to a time.Duration.
func (a *AgentConfig) ParseInterval() (time.Duration, error) {
	if a.Interval == "" {
		return 30 * time.Second, nil
	}
	return time.ParseDuration(a.Interval)
}

// ParseStaleThreshold parses the server stale threshold string to a time.Duration.
func (s *ServerConfig) ParseStaleThreshold() (time.Duration, error) {
	if s.StaleThreshold == "" {
		return 90 * time.Second, nil
	}
	return time.ParseDuration(s.StaleThreshold)
}

// Load reads and parses a TOML config file, applying defaults for unset values.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}

	cfg := &Config{}
	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}

	applyDefaults(cfg)
	return cfg, nil
}

func applyDefaults(cfg *Config) {
	// Agent defaults
	if cfg.Agent.MulticastGroup == "" {
		cfg.Agent.MulticastGroup = "239.255.0.1"
	}
	if cfg.Agent.Port == 0 {
		cfg.Agent.Port = 5678
	}
	if cfg.Agent.Interval == "" {
		cfg.Agent.Interval = "30s"
	}

	// Server defaults
	if cfg.Server.MulticastGroup == "" {
		cfg.Server.MulticastGroup = "239.255.0.1"
	}
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 5678
	}
	if cfg.Server.DBPath == "" {
		cfg.Server.DBPath = "/var/lib/lanmon/hosts.db"
	}
	if cfg.Server.RPCSocket == "" {
		cfg.Server.RPCSocket = "/run/lanmon/server.sock"
	}
	if cfg.Server.StaleThreshold == "" {
		cfg.Server.StaleThreshold = "90s"
	}
	if cfg.Server.LogLevel == "" {
		cfg.Server.LogLevel = "info"
	}

	// Connect defaults
	if cfg.Connect.RPCSocket == "" {
		cfg.Connect.RPCSocket = "/run/lanmon/server.sock"
	}
	if cfg.Connect.ServerPubKey == "" {
		cfg.Connect.ServerPubKey = os.ExpandEnv("$HOME/.ssh/id_rsa.pub")
	}
	if cfg.Connect.KnownHosts == "" {
		cfg.Connect.KnownHosts = "/etc/lanmon/known_hosts"
	}
}
