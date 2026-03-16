// Package store provides an rqlite-backed host record store for lanmon.
package store

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/rqlite/gorqlite"
	"github.com/rs/zerolog"

	"lanmon/internal/beacon"
)

// HostRecord represents a discovered host in the database.
type HostRecord struct {
	Beacon          beacon.BeaconPayload `json:"beacon"`
	FirstSeen       time.Time            `json:"first_seen"`
	LastSeen        time.Time            `json:"last_seen"`
	PacketCount     uint64               `json:"packet_count"`
	SSHKeyPushed    bool                 `json:"ssh_key_pushed"`
	SSHKeyPushedAt  *time.Time           `json:"ssh_key_pushed_at,omitempty"`
	Active          bool                 `json:"active"`
	IsInCluster     bool                 `json:"is_in_cluster"`
	PeerClusterMACs []string             `json:"peer_cluster_macs,omitempty"`
}

// Store wraps an rqlite connection for host records.
type Store struct {
	conn *gorqlite.Connection
	log  zerolog.Logger
}

const createTableSQL = `CREATE TABLE IF NOT EXISTS hosts (
	mac              TEXT PRIMARY KEY,
	ip               TEXT NOT NULL,
	hostname         TEXT NOT NULL,
	os_json          TEXT NOT NULL DEFAULT '{}',
	hw_json          TEXT NOT NULL DEFAULT '{}',
	cluster_macs     TEXT NOT NULL DEFAULT '[]',
	beacon_version   INTEGER NOT NULL DEFAULT 0,
	beacon_timestamp INTEGER NOT NULL DEFAULT 0,
	first_seen       TEXT NOT NULL,
	last_seen        TEXT NOT NULL,
	packet_count     INTEGER NOT NULL DEFAULT 0,
	ssh_key_pushed   INTEGER NOT NULL DEFAULT 0,
	ssh_key_pushed_at TEXT,
	active           INTEGER NOT NULL DEFAULT 1,
	is_in_cluster    INTEGER NOT NULL DEFAULT 0
)`

// New opens a connection to rqlite at the given URL and ensures the hosts table exists.
func New(rqliteURL string, log zerolog.Logger) (*Store, error) {
	conn, err := gorqlite.Open(rqliteURL)
	if err != nil {
		return nil, fmt.Errorf("connecting to rqlite at %s: %w", rqliteURL, err)
	}

	// Ensure the hosts table exists
	_, err = conn.WriteOne(createTableSQL)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("creating hosts table: %w", err)
	}

	return &Store{conn: conn, log: log}, nil
}

// Close closes the underlying rqlite connection.
func (s *Store) Close() error {
	s.conn.Close()
	return nil
}

// boolToInt converts a Go bool to an SQLite integer (0/1).
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Upsert inserts or updates a host record keyed by MAC address.
func (s *Store) Upsert(payload beacon.BeaconPayload) error {
	now := time.Now()

	// Check if the record already exists
	qr, err := s.conn.QueryOneParameterized(
		gorqlite.ParameterizedStatement{
			Query:     "SELECT packet_count, first_seen FROM hosts WHERE mac = ?",
			Arguments: []interface{}{payload.MACAddress},
		},
	)
	if err != nil {
		return fmt.Errorf("querying existing record: %w", err)
	}

	osJSON, err := json.Marshal(payload.OS)
	if err != nil {
		return fmt.Errorf("marshaling OS info: %w", err)
	}
	hwJSON, err := json.Marshal(payload.Hardware)
	if err != nil {
		return fmt.Errorf("marshaling HW info: %w", err)
	}
	clusterMACs, err := json.Marshal(payload.ClusterMACs)
	if err != nil {
		return fmt.Errorf("marshaling cluster MACs: %w", err)
	}
	// Ensure null slice serialization is "[]"
	if payload.ClusterMACs == nil {
		clusterMACs = []byte("[]")
	}

	nowStr := now.Format(time.RFC3339Nano)

	if qr.NumRows() > 0 && qr.Next() {
		// Existing record — update
		var packetCount int64
		var firstSeenStr string
		if err := qr.Scan(&packetCount, &firstSeenStr); err != nil {
			return fmt.Errorf("scanning existing record: %w", err)
		}

		packetCount++

		_, err = s.conn.WriteOneParameterized(
			gorqlite.ParameterizedStatement{
				Query: `UPDATE hosts SET
					ip = ?, hostname = ?, os_json = ?, hw_json = ?, cluster_macs = ?,
					beacon_version = ?, beacon_timestamp = ?,
					last_seen = ?, packet_count = ?, active = 1
					WHERE mac = ?`,
				Arguments: []interface{}{
					payload.IPAddress, payload.Hostname,
					string(osJSON), string(hwJSON), string(clusterMACs),
					payload.Version, payload.Timestamp,
					nowStr, packetCount,
					payload.MACAddress,
				},
			},
		)
		if err != nil {
			return fmt.Errorf("updating host record: %w", err)
		}

		s.log.Debug().
			Str("mac", payload.MACAddress).
			Str("hostname", payload.Hostname).
			Msg("Host updated")
	} else {
		// New record — insert
		_, err = s.conn.WriteOneParameterized(
			gorqlite.ParameterizedStatement{
				Query: `INSERT INTO hosts (mac, ip, hostname, os_json, hw_json, cluster_macs,
					beacon_version, beacon_timestamp, first_seen, last_seen,
					packet_count, ssh_key_pushed, active, is_in_cluster)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, 0, 1, 0)`,
				Arguments: []interface{}{
					payload.MACAddress, payload.IPAddress, payload.Hostname,
					string(osJSON), string(hwJSON), string(clusterMACs),
					payload.Version, payload.Timestamp,
					nowStr, nowStr,
				},
			},
		)
		if err != nil {
			return fmt.Errorf("inserting host record: %w", err)
		}

		s.log.Info().
			Str("mac", payload.MACAddress).
			Str("hostname", payload.Hostname).
			Str("ip", payload.IPAddress).
			Str("os", payload.OS.Name).
			Msg("New host discovered")
	}

	return nil
}

