// Package config provides TOML configuration loading for lanmon.
package config

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	toml "github.com/pelletier/go-toml/v2"
)

// Config is the top-level configuration structure.
type Config struct {
	Node    NodeConfig    `toml:"node"`
	Connect ConnectConfig `toml:"connect"`
}

// NodeConfig holds settings for the P2P discovery node.
type NodeConfig struct {
	NetworkRange   string `toml:"network_range"`
	Port           int    `toml:"port"`
	Interval       string `toml:"interval"`
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

// ParseInterval parses the node beacon interval string to a time.Duration.
func (n *NodeConfig) ParseInterval() (time.Duration, error) {
	if n.Interval == "" {
		return 30 * time.Second, nil
	}
	return time.ParseDuration(n.Interval)
}

// ParseStaleThreshold parses the node stale threshold string to a time.Duration.
func (n *NodeConfig) ParseStaleThreshold() (time.Duration, error) {
	if n.StaleThreshold == "" {
		return 90 * time.Second, nil
	}
	return time.ParseDuration(n.StaleThreshold)
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
	cfg.expandPaths()
	return cfg, nil
}

func (cfg *Config) expandPaths() {
	cfg.Connect.ServerPubKey = ExpandPath(cfg.Connect.ServerPubKey)
	cfg.Connect.KnownHosts = ExpandPath(cfg.Connect.KnownHosts)
	cfg.Node.DBPath = ExpandPath(cfg.Node.DBPath)
}

// ExpandPath expands tilde (~) to the user's home directory.
func ExpandPath(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	usr, err := user.Current()
	if err != nil {
		return path
	}
	if path == "~" {
		return usr.HomeDir
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(usr.HomeDir, path[2:])
	}
	return path
}

func applyDefaults(cfg *Config) {

	// Node defaults
	if cfg.Node.Port == 0 {
		cfg.Node.Port = 5678
	}
	if cfg.Node.Interval == "" {
		cfg.Node.Interval = "30s"
	}
	if cfg.Node.DBPath == "" {
		cfg.Node.DBPath = "/var/lib/lanmon/hosts.db"
	}
	if cfg.Node.RPCSocket == "" {
		cfg.Node.RPCSocket = "/run/lanmon/server.sock"
	}
	if cfg.Node.StaleThreshold == "" {
		cfg.Node.StaleThreshold = "90s"
	}
	if cfg.Node.LogLevel == "" {
		cfg.Node.LogLevel = "info"
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

