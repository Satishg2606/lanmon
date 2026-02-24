// Package connect implements the lanmon connect CLI (SSH key distributor).
package connect

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"

	"lanmon/internal/rpc"
	"lanmon/internal/store"
	"lanmon/pkg/config"
	"lanmon/pkg/logger"
	"lanmon/internal/sshpush"
)

// Run starts the interactive SSH key distribution CLI.
func Run(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	log := logger.Init(cfg.Server.LogLevel)

	// Connect to RPC server
	client, err := rpc.NewClient(cfg.Connect.RPCSocket)
	if err != nil {
		return fmt.Errorf("connecting to server: %w\nIs 'lanmon server' running?", err)
	}
	defer client.Close()

	// Fetch active hosts
	hosts, err := client.ListActiveHosts()
	if err != nil {
		return fmt.Errorf("fetching active hosts: %w", err)
	}

	if len(hosts) == 0 {
		fmt.Println("No active hosts discovered. Make sure agents are running.")
		return nil
	}

	// Display host table
	fmt.Printf("\n  Active Hosts (%d found)\n\n", len(hosts))
	displayHostTable(hosts)

	reader := bufio.NewReader(os.Stdin)

	// Prompt for host selection
	fmt.Print("\nEnter host index: ")
	indexStr, _ := reader.ReadString('\n')
	indexStr = strings.TrimSpace(indexStr)
	index, err := strconv.Atoi(indexStr)
	if err != nil || index < 1 || index > len(hosts) {
		return fmt.Errorf("invalid host index: %s", indexStr)
	}

	selectedHost := hosts[index-1]
	fmt.Printf("\nSelected: %s (%s)\n", selectedHost.Beacon.Hostname, selectedHost.Beacon.IPAddress)

	if selectedHost.SSHKeyPushed {
		fmt.Printf("⚠  SSH key was already pushed to this host at %s\n",
			selectedHost.SSHKeyPushedAt.Format("2006-01-02 15:04:05"))
		fmt.Print("Continue anyway? [y/N]: ")
		confirm, _ := reader.ReadString('\n')
		if strings.TrimSpace(strings.ToLower(confirm)) != "y" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	// Prompt for target user
	fmt.Print("Target username: ")
	username, _ := reader.ReadString('\n')
	username = strings.TrimSpace(username)
	if username == "" {
		return fmt.Errorf("username cannot be empty")
	}

	// Prompt for password (no echo)
	fmt.Print("SSH password: ")
	passwordBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	if err != nil {
		return fmt.Errorf("reading password: %w", err)
	}
	fmt.Println() // newline after password
	password := string(passwordBytes)

	// Validate public key file exists
	if _, err := os.Stat(cfg.Connect.ServerPubKey); os.IsNotExist(err) {
		return fmt.Errorf("server public key not found at %s\nRun 'ssh-keygen' to generate one", cfg.Connect.ServerPubKey)
	}

	fmt.Printf("\nPushing SSH key to %s@%s...\n", username, selectedHost.Beacon.IPAddress)

	// Push the key
	err = sshpush.PushKey(
		selectedHost.Beacon.IPAddress,
		22,
		username,
		password,
		cfg.Connect.ServerPubKey,
		cfg.Connect.KnownHosts,
	)

	// Zero out password from memory
	for i := range passwordBytes {
		passwordBytes[i] = 0
	}

	if err != nil {
		return fmt.Errorf("SSH key push failed: %w", err)
	}

	// Mark key as pushed via RPC
	if err := client.MarkKeyPushed(selectedHost.Beacon.MACAddress); err != nil {
		log.Warn().Err(err).Msg("Failed to update key push status in database")
	}

	fmt.Printf("\n✓ SSH key successfully pushed to %s@%s\n", username, selectedHost.Beacon.IPAddress)
	fmt.Println("  Passwordless SSH authentication is now configured.")
	return nil
}

func displayHostTable(hosts []store.HostRecord) {
	// Header
	fmt.Printf("  %-4s %-20s %-16s %-18s %-25s %-10s %-5s\n",
		"#", "Hostname", "IP Address", "MAC Address", "OS", "Last Seen", "Key")
	fmt.Printf("  %s %s %s %s %s %s %s\n",
		strings.Repeat("─", 4),
		strings.Repeat("─", 20),
		strings.Repeat("─", 16),
		strings.Repeat("─", 18),
		strings.Repeat("─", 25),
		strings.Repeat("─", 10),
		strings.Repeat("─", 5))

	for i, host := range hosts {
		keyStatus := "✗"
		if host.SSHKeyPushed {
			keyStatus = "✓"
		}

		// Truncate long fields
		hostname := truncate(host.Beacon.Hostname, 20)
		osName := truncate(host.Beacon.OS.Name, 25)

		fmt.Printf("  %-4d %-20s %-16s %-18s %-25s %-10s %-5s\n",
			i+1,
			hostname,
			host.Beacon.IPAddress,
			host.Beacon.MACAddress,
			osName,
			host.LastSeen.Format("15:04:05"),
			keyStatus,
		)
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "…"
}