// scanRecord converts a QueryResult row into a HostRecord.
// The caller must have called Next() before calling this.
func scanRecord(qr gorqlite.QueryResult) (HostRecord, error) {
	var (
		mac, ip, hostname                    string
		osJSON, hwJSON, clusterMACs          string
		beaconVersion                        int64
		beaconTimestamp                      int64
		firstSeenStr, lastSeenStr            string
		packetCount                          int64
		sshKeyPushed, active, isInCluster    int64
		sshKeyPushedAtStr                    gorqlite.NullString
	)

	err := qr.Scan(
		&mac, &ip, &hostname,
		&osJSON, &hwJSON, &clusterMACs,
		&beaconVersion, &beaconTimestamp,
		&firstSeenStr, &lastSeenStr,
		&packetCount,
		&sshKeyPushed, &sshKeyPushedAtStr,
		&active, &isInCluster,
	)
	if err != nil {
		return HostRecord{}, fmt.Errorf("scanning row: %w", err)
	}

	var osInfo beacon.OSInfo
	if err := json.Unmarshal([]byte(osJSON), &osInfo); err != nil {
		return HostRecord{}, fmt.Errorf("unmarshaling os_json: %w", err)
	}
	var hwInfo beacon.HWInfo
	if err := json.Unmarshal([]byte(hwJSON), &hwInfo); err != nil {
		return HostRecord{}, fmt.Errorf("unmarshaling hw_json: %w", err)
	}
	var peerMACs []string
	if err := json.Unmarshal([]byte(clusterMACs), &peerMACs); err != nil {
		return HostRecord{}, fmt.Errorf("unmarshaling cluster_macs: %w", err)
	}

	firstSeen, _ := time.Parse(time.RFC3339Nano, firstSeenStr)
	lastSeen, _ := time.Parse(time.RFC3339Nano, lastSeenStr)

	record := HostRecord{
		Beacon: beacon.BeaconPayload{
			Version:     uint8(beaconVersion),
			Timestamp:   beaconTimestamp,
			MACAddress:  mac,
			IPAddress:   ip,
			Hostname:    hostname,
			OS:          osInfo,
			Hardware:    hwInfo,
			ClusterMACs: peerMACs,
		},
		FirstSeen:       firstSeen,
		LastSeen:        lastSeen,
		PacketCount:     uint64(packetCount),
		SSHKeyPushed:    sshKeyPushed != 0,
		Active:          active != 0,
		IsInCluster:     isInCluster != 0,
		PeerClusterMACs: peerMACs,
	}

	if sshKeyPushedAtStr.Valid {
		t, err := time.Parse(time.RFC3339Nano, sshKeyPushedAtStr.String)
		if err == nil {
			record.SSHKeyPushedAt = &t
		}
	}

	return record, nil
}

