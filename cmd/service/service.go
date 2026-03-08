// Package service implements the lanmon service management CLI.
package service

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"lanmon/pkg/config"
	"lanmon/pkg/logger"
)

// Run handles the service management commands: start, stop, restart, status.
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

	svcName := cfg.Connect.ServiceName
	if svcName == "" {
		svcName = "lanmon"
	}
	if !strings.HasSuffix(svcName, ".service") {
		svcName = svcName + ".service"
	}

	action := args[0]

	switch action {
	case "start", "stop", "restart":
		log.Info().Str("service", svcName).Str("action", action).Msg("Running systemctl command")
		return runSystemctl(action, svcName)
	case "status":
		return runSystemctlStatus(svcName)
	default:
		printUsage()
		return fmt.Errorf("unknown service action: %s", action)
	}
}

func runSystemctl(action, serviceName string) error {
	fmt.Printf("Running: systemctl %s %s\n", action, serviceName)

	cmd := exec.Command("systemctl", action, serviceName)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("systemctl %s %s failed: %w", action, serviceName, err)
	}

	actionLabel := map[string]string{
		"start":   "started",
		"stop":    "stopped",
		"restart": "restarted",
	}[action]
	fmt.Printf("✓ Service %s %s successfully.\n", serviceName, actionLabel)
	return nil
}

func runSystemctlStatus(serviceName string) error {
	cmd := exec.Command("systemctl", "status", serviceName)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Run() // status returns non-zero for stopped services, so ignore error
	return nil
}

func printUsage() {
	fmt.Println(`Usage: lanmon service <action>

Actions:
  start    Start the lanmon node service
  stop     Stop the lanmon node service
  restart  Restart the lanmon node service
  status   Show the status of the lanmon node service

Note: The service name is configured via 'service_name' in config (default: lanmon).`)
}
