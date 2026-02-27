// Package connect implements the lanmon connect CLI (SSH key distributor and launcher).
package connect

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/term"

	"lanmon/internal/rpc"
	"lanmon/internal/sshpush"
	"lanmon/internal/store"
	"lanmon/pkg/config"
	"lanmon/pkg/logger"
)

// Run starts the interactive SSH key distribution and connection CLI.
func Run(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	log := logger.Init(cfg.Node.LogLevel)

	// Connect to RPC server
	client, err := rpc.NewClient(cfg.Connect.RPCSocket)
	if err != nil {
		return fmt.Errorf("connecting to server: %w\nIs 'lanmon node' running?", err)
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

	// --- Determine the username to use ---
	// If key was already pushed, we know which user we pushed to previously.
	// Still ask, but default to "root"
	fmt.Print("Username [root]: ")
	username, _ := reader.ReadString('\n')
	username = strings.TrimSpace(username)
	if username == "" {
		username = "root"
	}

	pubKeyPath := cfg.Connect.ServerPubKey

	// --- Smart connect logic ---
	//
	// 1.  Key already pushed to this node (marked in DB)?
	//     → Try passwordless SSH immediately. If it works, exec into it.
	// 2.  Key exists locally but not marked as pushed?
	//     → Try passwordless SSH. If it works, connect and mark. If not, do key-push flow.
	// 3.  No local key at all?
	//     → Auto-generate, then run key-push flow.
	//

	if _, err := os.Stat(pubKeyPath); os.IsNotExist(err) {
		if err := generateSSHKey(pubKeyPath, reader); err != nil {
			return err
		}
	}

	// Try a quick passwordless probe — if it works, just connect
	if canSSHWithoutPassword(username, selectedHost.Beacon.IPAddress) {
		fmt.Printf("\n✓ Passwordless SSH already configured — connecting to %s@%s ...\n\n",
			username, selectedHost.Beacon.IPAddress)
		// Mark in DB in case it wasn't marked yet
		if !selectedHost.SSHKeyPushed {
			if err := client.MarkKeyPushed(selectedHost.Beacon.MACAddress); err != nil {
				log.Warn().Err(err).Msg("Failed to update key push status in database")
			}
		}
		return execSSH(username, selectedHost.Beacon.IPAddress)
	}

	// Passwordless didn't work — we need to push the key first
	if selectedHost.SSHKeyPushed {
		fmt.Printf("⚠  Previous key push recorded (at %s) but passwordless SSH still requires setup.\n",
			selectedHost.SSHKeyPushedAt.Format("2006-01-02 15:04:05"))
	}

	fmt.Printf("\nTo set up passwordless SSH to %s, enter the SSH password:\n", selectedHost.Beacon.Hostname)
	fmt.Print("SSH password: ")
	passwordBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	if err != nil {
		return fmt.Errorf("reading password: %w", err)
	}
	fmt.Println()
	password := string(passwordBytes)

	fmt.Printf("\nPushing SSH key to %s@%s...\n", username, selectedHost.Beacon.IPAddress)

	err = sshpush.PushKey(
		selectedHost.Beacon.IPAddress,
		22,
		username,
		password,
		pubKeyPath,
		cfg.Connect.KnownHosts,
	)

	// Zero password from memory
	for i := range passwordBytes {
		passwordBytes[i] = 0
	}

	if err != nil {
		return fmt.Errorf("SSH key push failed: %w", err)
	}

	// Mark key as pushed in DB
	if err := client.MarkKeyPushed(selectedHost.Beacon.MACAddress); err != nil {
		log.Warn().Err(err).Msg("Failed to update key push status in database")
	}

	fmt.Printf("\n✓ SSH key pushed to %s@%s — connecting now ...\n\n",
		username, selectedHost.Beacon.IPAddress)

	return execSSH(username, selectedHost.Beacon.IPAddress)
}

// generateSSHKey checks if a key exists and, if not, generates one.
func generateSSHKey(pubKeyPath string, reader *bufio.Reader) error {
	fmt.Printf("⚠  SSH public key not found at %s\n", pubKeyPath)
	fmt.Print("Would you like to generate a new SSH key pair? [Y/n]: ")
	ans, _ := reader.ReadString('\n')
	ans = strings.TrimSpace(strings.ToLower(ans))
	if ans != "" && ans != "y" {
		return fmt.Errorf("SSH key required to proceed")
	}

	privKeyPath := strings.TrimSuffix(pubKeyPath, ".pub")
	fmt.Printf("Generating key pair: %s ...\n", privKeyPath)

	sshDir := filepath.Dir(pubKeyPath)
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		return fmt.Errorf("creating SSH directory %s: %w", sshDir, err)
	}

	cmd := exec.Command("ssh-keygen", "-t", "rsa", "-b", "4096", "-f", privKeyPath, "-N", "")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ssh-keygen failed: %w", err)
	}
	fmt.Println("✓ SSH key pair generated.")
	return nil
}

// canSSHWithoutPassword tests if passwordless SSH works by attempting a quick connection.
func canSSHWithoutPassword(user, host string) bool {
	cmd := exec.Command("ssh",
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=no",
		"-o", "ConnectTimeout=5",
		"-o", "LogLevel=ERROR",
		fmt.Sprintf("%s@%s", user, host),
		"exit",
	)
	return cmd.Run() == nil
}

// execSSH replaces the current process with an interactive SSH session.
func execSSH(user, host string) error {
	sshBin, err := exec.LookPath("ssh")
	if err != nil {
		// Fall back to non-exec mode
		cmd := exec.Command("ssh", fmt.Sprintf("%s@%s", user, host))
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
	// Use syscall.Exec to replace the process so the terminal feels native
	args := []string{"ssh", fmt.Sprintf("%s@%s", user, host)}
	return syscall.Exec(sshBin, args, os.Environ())
}

func displayHostTable(hosts []store.HostRecord) {
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
