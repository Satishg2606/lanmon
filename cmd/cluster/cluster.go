// Package cluster implements the lanmon cluster CLI.
package cluster

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/rs/zerolog"

	"lanmon/internal/cluster"
	"lanmon/internal/rpc"
	"lanmon/internal/store"
	"lanmon/internal/sysinfo"
	"lanmon/pkg/config"
	"lanmon/pkg/logger"
)

// Run starts the cluster management CLI.
func Run(configPath string, args []string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	log := logger.Init(cfg.Node.LogLevel, cfg.Node.LogFile)

	if len(args) == 0 {
		printUsage()
		return nil
	}

	client, err := rpc.NewClient(cfg.Connect.RPCSocket)
	if err != nil {
		return fmt.Errorf("connecting to server: %w\nIs 'lanmon node' running?", err)
	}
	defer client.Close()

	command := args[0]
	switch command {
	case "add":
		return handleAdd(client, cfg, log)
	case "list":
		return handleList(client)
	case "remove":
		return handleRemove(client, cfg, log)
	case "remove-self":
		return handleRemoveSelf(client, cfg, log)
	default:
		printUsage()
		return fmt.Errorf("unknown cluster command: %s", command)
	}
}

func handleAdd(client *rpc.Client, cfg *config.Config, log zerolog.Logger) error {
	hosts, err := client.ListActiveHosts()
	if err != nil {
		return fmt.Errorf("fetching hosts: %w", err)
	}

	if len(hosts) == 0 {
		fmt.Println("No active hosts discovered.")
		return nil
	}

	displayHostTable(hosts)

	reader := bufio.NewReader(os.Stdin)
	fmt.Print("\nEnter host index to add to cluster: ")
	indexStr, _ := reader.ReadString('\n')
	index, err := strconv.Atoi(strings.TrimSpace(indexStr))
	if err != nil || index < 1 || index > len(hosts) {
		return fmt.Errorf("invalid index")
	}

	selected := hosts[index-1]
	fmt.Printf("Selected: %s (%s)\n", selected.Beacon.Hostname, selected.Beacon.IPAddress)

	// In the new Zero-Touch model, we don't need SSH passwords.
	// We just mark the node in our DB, and the new node will "pull" the keys.
	fmt.Printf("Adding %s to cluster via beacon propagation...\n", selected.Beacon.Hostname)

	// Generate cluster key if it doesn't exist
	if _, err := os.Stat(cfg.Connect.ClusterKey); os.IsNotExist(err) {
		fmt.Printf("Creating cluster key at %s...\n", cfg.Connect.ClusterKey)
		cmd := exec.Command("ssh-keygen", "-t", "rsa", "-b", "4096", "-f", cfg.Connect.ClusterKey, "-N", "")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to generate cluster key: %w", err)
		}
	}

	// Mark the new node in local DB
	err = client.MarkClusterNode(selected.Beacon.MACAddress, true)
	if err != nil {
		return fmt.Errorf("failed to mark node in DB: %w", err)
	}

	// Also mark the local node as cluster member to ensure it bootstraps
	info, err := sysinfo.Collect("")
	if err == nil {
		client.MarkClusterNode(info.MACAddress, true)
	}

	// Propagate addition to other members
	fmt.Println("Propagating addition to other cluster members...")
	cluster.PushMembershipChange(client, selected.Beacon.MACAddress, true, cfg.Connect.ClusterKey, log)

	fmt.Println("✓ Node added to cluster successfully.")
	fmt.Println("  Cluster membership will sync to all peers via beacons within ~15 seconds.")

	return nil
}

func handleList(client *rpc.Client) error {
	hosts, err := client.ListClusterNodes()
	if err != nil {
		return fmt.Errorf("fetching cluster nodes: %w", err)
	}

	if len(hosts) == 0 {
		fmt.Println("No nodes in cluster.")
		return nil
	}

	fmt.Printf("\n  Cluster Nodes (%d)\n\n", len(hosts))
	displayHostTable(hosts)
	return nil
}

