// Package listener implements the UDP multicast receiver for lanmon server.
package listener

import (
	"fmt"
	"math"
	"net"
	"time"

	"github.com/rs/zerolog"
	"github.com/vmihailenco/msgpack/v5"

	"lanmon/internal/beacon"
	"lanmon/internal/store"
)

const (
	maxPacketSize    = 4096
	timestampMaxAge  = 60 // seconds
	maxPacketsPerMin = 5
)

// rateTracker tracks per-source-IP packet counts for rate limiting.
type rateTracker struct {
	counts    map[string]int
	resetTime time.Time
}

// StartListener joins the UDP multicast group and processes incoming beacon packets.
func StartListener(ifaceName, multicastGroup string, port int, sharedSecret string, db *store.Store, log zerolog.Logger) error {
	var iface *net.Interface
	if ifaceName != "" {
		var err error
		iface, err = net.InterfaceByName(ifaceName)
		if err != nil {
			return fmt.Errorf("finding interface %s: %w", ifaceName, err)
		}
	}

	group := net.ParseIP(multicastGroup)
	if group == nil {
		return fmt.Errorf("invalid multicast group: %s", multicastGroup)
	}

	listenAddr := &net.UDPAddr{
		IP:   group,
		Port: port,
	}

	conn, err := net.ListenMulticastUDP("udp4", iface, listenAddr)
	if err != nil {
		return fmt.Errorf("joining multicast group: %w", err)
	}
	defer conn.Close()

	if err := conn.SetReadBuffer(maxPacketSize * 10); err != nil {
		log.Warn().Err(err).Msg("Failed to set read buffer")
	}

	log.Info().
		Str("multicast_group", multicastGroup).
		Int("port", port).
		Msg("Listener started, waiting for beacons")

	tracker := &rateTracker{
		counts:    make(map[string]int),
		resetTime: time.Now().Add(time.Minute),
	}

	buf := make([]byte, maxPacketSize)
	for {
		n, src, err := conn.ReadFromUDP(buf)
		if err != nil {
			log.Error().Err(err).Msg("Error reading from UDP")
			continue
		}

		// Rate limiting
		now := time.Now()
		if now.After(tracker.resetTime) {
			tracker.counts = make(map[string]int)
			tracker.resetTime = now.Add(time.Minute)
		}
		srcIP := src.IP.String()
		tracker.counts[srcIP]++
		if tracker.counts[srcIP] > maxPacketsPerMin {
			log.Warn().Str("src_ip", srcIP).Msg("Rate limit exceeded, dropping packet")
			continue
		}

		// Copy packet data for goroutine
		packet := make([]byte, n)
		copy(packet, buf[:n])

		go handlePacket(packet, src, sharedSecret, db, log)
	}
}

func handlePacket(packet []byte, src *net.UDPAddr, secret string, db *store.Store, log zerolog.Logger) {
	srcIP := src.IP.String()

	log.Debug().
		Str("src_ip", srcIP).
		Int("payload_bytes", len(packet)).
		Msg("Packet received")

	// Minimum packet size: 32 bytes HMAC + at least 1 byte payload
	if len(packet) <= beacon.HMACSize {
		log.Warn().Str("src_ip", srcIP).Msg("Packet too small, discarding")
		return
	}

	// Extract HMAC signature and payload
	sig := packet[:beacon.HMACSize]
	data := packet[beacon.HMACSize:]

	// Verify HMAC
	if !beacon.VerifyHMAC(sig, data, secret) {
		log.Warn().
			Str("src_ip", srcIP).
			Str("reason", "HMAC mismatch").
			Msg("HMAC validation failed")
		return
	}

	// Deserialise MessagePack payload
	var payload beacon.BeaconPayload
	if err := msgpack.Unmarshal(data, &payload); err != nil {
		log.Warn().
			Str("src_ip", srcIP).
			Err(err).
			Msg("Failed to unmarshal payload")
		return
	}

	// Validate timestamp — reject packets older than 60 seconds
	now := time.Now().Unix()
	if math.Abs(float64(now-payload.Timestamp)) > timestampMaxAge {
		log.Warn().
			Str("src_ip", srcIP).
			Int64("packet_ts", payload.Timestamp).
			Int64("server_ts", now).
			Msg("Stale timestamp, discarding packet")
		return
	}

	// Upsert record into store
	if err := db.Upsert(payload); err != nil {
		log.Error().
			Str("mac", payload.MACAddress).
			Err(err).
			Msg("Database write error")
	}
}
