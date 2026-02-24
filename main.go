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
	"lanmon/cmd/connect"
	"lanmon/cmd/server"
)

const (
	defaultConfigPath = "/etc/lanmon/config.toml"
	version           = "1.0.0"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	configPath := defaultConfigPath

	// Parse --config flag if present
	args := os.Args[1:]
	for i, arg := range args {
		if arg == "--config" && i+1 < len(args) {
			configPath = args[i+1]
			args = append(args[:i], args[i+2:]...)
			break
		}
		if len(arg) > 9 && arg[:9] == "--config=" {
			configPath = arg[9:]
			args = append(args[:i], args[i+1:]...)
			break
		}
	}

	if len(args) == 0 {
		printUsage()
		os.Exit(1)
	}

	subcommand := args[0]
	var err error

	switch subcommand {
	case "agent":
		err = agent.Run(configPath)
	case "server":
		err = server.Run(configPath)
	case "connect":
		err = connect.Run(configPath)
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
	fmt.Printf(`lanmon v%s — LAN Discovery & SSH Key Exchange System

Usage:
  lanmon <command> [--config <path>]

Commands:
  agent    Start the LANBeacon agent (broadcasts system info)
  server   Start the LANListener server (captures beacons, stores hosts)
  connect  Launch the LANConnect SSH key distributor (interactive)
  version  Print version information
  help     Show this help message

Options:
  --config <path>  Path to config file (default: %s)

Examples:
  lanmon agent                          # Start agent with default config
  lanmon server --config ./config.toml  # Start server with custom config
  lanmon connect                        # Interactive SSH key push

`, version, defaultConfigPath)
}