func handleRemove(client *rpc.Client, cfg *config.Config, log zerolog.Logger) error {
	hosts, err := client.ListClusterNodes()
	if err != nil {
		return fmt.Errorf("fetching clusters: %w", err)
	}

	if len(hosts) == 0 {
		fmt.Println("No nodes in cluster.")
		return nil
	}

	displayHostTable(hosts)
	fmt.Print("\nEnter node index to remove: ")
	reader := bufio.NewReader(os.Stdin)
	indexStr, _ := reader.ReadString('\n')
	index, _ := strconv.Atoi(strings.TrimSpace(indexStr))

	if index < 1 || index > len(hosts) {
		return fmt.Errorf("invalid index")
	}

	selected := hosts[index-1]

	// Unmark locally
	err = client.MarkClusterNode(selected.Beacon.MACAddress, false)
	if err != nil {
		return fmt.Errorf("failed to unmark node: %w", err)
	}

	// Propagate removal to other members
	fmt.Printf("Propagating removal of %s to other cluster members...\n", selected.Beacon.Hostname)
	cluster.PushMembershipChange(client, selected.Beacon.MACAddress, false, cfg.Connect.ClusterKey, log)

	// Also immediately try to remove keys from the remote node
	fmt.Printf("Removing cluster keys from %s...\n", selected.Beacon.Hostname)
	err = cluster.RemoveKeys(selected.Beacon.IPAddress, 22, "root", cfg.Connect.ClusterKey)
	if err != nil {
		log.Warn().Err(err).Msg("Could not remove remote keys (node may be unreachable)")
		fmt.Printf("  ⚠  Could not remove keys from remote: %v\n", err)
	} else {
		fmt.Println("  ✓ Remote keys removed.")
	}

	fmt.Printf("✓ Removed %s from cluster.\n", selected.Beacon.Hostname)
	fmt.Println("  Removal will sync to all peers via beacons within ~15 seconds.")
	return nil
}

func displayHostTable(hosts []store.HostRecord) {
	fmt.Printf("  %-4s %-20s %-16s %-18s %-10s\n", "#", "Hostname", "IP Address", "MAC Address", "Active")
	fmt.Printf("  %s %s %s %s %s\n", strings.Repeat("─", 4), strings.Repeat("─", 20), strings.Repeat("─", 16), strings.Repeat("─", 18), strings.Repeat("─", 10))
	for i, h := range hosts {
		active := "✗"
		if h.Active {
			active = "✓"
		}
		fmt.Printf("  %-4d %-20s %-16s %-18s %-10s\n", i+1, h.Beacon.Hostname, h.Beacon.IPAddress, h.Beacon.MACAddress, active)
	}
}

func printUsage() {
	fmt.Println(`Usage: lanmon cluster <command>

Commands:
  add          Add a discovered node to the cluster
  list         List all cluster nodes
  remove       Remove a node from the cluster
  remove-self  Remove this node from the cluster and cut all cluster SSH access`)
}

// handleRemoveSelf removes the local node from the cluster and isolates it.
func handleRemoveSelf(client *rpc.Client, cfg *config.Config, log zerolog.Logger) error {
	// Determine local MAC address
	var localMAC string
	if len(cfg.Node.NetworkRanges) > 0 {
		info, err := sysinfo.Collect(cfg.Node.NetworkRanges[0])
		if err == nil {
			localMAC = info.MACAddress
		}
	}

	if localMAC == "" {
		return fmt.Errorf("could not determine local MAC address — is network_ranges set in config?")
	}

	// Show the current cluster membership so user knows what they're leaving
	hosts, err := client.ListClusterNodes()
	if err != nil {
		return fmt.Errorf("fetching cluster nodes: %w", err)
	}

	if len(hosts) == 0 {
		fmt.Println("This node does not appear to be in any cluster.")
		return nil
	}

	fmt.Printf("\n  Current cluster (%d nodes):\n", len(hosts))
	displayHostTable(hosts)

	// Find self in the list
	inCluster := false
	for _, h := range hosts {
		if h.Beacon.MACAddress == localMAC {
			inCluster = true
			break
		}
	}
	if !inCluster {
		fmt.Println("\nThis node is not listed as a cluster member.")
		return nil
	}

	fmt.Printf(`
⚠  WARNING: This will:
  • Remove this node from the cluster on all peers
  • Delete local cluster SSH keys (%s, %s.pub)
  • Remove the cluster key from local authorized_keys
  • Remove cluster peer IPs from local known_hosts

This node will no longer be able to SSH into cluster peers, and peers
will no longer be able to SSH into this node via cluster keys.

`, cfg.Connect.ClusterKey, cfg.Connect.ClusterKey)

	fmt.Print("Are you sure you want to remove this node from the cluster? [y/N]: ")
	var answer string
	fmt.Scanln(&answer)
	if strings.ToLower(strings.TrimSpace(answer)) != "y" {
		fmt.Println("Aborted.")
		return nil
	}

	fmt.Println("\nRemoving this node from the cluster...")
	if err := cluster.RemoveSelf(client, cfg.Connect.ClusterKey, cfg.Connect.KnownHosts, localMAC, log); err != nil {
		return fmt.Errorf("remove-self failed: %w", err)
	}

	fmt.Println("✓ This node has been removed from the cluster and isolated.")
	fmt.Println("  You may stop the lanmon node service if it is no longer needed.")
	return nil
}