// GetAll returns all host records.
func (s *Store) GetAll() ([]HostRecord, error) {
	qr, err := s.conn.QueryOne(
		"SELECT mac, ip, hostname, os_json, hw_json, cluster_macs, beacon_version, beacon_timestamp, first_seen, last_seen, packet_count, ssh_key_pushed, ssh_key_pushed_at, active, is_in_cluster FROM hosts",
	)
	if err != nil {
		return nil, fmt.Errorf("querying all hosts: %w", err)
	}

	var records []HostRecord
	for qr.Next() {
		record, err := scanRecord(qr)
		if err != nil {
			s.log.Warn().Err(err).Msg("Skipping corrupt record")
			continue
		}
		records = append(records, record)
	}
	return records, nil
}

// GetActive returns only active host records.
func (s *Store) GetActive() ([]HostRecord, error) {
	all, err := s.GetAll()
	if err != nil {
		return nil, err
	}

	var active []HostRecord
	for _, r := range all {
		if r.Active {
			active = append(active, r)
		}
	}
	return active, nil
}

// MarkClusterNode marks or unmarks a host as a cluster member.
func (s *Store) MarkClusterNode(mac string, inCluster bool) error {
	activeVal := 0
	if inCluster {
		activeVal = 1
	}

	_, err := s.conn.WriteOneParameterized(
		gorqlite.ParameterizedStatement{
			Query:     "UPDATE hosts SET is_in_cluster = ?, active = CASE WHEN ? = 1 THEN 1 ELSE active END WHERE mac = ?",
			Arguments: []interface{}{boolToInt(inCluster), activeVal, mac},
		},
	)
	if err != nil {
		return fmt.Errorf("updating cluster membership for %s: %w", mac, err)
	}

	// Verify the row existed
	qr, err := s.conn.QueryOneParameterized(
		gorqlite.ParameterizedStatement{
			Query:     "SELECT hostname FROM hosts WHERE mac = ?",
			Arguments: []interface{}{mac},
		},
	)
	if err != nil {
		return err
	}
	if qr.NumRows() == 0 {
		return fmt.Errorf("host %s not found", mac)
	}

	var hostname string
	if qr.Next() {
		qr.Scan(&hostname)
	}

	s.log.Info().
		Str("mac", mac).
		Str("hostname", hostname).
		Bool("in_cluster", inCluster).
		Msg("Cluster membership updated")

	return nil
}

// GetClusterNodes returns only nodes marked as cluster members.
func (s *Store) GetClusterNodes() ([]HostRecord, error) {
	all, err := s.GetAll()
	if err != nil {
		return nil, err
	}

	var clusterNodes []HostRecord
	for _, r := range all {
		if r.IsInCluster {
			clusterNodes = append(clusterNodes, r)
		}
	}
	return clusterNodes, nil
}

// MarkKeyPushed marks a host's SSH key as pushed.
func (s *Store) MarkKeyPushed(mac string) error {
	// Verify the row exists first
	qr, err := s.conn.QueryOneParameterized(
		gorqlite.ParameterizedStatement{
			Query:     "SELECT hostname FROM hosts WHERE mac = ?",
			Arguments: []interface{}{mac},
		},
	)
	if err != nil {
		return err
	}
	if qr.NumRows() == 0 {
		return fmt.Errorf("host %s not found", mac)
	}

	var hostname string
	if qr.Next() {
		qr.Scan(&hostname)
	}

	now := time.Now().Format(time.RFC3339Nano)
	_, err = s.conn.WriteOneParameterized(
		gorqlite.ParameterizedStatement{
			Query:     "UPDATE hosts SET ssh_key_pushed = 1, ssh_key_pushed_at = ? WHERE mac = ?",
			Arguments: []interface{}{now, mac},
		},
	)
	if err != nil {
		return fmt.Errorf("marking key pushed for %s: %w", mac, err)
	}

	s.log.Info().
		Str("mac", mac).
		Str("hostname", hostname).
		Msg("SSH key pushed")

	return nil
}

// RunExpiry starts a background goroutine that marks hosts as inactive
// if their LastSeen exceeds the given threshold. Runs at the given check interval.
func (s *Store) RunExpiry(checkInterval, threshold time.Duration) {
	go func() {
		ticker := time.NewTicker(checkInterval)
		defer ticker.Stop()

		for range ticker.C {
			s.expireStaleHosts(threshold)
		}
	}()
}

