// Package listener implements the UDP multicast receiver for lanmon server.
package listener

import (
	"fmt"
	"math"
	"net"
	"time"

	"github.com/rs/zerolog"
	"github.com/vmihailenco/msgpack/v5"
	"golang.org/x/net/ipv4"

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
	group := net.ParseIP(multicastGroup)
	if group == nil {
		return fmt.Errorf("invalid multicast group: %s", multicastGroup)
	}

	// Resolve the interface
	var iface *net.Interface
	if ifaceName != "" {
		var err error
		iface, err = net.InterfaceByName(ifaceName)
		if err != nil {
			return fmt.Errorf("finding interface %s: %w", ifaceName, err)
		}
	}

	// Bind to the wildcard address (0.0.0.0) on the specified port.
	// This allows receiving both unicast and multicast packets.
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{
		IP:   net.IPv4zero,
		Port: port,
	})
	if err != nil {
		return fmt.Errorf("listening on UDP: %w", err)
	}
	defer conn.Close()

	// If a multicast group is specified, join it on the given interface.
	if group.IsMulticast() {
		p := ipv4.NewPacketConn(conn)
		if err := p.JoinGroup(iface, &net.UDPAddr{IP: group}); err != nil {
			return fmt.Errorf("joining multicast group: %w", err)
		}
	}

	if err := conn.SetReadBuffer(maxPacketSize * 10); err != nil {
		log.Warn().Err(err).Msg("Failed to set read buffer")
	}

	log.Info().
		Str("interface", ifaceName).
		Str("multicast_group", multicastGroup).
		Int("port", port).
		Msg("Listener started, waiting for beacons")

	buf := make([]byte, maxPacketSize)
	for {
		n, src, err := conn.ReadFromUDP(buf)
		if err != nil {
			log.Error().Err(err).Msg("Error reading from UDP")
			continue
		}

		log.Info().
			Str("src", src.String()).
			Int("bytes", n).
			Msg("Packet received")

		packet := make([]byte, n)
		copy(packet, buf[:n])

		go handlePacket(packet, src, sharedSecret, db, log)
	}
}

func handlePacket(packet []byte, src *net.UDPAddr, secret string, db *store.Store, log zerolog.Logger) {
	srcAddr := src.String()

	if len(packet) <= beacon.HMACSize {
		log.Warn().Str("src", srcAddr).Msg("Packet too small")
		return
	}

	sig := packet[:beacon.HMACSize]
	data := packet[beacon.HMACSize:]

	if !beacon.VerifyHMAC(sig, data, secret) {
		log.Warn().
			Str("src", srcAddr).
			Msg("HMAC validation failed")
		return
	}

	var payload beacon.BeaconPayload
	if err := msgpack.Unmarshal(data, &payload); err != nil {
		log.Error().Err(err).Str("src", srcAddr).Msg("Failed to unmarshal beacon")
		return
	}

	now := time.Now().Unix()
	if math.Abs(float64(now-payload.Timestamp)) > timestampMaxAge {
		log.Warn().
			Str("src", srcAddr).
			Int64("payload_ts", payload.Timestamp).
			Int64("server_ts", now).
			Msg("Stale timestamp")
		return
	}

	log.Info().
		Str("hostname", payload.Hostname).
		Str("ip", payload.IPAddress).
		Msg("New host discovered")

	if err := db.Upsert(payload); err != nil {
		log.Error().Err(err).Msg("Database write error")
	}
}
