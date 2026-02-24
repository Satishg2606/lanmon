package beacon

import (
	"fmt"
	"net"
	"time"

	"github.com/rs/zerolog"
	"github.com/vmihailenco/msgpack/v5"

	"lanmon/internal/sysinfo"
)

// StartBeacon begins the periodic beacon broadcast loop.
// It collects system info and sends HMAC-signed MessagePack-encoded packets
// to the configured UDP multicast group.
func StartBeacon(multicastGroup string, port int, interval time.Duration, sharedSecret string, log zerolog.Logger) error {
	addr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("%s:%d", multicastGroup, port))
	if err != nil {
		return fmt.Errorf("resolving multicast address: %w", err)
	}

	conn, err := net.DialUDP("udp4", nil, addr)
	if err != nil {
		return fmt.Errorf("dialing multicast: %w", err)
	}
	defer conn.Close()

	if err := conn.SetWriteBuffer(4096); err != nil {
		log.Warn().Err(err).Msg("Failed to set write buffer")
	}

	log.Info().
		Str("multicast_group", multicastGroup).
		Int("port", port).
		Dur("interval", interval).
		Msg("Beacon started")

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Send first beacon immediately
	if err := sendBeacon(conn, sharedSecret, log); err != nil {
		log.Error().Err(err).Msg("Failed to send initial beacon")
	}

	for range ticker.C {
		if err := sendBeacon(conn, sharedSecret, log); err != nil {
			log.Error().Err(err).Msg("Failed to send beacon")
		}
	}

	return nil
}

func sendBeacon(conn *net.UDPConn, secret string, log zerolog.Logger) error {
	info, err := sysinfo.Collect()
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

	_, err = conn.Write(packet)
	if err != nil {
		return fmt.Errorf("writing packet: %w", err)
	}

	log.Debug().
		Str("hostname", payload.Hostname).
		Str("ip", payload.IPAddress).
		Str("mac", payload.MACAddress).
		Int("payload_bytes", len(packet)).
		Msg("Beacon sent")

	return nil
}
