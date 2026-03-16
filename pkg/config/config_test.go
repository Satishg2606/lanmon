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
[node]
  network_ranges = ["10.51.240.0/23"]
  port = 5678
  interval = "30s"
  shared_secret = "my-secret"
  rqlite_url = "http://localhost:4001"
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

	if len(cfg.Node.NetworkRanges) != 1 || cfg.Node.NetworkRanges[0] != "10.51.240.0/23" {
		t.Errorf("Node.NetworkRanges: got %v, want [10.51.240.0/23]", cfg.Node.NetworkRanges)
	}
	if cfg.Node.SharedSecret != "my-secret" {
		t.Errorf("Node.SharedSecret: got %s, want my-secret", cfg.Node.SharedSecret)
	}
	if cfg.Node.RqliteURL != "http://localhost:4001" {
		t.Errorf("Node.RqliteURL: got %s, want http://localhost:4001", cfg.Node.RqliteURL)
	}
	if cfg.Node.LogLevel != "debug" {
		t.Errorf("Node.LogLevel: got %s, want debug", cfg.Node.LogLevel)
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
[node]
  shared_secret = "test"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if cfg.Node.Port != 5678 {
		t.Errorf("default Port: got %d, want 5678", cfg.Node.Port)
	}
	if cfg.Node.Interval != "30s" {
		t.Errorf("default Interval: got %s, want 30s", cfg.Node.Interval)
	}
	if cfg.Node.StaleThreshold != "90s" {
		t.Errorf("default StaleThreshold: got %s, want 90s", cfg.Node.StaleThreshold)
	}
	if cfg.Node.LogLevel != "info" {
		t.Errorf("default LogLevel: got %s, want info", cfg.Node.LogLevel)
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
	cfg := &NodeConfig{Interval: "10s"}
	d, err := cfg.ParseInterval()
	if err != nil {
		t.Fatalf("parse interval: %v", err)
	}
	if d.Seconds() != 10 {
		t.Errorf("Interval: got %v, want 10s", d)
	}
}

func TestParseInterval_Default(t *testing.T) {
	cfg := &NodeConfig{}
	d, err := cfg.ParseInterval()
	if err != nil {
		t.Fatalf("parse interval: %v", err)
	}
	if d.Seconds() != 30 {
		t.Errorf("Default interval: got %v, want 30s", d)
	}
}

func TestParseStaleThreshold(t *testing.T) {
	cfg := &NodeConfig{StaleThreshold: "120s"}
	d, err := cfg.ParseStaleThreshold()
	if err != nil {
		t.Fatalf("parse threshold: %v", err)
	}
	if d.Seconds() != 120 {
		t.Errorf("Threshold: got %v, want 120s", d)
	}
}

