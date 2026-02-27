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

// StartNode begins the P2P discovery node (broadcast + listen).
func StartNode(networkRange string, port int, interval time.Duration, secret string, db *store.Store, log zerolog.Logger) error {
	// Auto-detect interface and info matching the network range
	info, err := sysinfo.Collect(networkRange)
	if err != nil {
		return fmt.Errorf("auto-detecting interface: %w", err)
	}

	log.Info().
		Str("interface_ip", info.IPAddress).
		Str("mac", info.MACAddress).
		Str("network_range", networkRange).
		Msg("Node interface detected")

	// Calculate broadcast address
	_, ipNet, err := net.ParseCIDR(networkRange)
	if err != nil {
		return fmt.Errorf("parsing network range: %w", err)
	}
	broadcastIP := getBroadcastIP(ipNet)
	broadcastAddr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("%s:%d", broadcastIP, port))
	if err != nil {
		return fmt.Errorf("resolving broadcast address: %w", err)
	}

	// Create UDP connection for both sending and receiving
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: port})
	if err != nil {
		return fmt.Errorf("listening on UDP port %d: %w", port, err)
	}
	// Note: We don't defer conn.Close() here because it's a long-running node,
	// and we might want to manage it differently if we added graceful shutdown.

	log.Info().
		Str("broadcast_target", broadcastAddr.String()).
		Int("port", port).
		Dur("interval", interval).
		Msg("P2P Discovery node started")

	// Start listener in a goroutine
	go listen(conn, info.MACAddress, secret, db, log)

	// Start broadcast loop
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Initial broadcast
	broadcast(conn, broadcastAddr, secret, networkRange, log)

	for range ticker.C {
		broadcast(conn, broadcastAddr, secret, networkRange, log)
	}

	return nil
}

func broadcast(conn *net.UDPConn, addr *net.UDPAddr, secret string, networkRange string, log zerolog.Logger) {
	info, err := sysinfo.Collect(networkRange)
	if err != nil {
		log.Error().Err(err).Msg("Failed to collect system info for broadcast")
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

	_, err = conn.WriteToUDP(packet, addr)
	if err != nil {
		log.Error().Err(err).Str("target", addr.String()).Msg("Failed to send broadcast beacon")
		return
	}

	log.Debug().
		Str("target", addr.String()).
		Int("bytes", len(packet)).
		Msg("Beacon broadcasted")
}

func listen(conn *net.UDPConn, selfMAC string, secret string, db *store.Store, log zerolog.Logger) {
	buf := make([]byte, maxPacketSize)
	for {
		n, src, err := conn.ReadFromUDP(buf)
		if err != nil {
			log.Error().Err(err).Msg("Error reading from UDP")
			continue
		}

		packet := make([]byte, n)
		copy(packet, buf[:n])

		go handlePacket(packet, src, selfMAC, secret, db, log)
	}
}

func handlePacket(packet []byte, src *net.UDPAddr, selfMAC string, secret string, db *store.Store, log zerolog.Logger) {
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

	// Ignore beacons from self
	if payload.MACAddress == selfMAC {
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
