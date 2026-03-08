package discovery

import (
	"fmt"
	"math"
	"net"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/vmihailenco/msgpack/v5"

	"lanmon/internal/beacon"
	"lanmon/internal/cluster"
	"lanmon/internal/hosts"
	"lanmon/internal/store"
	"lanmon/internal/sysinfo"
)

const (
	maxPacketSize   = 4096
	timestampMaxAge = 60 // seconds
)

// rangeTarget holds the resolved broadcast address and interface info for one network range.
type rangeTarget struct {
	networkRange  string
	broadcastAddr *net.UDPAddr
	info          *sysinfo.SystemInfo
}

// StartNode begins the P2P discovery node (broadcast + listen) on multiple network ranges.
func StartNode(networkRanges []string, port int, interval time.Duration, secret string, db *store.Store, log zerolog.Logger) error {
	// Resolve all ranges
	var targets []rangeTarget
	var selfMACs []string

	for _, nr := range networkRanges {
		info, err := sysinfo.Collect(nr)
		if err != nil {
			log.Warn().Err(err).Str("range", nr).Msg("Skipping network range (no matching interface)")
			continue
		}

		_, ipNet, err := net.ParseCIDR(nr)
		if err != nil {
			log.Warn().Err(err).Str("range", nr).Msg("Skipping invalid CIDR")
			continue
		}

		broadcastIP := getBroadcastIP(ipNet)
		broadcastAddr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("%s:%d", broadcastIP, port))
		if err != nil {
			log.Warn().Err(err).Str("range", nr).Msg("Skipping unresolvable broadcast address")
			continue
		}

		targets = append(targets, rangeTarget{
			networkRange:  nr,
			broadcastAddr: broadcastAddr,
			info:          info,
		})
		selfMACs = append(selfMACs, info.MACAddress)

		log.Info().
			Str("interface_ip", info.IPAddress).
			Str("mac", info.MACAddress).
			Str("broadcast", broadcastAddr.String()).
			Str("range", nr).
			Msg("Network range configured")
	}

	if len(targets) == 0 {
		return fmt.Errorf("no valid network ranges found — check your config and network interfaces")
	}

	// Build a set of self MACs for fast lookup
	selfMACSet := make(map[string]bool)
	for _, mac := range selfMACs {
		selfMACSet[mac] = true
	}

	// Single UDP listener on 0.0.0.0:<port> receives from all ranges
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: port})
	if err != nil {
		return fmt.Errorf("listening on UDP port %d: %w", port, err)
	}

	log.Info().
		Int("port", port).
		Int("ranges", len(targets)).
		Dur("interval", interval).
		Msg("P2P Discovery node started")

	// Pre-populate DB with local ranges for visibility in local 'cluster list'
	for _, t := range targets {
		payload := beacon.BeaconPayload{
			Timestamp:  time.Now().Unix(),
			MACAddress: t.info.MACAddress,
			IPAddress:  t.info.IPAddress,
			Hostname:   t.info.Hostname,
			OS: beacon.OSInfo{
				Name:   t.info.OSName,
				Kernel: t.info.Kernel,
				Arch:   t.info.Arch,
			},
			Hardware: beacon.HWInfo{
				CPUModel:  t.info.CPUModel,
				CPUCores:  t.info.CPUCores,
				MemoryGB:  t.info.MemoryGB,
				DiskCount: t.info.DiskCount,
			},
		}
		db.Upsert(payload)
	}

	// Start listener
	go listen(conn, selfMACSet, secret, db, log)

	// Start broadcast loop
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Initial broadcast on all ranges
	broadcastAll(conn, targets, secret, db, log)

	for range ticker.C {
		broadcastAll(conn, targets, secret, db, log)
	}

	return nil
}

// WatchForClusterAssignment monitors the local store and triggers a key pull
// if the node is marked as part of a cluster but lacks the cluster keys.
// It also ensures known_hosts is kept in sync for all cluster members.
func WatchForClusterAssignment(db *store.Store, sharedSecret, clusterKeyPath, knownHostsPath string, log zerolog.Logger) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Initial check
	checkClusterProvisioning(db, sharedSecret, clusterKeyPath, knownHostsPath, log)

	for range ticker.C {
		checkClusterProvisioning(db, sharedSecret, clusterKeyPath, knownHostsPath, log)
	}
}

func checkClusterProvisioning(db *store.Store, sharedSecret, clusterKeyPath, knownHostsPath string, log zerolog.Logger) {
	// 1. Get local system info
	info, err := sysinfo.Collect("")
	if err != nil {
		return
	}

	// 2. Check if local node is marked as in cluster
	all, _ := db.GetAll()
	var selfRecord *store.HostRecord
	for _, r := range all {
		if r.Beacon.MACAddress == info.MACAddress {
			selfRecord = &r
			break
		}
	}

	if selfRecord == nil || !selfRecord.IsInCluster {
		return
	}

	// 3. Always sync known_hosts for active cluster members
	var clusterMembers []store.HostRecord
	for _, r := range all {
		if r.IsInCluster {
			clusterMembers = append(clusterMembers, r)
		}
	}
	cluster.SyncClusterKnownHosts(clusterMembers, knownHostsPath, log)

	// 4. Check if we already have the keys
	if _, err := os.Stat(clusterKeyPath); err == nil {
		return
	}

	log.Warn().Msg("Node is marked in cluster but lacks cluster keys. Attempting to pull from peers...")

	// 5. Find an active peer that IS in the cluster to pull from
	for _, r := range all {
		if r.Beacon.MACAddress == info.MACAddress {
			continue
		}
		if r.Active && r.IsInCluster {
			log.Info().Str("peer", r.Beacon.Hostname).Str("ip", r.Beacon.IPAddress).Msg("Attempting to pull keys from peer...")
			// Network RPC on port 9876
			err := cluster.ProvisionClusterKeys(r.Beacon.IPAddress, 9876, sharedSecret, clusterKeyPath, log)
			if err == nil {
				log.Info().Msg("Successfully provisioned cluster keys from peer.")
				return
			}
			log.Warn().Err(err).Str("peer", r.Beacon.Hostname).Msg("Failed to pull keys from peer, trying next...")
		}
	}
}

