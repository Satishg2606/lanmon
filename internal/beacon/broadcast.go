package beacon

import (
	"fmt"
	"net"
	"time"

	"github.com/rs/zerolog"
	"github.com/vmihailenco/msgpack/v5"
	"golang.org/x/net/ipv4"

	"lanmon/internal/sysinfo"
)

// StartBeacon begins the periodic beacon broadcast loop.
func StartBeacon(ifaceName, multicastGroup string, serverAddress string, port int, interval time.Duration, sharedSecret string, log zerolog.Logger) error {
	var addrs []*net.UDPAddr

	// Resolve multicast address
	mAddr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("%s:%d", multicastGroup, port))
	if err != nil {
		return fmt.Errorf("resolving multicast address: %w", err)
	}
	addrs = append(addrs, mAddr)

	// Resolve optional unicast server address
	if serverAddress != "" {
		sAddr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("%s:%d", serverAddress, port))
		if err != nil {
			log.Warn().Err(err).Str("server_address", serverAddress).Msg("Failed to resolve unicast server address")
		} else {
			addrs = append(addrs, sAddr)
			log.Info().Str("server_address", serverAddress).Msg("Unicast discovery enabled")
		}
	}

	// Create a UDP connection
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return fmt.Errorf("listening for UDP: %w", err)
	}
	defer conn.Close()

	if ifaceName != "" {
		iface, err := net.InterfaceByName(ifaceName)
		if err != nil {
			return fmt.Errorf("finding interface %s: %w", ifaceName, err)
		}
		// ipv4.PacketConn is used for multicast control
		pc := ipv4.NewPacketConn(conn)
		if err := pc.SetMulticastInterface(iface); err != nil {
			log.Warn().Err(err).Msg("Failed to set multicast interface")
		}
		if err := pc.SetMulticastTTL(1); err != nil {
			log.Warn().Err(err).Msg("Failed to set multicast TTL")
		}
	}

	if err := conn.SetWriteBuffer(4096); err != nil {
		log.Warn().Err(err).Msg("Failed to set write buffer")
	}

	log.Info().
		Str("interface", ifaceName).
		Str("multicast_group", multicastGroup).
		Int("port", port).
		Dur("interval", interval).
		Msg("Beacon started")

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Helper to send to all targets
	broadcast := func() {
		for _, a := range addrs {
			if err := sendBeacon(conn, a, sharedSecret, log); err != nil {
				log.Error().Err(err).Str("target", a.String()).Msg("Failed to send beacon")
			}
		}
	}

	broadcast()
	for range ticker.C {
		broadcast()
	}

	return nil
}

func sendBeacon(conn *net.UDPConn, addr *net.UDPAddr, secret string, log zerolog.Logger) error {
	info, err := sysinfo.Collect("")
	if err != nil {
		return fmt.Errorf("collecting system info: %w", err)
	}

	payload := &BeaconPayload{
		Version:    1,
		Timestamp:  time.Now().Unix(),
		MACAddress: info.MACAddress,
		IPAddress:  info.IPAddress,
		Hostname:   info.Hostname,
		OS: OSInfo{
			Name:   info.OSName,
			Kernel: info.Kernel,
			Arch:   info.Arch,
		},
		Hardware: HWInfo{
			CPUModel:  info.CPUModel,
			CPUCores:  info.CPUCores,
			MemoryGB:  info.MemoryGB,
			DiskCount: info.DiskCount,
		},
	}

	data, err := msgpack.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling payload: %w", err)
	}

	hmacSig := ComputeHMAC(data, secret)
	packet := append(hmacSig, data...)

	_, err = conn.WriteToUDP(packet, addr)
	if err != nil {
		return fmt.Errorf("writing packet to %s: %w", addr, err)
	}

	log.Debug().
		Str("target", addr.String()).
		Str("hostname", payload.Hostname).
		Int("bytes", len(packet)).
		Msg("Beacon sent")

	return nil
}
