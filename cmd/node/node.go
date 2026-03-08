package node

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"lanmon/internal/cluster"
	"lanmon/internal/discovery"
	"lanmon/internal/hosts"
	"lanmon/internal/rpc"
	"lanmon/internal/sysinfo"

	"lanmon/internal/store"
	"lanmon/pkg/config"
	"lanmon/pkg/logger"
)

// Run starts the P2P discovery node.
func Run(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	log := logger.Init(cfg.Node.LogLevel, cfg.Node.LogFile)

	if cfg.Node.SharedSecret == "" || cfg.Node.SharedSecret == "CHANGE_ME" {
		return fmt.Errorf("shared_secret must be set in config (not 'CHANGE_ME')")
	}

	if len(cfg.Node.NetworkRanges) == 0 {
		return fmt.Errorf("network_ranges must be set in config (e.g. ['10.51.240.0/23'])")
	}

	// Ensure database directory exists
	dbDir := filepath.Dir(cfg.Node.DBPath)
	if err := os.MkdirAll(dbDir, 0700); err != nil {
		return fmt.Errorf("creating database directory %s: %w", dbDir, err)
	}

	// Ensure RPC socket directory exists
	sockDir := filepath.Dir(cfg.Node.RPCSocket)
	if err := os.MkdirAll(sockDir, 0700); err != nil {
		return fmt.Errorf("creating socket directory %s: %w", sockDir, err)
	}

	// Open store
	db, err := store.New(cfg.Node.DBPath, log)
	if err != nil {
		return fmt.Errorf("opening store: %w", err)
	}
	defer db.Close()

	// Initial sync of /etc/hosts from database
	if err := hosts.Sync(db); err != nil {
		log.Warn().Err(err).Msg("Failed to perform initial /etc/hosts sync")
	}

	// Start stale host expiry
	staleThreshold, err := cfg.Node.ParseStaleThreshold()
	if err != nil {
		return fmt.Errorf("parsing stale threshold: %w", err)
	}
	db.RunExpiry(5*time.Second, staleThreshold)

	// Start cluster quorum loop (every 15 seconds)
	removedCh := db.RunQuorum(15 * time.Second)

	// Get local MAC for cleanup decisions
	localMAC := ""
	if len(cfg.Node.NetworkRanges) > 0 {
		info, err := sysinfo.Collect(cfg.Node.NetworkRanges[0])
		if err == nil {
			localMAC = info.MACAddress
		}
	}

	// Background goroutine to handle cleanup when nodes are removed by quorum
	go func() {
		for removedMACs := range removedCh {
			// Build a map of MAC -> IP from the database
			allHosts, err := db.GetAll()
			if err != nil {
				log.Error().Err(err).Msg("Failed to get hosts for cleanup")
				continue
			}
			hostMap := make(map[string]string)
			for _, h := range allHosts {
				hostMap[h.Beacon.MACAddress] = h.Beacon.IPAddress
			}

			cluster.HandleRemovedNodes(removedMACs, hostMap, localMAC, cfg.Connect.ClusterKey, log)
		}
	}()

	// Start RPC server (for 'lanmon connect' to query this node)
	if err := rpc.StartServer(cfg.Node.RPCSocket, db, log, cfg.Node.SharedSecret, cfg.Connect.ClusterKey); err != nil {
		return fmt.Errorf("starting RPC server: %w", err)
	}

	// Start Network RPC server (for inter-node key distribution)
	// Default to port 9876 for now
	if err := rpc.StartNetworkServer(":9876", db, log, cfg.Node.SharedSecret, cfg.Connect.ClusterKey); err != nil {
		log.Warn().Err(err).Msg("Failed to start network RPC server (inter-node sync may fail)")
	}

	interval, err := cfg.Node.ParseInterval()
	if err != nil {
		return fmt.Errorf("parsing interval: %w", err)
	}

	log.Info().
		Str("db_path", cfg.Node.DBPath).
		Strs("network_ranges", cfg.Node.NetworkRanges).
		Msg("Starting LANNode P2P Discovery")

	// Start discovery in a goroutine
	errCh := make(chan error, 1)
	go func() {
		// Start watcher for cluster assignment
		go discovery.WatchForClusterAssignment(db, cfg.Node.SharedSecret, cfg.Connect.ClusterKey, cfg.Connect.KnownHosts, log)

		errCh <- discovery.StartNode(
			cfg.Node.NetworkRanges,
			cfg.Node.Port,
			interval,
			cfg.Node.SharedSecret,
			db,
			log,
		)
	}()

	// Wait for shutdown signal or discovery error
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		return fmt.Errorf("discovery error: %w", err)
	case sig := <-sigCh:
		log.Info().Str("signal", sig.String()).Msg("Shutting down")
		os.Remove(cfg.Node.RPCSocket)
		return nil
	}
}
