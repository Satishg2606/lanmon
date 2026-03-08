// lanmon — LAN Discovery & SSH Key Exchange System
//
// Usage:
//
//	lanmon agent   — broadcast system info via UDP multicast
//	lanmon server  — capture beacons and store host records
//	lanmon connect — list hosts and push SSH public key
package main

import (
	"fmt"
	"os"

	"lanmon/cmd/agent"
	"lanmon/cmd/cluster"
	"lanmon/cmd/connect"
	"lanmon/cmd/node"
	"lanmon/cmd/server"
	"lanmon/cmd/service"
)

const (
	defaultSystemPath = "/etc/lanmon/config.toml"
	defaultLocalPath  = "config.toml"
	version           = "1.1.0"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	configPath := ""

	// Parse --config flag if present
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--config" && i+1 < len(args) {
			configPath = args[i+1]
			args = append(args[:i], args[i+2:]...)
			i--
			continue
		}
		if len(arg) > 9 && arg[:9] == "--config=" {
			configPath = arg[9:]
			args = append(args[:i], args[i+1:]...)
			i--
			continue
		}
	}

	// Auto-discover config if not specified
	if configPath == "" {
		if _, err := os.Stat(defaultLocalPath); err == nil {
			configPath = defaultLocalPath
		} else {
			configPath = defaultSystemPath
		}
	}

	if len(args) == 0 {
		printUsage()
		os.Exit(1)
	}

	subcommand := args[0]
	var err error

	switch subcommand {
	case "node":
		err = node.Run(configPath)
	case "agent":
		fmt.Println("⚠ 'agent' is deprecated. Use 'lanmon node' for P2P discovery.")
		err = agent.Run(configPath)
	case "server":
		fmt.Println("⚠ 'server' is deprecated. Use 'lanmon node' for P2P discovery.")
		err = server.Run(configPath)
	case "connect":
		err = connect.Run(configPath)
	case "cluster":
		err = cluster.Run(configPath, args[1:])
	case "service":
		err = service.Run(configPath, args[1:])
	case "edit":
		err = node.EditConfig(configPath)
	case "version":
		fmt.Printf("lanmon v%s\n", version)
		return
	case "help", "--help", "-h":
		printUsage()
		return
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", subcommand)
		printUsage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Printf(`lanmon v%s — P2P LAN Discovery & SSH Key Exchange System

Usage:
  lanmon <command> [--config <path>]

Commands:
  node     Start the P2P discovery node (broadcasts & listens)
  connect  Launch the LANConnect SSH key distributor (interactive)
  cluster  Manage cluster nodes (add, list, remove, remove-self)
  service  Manage the lanmon systemd service (start, stop, restart, status)
  edit     Edit the configuration file in your system editor
  version  Print version information
  help     Show this help message

Options:
  --config <path>  Path to config file (default: looks for ./config.toml, then %s)

Examples:
  lanmon node                           # Start P2P node with default config
  lanmon edit                           # Edit configuration
  lanmon connect                        # Interactive SSH key push
  lanmon cluster add 1                  # Add first discovered node to cluster
  lanmon cluster remove-self            # Remove self from cluster (with isolation)
  lanmon service restart                # Restart the lanmon service

`, version, defaultSystemPath)
}

