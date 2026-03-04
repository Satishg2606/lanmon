package discovery

import (
	"fmt"
	"math"
	"net"
	"time"

	"github.com/rs/zerolog"
	"github.com/vmihailenco/msgpack/v5"

	"lanmon/internal/beacon"
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

	// Start listener
	go listen(conn, selfMACSet, secret, db, log)

	// Start broadcast loop
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Initial broadcast on all ranges
	broadcastAll(conn, targets, secret, log)

	for range ticker.C {
		broadcastAll(conn, targets, secret, log)
	}

	return nil
}

func broadcastAll(conn *net.UDPConn, targets []rangeTarget, secret string, log zerolog.Logger) {
	for _, t := range targets {
		broadcastOne(conn, t, secret, log)
	}
}

func broadcastOne(conn *net.UDPConn, target rangeTarget, secret string, log zerolog.Logger) {
	info, err := sysinfo.Collect(target.networkRange)
	if err != nil {
		log.Error().Err(err).Str("range", target.networkRange).Msg("Failed to collect system info for broadcast")
		return
	}

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
