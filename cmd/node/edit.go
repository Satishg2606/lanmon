package node

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const defaultConfigTemplate = `[node]
  network_range   = "10.51.240.0/23"
  port            = 5678
  interval        = "30s"
  shared_secret   = "CHANGE_ME"
  db_path         = "/var/lib/lanmon/hosts.db"
  rpc_socket      = "/run/lanmon/server.sock"
  stale_threshold = "90s"
  log_level       = "info"

[connect]
  rpc_socket     = "/run/lanmon/server.sock"
  server_pubkey  = "~/.ssh/id_rsa.pub"
  known_hosts    = "/etc/lanmon/known_hosts"
`

// EditConfig opens the configuration file in the system editor.
// If the file does not exist, it creates it with default values.
func EditConfig(path string) error {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("creating directory %s: %w", dir, err)
		}
	}

	// Create file if it doesn't exist
	if _, err := os.Stat(path); os.IsNotExist(err) {
		fmt.Printf("Creating new config file at %s...\n", path)
		if err := os.WriteFile(path, []byte(defaultConfigTemplate), 0644); err != nil {
			return fmt.Errorf("writing default config: %w", err)
		}
	}

	// Determine editor
	editor := os.Getenv("EDITOR")
	if editor == "" {
		// Fallback to vi or nano
		for _, e := range []string{"vi", "nano", "vim"} {
			if _, err := exec.LookPath(e); err == nil {
				editor = e
				break
			}
		}
	}

	if editor == "" {
		return fmt.Errorf("no editor found ($EDITOR environment variable not set, and vi/nano/vim not in PATH)")
	}

	// Run editor
	cmd := exec.Command(editor, path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}
