package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_ValidConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")

	content := `
[agent]
  interface = "eth0"
  multicast_group = "239.255.0.1"
  port = 5678
  interval = "30s"
  shared_secret = "my-secret"

[server]
  interface = "eth0"
  multicast_group = "239.255.0.1"
  port = 5678
  shared_secret = "my-secret"
  db_path = "/tmp/test.db"
  rpc_socket = "/tmp/test.sock"
  stale_threshold = "90s"
  log_level = "debug"

[connect]
  rpc_socket = "/tmp/test.sock"
  server_pubkey = "/tmp/id_rsa.pub"
  known_hosts = "/tmp/known_hosts"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if cfg.Agent.Interface != "eth0" {
		t.Errorf("Agent.Interface: got %s, want eth0", cfg.Agent.Interface)
	}
	if cfg.Agent.SharedSecret != "my-secret" {
		t.Errorf("Agent.SharedSecret: got %s, want my-secret", cfg.Agent.SharedSecret)
	}
	if cfg.Server.DBPath != "/tmp/test.db" {
		t.Errorf("Server.DBPath: got %s, want /tmp/test.db", cfg.Server.DBPath)
	}
	if cfg.Server.LogLevel != "debug" {
		t.Errorf("Server.LogLevel: got %s, want debug", cfg.Server.LogLevel)
	}
	if cfg.Connect.ServerPubKey != "/tmp/id_rsa.pub" {
		t.Errorf("Connect.ServerPubKey: got %s, want /tmp/id_rsa.pub", cfg.Connect.ServerPubKey)
	}
}

func TestLoad_Defaults(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")

	// Minimal config — all defaults should apply
	content := `
[agent]
  shared_secret = "test"
[server]
  shared_secret = "test"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if cfg.Agent.MulticastGroup != "239.255.0.1" {
		t.Errorf("default MulticastGroup: got %s, want 239.255.0.1", cfg.Agent.MulticastGroup)
	}
	if cfg.Agent.Port != 5678 {
		t.Errorf("default Port: got %d, want 5678", cfg.Agent.Port)
	}
	if cfg.Agent.Interval != "30s" {
		t.Errorf("default Interval: got %s, want 30s", cfg.Agent.Interval)
	}
	if cfg.Server.StaleThreshold != "90s" {
		t.Errorf("default StaleThreshold: got %s, want 90s", cfg.Server.StaleThreshold)
	}
	if cfg.Server.LogLevel != "info" {
		t.Errorf("default LogLevel: got %s, want info", cfg.Server.LogLevel)
	}
}

func TestLoad_NonexistentFile(t *testing.T) {
	_, err := Load("/nonexistent/config.toml")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestLoad_InvalidTOML(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")

	if err := os.WriteFile(cfgPath, []byte("invalid [[[ toml"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(cfgPath)
	if err == nil {
		t.Error("expected error for invalid TOML")
	}
}

func TestParseInterval(t *testing.T) {
	cfg := &AgentConfig{Interval: "10s"}
	d, err := cfg.ParseInterval()
	if err != nil {
		t.Fatalf("parse interval: %v", err)
	}
	if d.Seconds() != 10 {
		t.Errorf("Interval: got %v, want 10s", d)
	}
}

func TestParseInterval_Default(t *testing.T) {
	cfg := &AgentConfig{}
	d, err := cfg.ParseInterval()
	if err != nil {
		t.Fatalf("parse interval: %v", err)
	}
	if d.Seconds() != 30 {
		t.Errorf("Default interval: got %v, want 30s", d)
	}
}

func TestParseStaleThreshold(t *testing.T) {
	cfg := &ServerConfig{StaleThreshold: "120s"}
	d, err := cfg.ParseStaleThreshold()
	if err != nil {
		t.Fatalf("parse threshold: %v", err)
	}
	if d.Seconds() != 120 {
		t.Errorf("Threshold: got %v, want 120s", d)
	}
}