func broadcastAll(conn *net.UDPConn, targets []rangeTarget, secret string, db *store.Store, log zerolog.Logger) {
	for _, t := range targets {
		broadcastOne(conn, t, secret, db, log)
	}
}

func broadcastOne(conn *net.UDPConn, target rangeTarget, secret string, db *store.Store, log zerolog.Logger) {
	info, err := sysinfo.Collect(target.networkRange)
	if err != nil {
		log.Error().Err(err).Str("range", target.networkRange).Msg("Failed to collect system info for broadcast")
		return
	}

	// Get the local cluster MACs to broadcast to peers
	clusterMACs, _ := db.GetClusterMACs()

	payload := &beacon.BeaconPayload{
		Version:    1,
		Timestamp:  time.Now().Unix(),
		MACAddress: info.MACAddress,
		IPAddress:  info.IPAddress,
		Hostname:   info.Hostname,
		OS: beacon.OSInfo{
			Name:   info.OSName,
			Kernel: info.Kernel,
			Arch:   info.Arch,
		},
		Hardware: beacon.HWInfo{
			CPUModel:  info.CPUModel,
			CPUCores:  info.CPUCores,
			MemoryGB:  info.MemoryGB,
			DiskCount: info.DiskCount,
		},
		ClusterMACs: clusterMACs,
	}

	data, err := msgpack.Marshal(payload)
	if err != nil {
		log.Error().Err(err).Msg("Marshaling payload failed")
		return
	}

	hmacSig := beacon.ComputeHMAC(data, secret)
	packet := append(hmacSig, data...)

	_, err = conn.WriteToUDP(packet, target.broadcastAddr)
	if err != nil {
		log.Error().Err(err).Str("target", target.broadcastAddr.String()).Msg("Failed to send broadcast beacon")
		return
	}

	// Refresh local node's own DB record so it always appears Active in cluster list.
	// (Self-beacons are dropped by the listener to avoid loops — so we do it here.)
	db.Upsert(*payload)

	log.Debug().
		Str("target", target.broadcastAddr.String()).
		Int("bytes", len(packet)).
		Msg("Beacon broadcasted")
}

func listen(conn *net.UDPConn, selfMACs map[string]bool, secret string, db *store.Store, log zerolog.Logger) {
	buf := make([]byte, maxPacketSize)
	for {
		n, src, err := conn.ReadFromUDP(buf)
		if err != nil {
			log.Error().Err(err).Msg("Error reading from UDP")
			continue
		}

		packet := make([]byte, n)
		copy(packet, buf[:n])

		go handlePacket(packet, src, selfMACs, secret, db, log)
	}
}

func handlePacket(packet []byte, src *net.UDPAddr, selfMACs map[string]bool, secret string, db *store.Store, log zerolog.Logger) {
	if len(packet) <= beacon.HMACSize {
		return
	}

	sig := packet[:beacon.HMACSize]
	data := packet[beacon.HMACSize:]

	if !beacon.VerifyHMAC(sig, data, secret) {
		log.Warn().Str("src", src.String()).Msg("HMAC validation failed")
		return
	}

	var payload beacon.BeaconPayload
	if err := msgpack.Unmarshal(data, &payload); err != nil {
		log.Error().Err(err).Str("src", src.String()).Msg("Failed to unmarshal beacon")
		return
	}

	// Ignore beacons from self (check all our MACs)
	if selfMACs[payload.MACAddress] {
		return
	}

	now := time.Now().Unix()
	if math.Abs(float64(now-payload.Timestamp)) > timestampMaxAge {
		log.Warn().Str("src", src.String()).Msg("Stale timestamp in beacon")
		return
	}

	log.Info().
		Str("hostname", payload.Hostname).
		Str("ip", payload.IPAddress).
		Msg("Peer discovered")

	if err := db.Upsert(payload); err != nil {
		log.Error().Err(err).Msg("Database write error")
		return
	}

	// Sync /etc/hosts for resolution
	if err := hosts.Sync(db); err != nil {
		log.Warn().Err(err).Msg("Failed to sync /etc/hosts (permission denied?)")
	}
}

func getBroadcastIP(n *net.IPNet) net.IP {
	ip := n.IP.To4()
	if ip == nil {
		return nil
	}
	mask := n.Mask
	broadcastIP := make(net.IP, len(ip))
	for i := range ip {
		broadcastIP[i] = ip[i] | ^mask[i]
	}
	return broadcastIP
}