func (s *Store) expireStaleHosts(threshold time.Duration) {
	cutoff := time.Now().Add(-threshold).Format(time.RFC3339Nano)

	// Get hosts that will be marked inactive (for logging)
	qr, err := s.conn.QueryOneParameterized(
		gorqlite.ParameterizedStatement{
			Query:     "SELECT mac, hostname, last_seen FROM hosts WHERE active = 1 AND last_seen < ?",
			Arguments: []interface{}{cutoff},
		},
	)
	if err != nil {
		s.log.Error().Err(err).Msg("Database error during expiry check (query)")
		return
	}

	for qr.Next() {
		var mac, hostname, lastSeenStr string
		if err := qr.Scan(&mac, &hostname, &lastSeenStr); err != nil {
			continue
		}
		lastSeen, _ := time.Parse(time.RFC3339Nano, lastSeenStr)
		s.log.Info().
			Str("mac", mac).
			Str("hostname", hostname).
			Time("last_seen", lastSeen).
			Msg("Host marked inactive")
	}

	// Mark them inactive
	_, err = s.conn.WriteOneParameterized(
		gorqlite.ParameterizedStatement{
			Query:     "UPDATE hosts SET active = 0 WHERE active = 1 AND last_seen < ?",
			Arguments: []interface{}{cutoff},
		},
	)
	if err != nil {
		s.log.Error().Err(err).Msg("Database error during expiry check (update)")
	}
}

// GetClusterMACs returns a list of MAC addresses of all nodes marked as cluster members.
func (s *Store) GetClusterMACs() ([]string, error) {
	nodes, err := s.GetClusterNodes()
	if err != nil {
		return nil, err
	}
	var macs []string
	for _, n := range nodes {
		macs = append(macs, n.Beacon.MACAddress)
	}
	return macs, nil
}

// RunQuorum starts a background goroutine that runs every `interval` to evaluate
// cluster membership based on what active peers report in their beacons.
// Returns a channel of removed MACs for external cleanup.
func (s *Store) RunQuorum(interval time.Duration) <-chan []string {
	removedCh := make(chan []string, 1)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			removed := s.evaluateQuorum()
			if len(removed) > 0 {
				// Non-blocking send of removed MACs
				select {
				case removedCh <- removed:
				default:
				}
			}
		}
	}()
	return removedCh
}

// evaluateQuorum counts votes from all active peers and the local node,
// then updates IsInCluster flags. Returns MACs that were removed from cluster.
func (s *Store) evaluateQuorum() []string {
	var removed []string

	all, err := s.GetAll()
	if err != nil {
		s.log.Error().Err(err).Msg("Database error during quorum evaluation (get all)")
		return removed
	}

	// Step 1: Count votes — each active peer's PeerClusterMACs list is one vote
	votes := make(map[string]int)
	voterCount := 0

	for _, r := range all {
		if !r.Active {
			continue
		}
		if len(r.PeerClusterMACs) > 0 {
			voterCount++
			for _, mac := range r.PeerClusterMACs {
				votes[mac]++
			}
		}
	}

	// The local node also votes (its own IsInCluster knowledge)
	localClusterMACs := []string{}
	for _, r := range all {
		if r.IsInCluster {
			localClusterMACs = append(localClusterMACs, r.Beacon.MACAddress)
		}
	}
	if len(localClusterMACs) > 0 {
		voterCount++
		for _, mac := range localClusterMACs {
			votes[mac]++
		}
	}

	// If no voters, nothing to do
	if voterCount == 0 {
		return removed
	}

	// Step 2: Approval Adoption — if any active peer (already HMAC-validated)
	// reports a MAC in its ClusterMACs, we automatically adopt it.
	trustedVotes := make(map[string]bool)
	for _, r := range all {
		if !r.Active {
			continue
		}
		for _, mac := range r.PeerClusterMACs {
			trustedVotes[mac] = true
		}
	}

	// Build a lookup of existing records by MAC
	recordByMAC := make(map[string]int)
	for i, r := range all {
		recordByMAC[r.Beacon.MACAddress] = i
	}

	// Adopt additions from peers
	for mac := range trustedVotes {
		idx, exists := recordByMAC[mac]
		if !exists {
			continue
		}
		if !all[idx].IsInCluster {
			s.log.Info().
				Str("mac", mac).
				Str("hostname", all[idx].Beacon.Hostname).
				Msg("Quorum: adopting cluster addition from secret-validated peer")

			s.conn.WriteOneParameterized(
				gorqlite.ParameterizedStatement{
					Query:     "UPDATE hosts SET is_in_cluster = 1, active = 1 WHERE mac = ?",
					Arguments: []interface{}{mac},
				},
			)
		}
	}

	return removed
}
