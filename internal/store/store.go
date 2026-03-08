// Package store provides a BoltDB-backed host record store for lanmon.
package store

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"
	bolt "go.etcd.io/bbolt"

	"lanmon/internal/beacon"
)

var hostsBucket = []byte("hosts")

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

// Store wraps a bbolt database for host records.
type Store struct {
	db  *bolt.DB
	mu  sync.RWMutex
	log zerolog.Logger
}

// New opens or creates a BoltDB file at the given path.
func New(path string, log zerolog.Logger) (*Store, error) {
	db, err := bolt.Open(path, 0600, &bolt.Options{Timeout: 5 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("opening database %s: %w", path, err)
	}

	// Ensure the hosts bucket exists
	err = db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(hostsBucket)
		return err
	})
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("creating hosts bucket: %w", err)
	}

	return &Store{db: db, log: log}, nil
}

// Close closes the underlying BoltDB.
func (s *Store) Close() error {
	return s.db.Close()
}

// Upsert inserts or updates a host record keyed by MAC address.
func (s *Store) Upsert(payload beacon.BeaconPayload) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(hostsBucket)
		key := []byte(payload.MACAddress)

		now := time.Now()
		var record HostRecord

		existing := b.Get(key)
		if existing != nil {
			if err := json.Unmarshal(existing, &record); err != nil {
				s.log.Warn().Err(err).Str("mac", payload.MACAddress).Msg("Failed to unmarshal existing record, overwriting")
			}
			record.Beacon = payload
			record.LastSeen = now
			record.PacketCount++
			record.Active = true
			record.PeerClusterMACs = payload.ClusterMACs

			s.log.Debug().
				Str("mac", payload.MACAddress).
				Str("hostname", payload.Hostname).
				Msg("Host updated")
		} else {
			record = HostRecord{
				Beacon:          payload,
				FirstSeen:       now,
				LastSeen:        now,
				PacketCount:     1,
				Active:          true,
				PeerClusterMACs: payload.ClusterMACs,
			}

			s.log.Info().
				Str("mac", payload.MACAddress).
				Str("hostname", payload.Hostname).
				Str("ip", payload.IPAddress).
				Str("os", payload.OS.Name).
				Msg("New host discovered")
		}

		data, err := json.Marshal(record)
		if err != nil {
			return fmt.Errorf("marshaling host record: %w", err)
		}

		return b.Put(key, data)
	})
}

// GetAll returns all host records.
func (s *Store) GetAll() ([]HostRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var records []HostRecord
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(hostsBucket)
		return b.ForEach(func(k, v []byte) error {
			var record HostRecord
			if err := json.Unmarshal(v, &record); err != nil {
				s.log.Warn().Err(err).Str("key", string(k)).Msg("Skipping corrupt record")
				return nil
			}
			records = append(records, record)
			return nil
		})
	})
	return records, err
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
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(hostsBucket)
		key := []byte(mac)

		existing := b.Get(key)
		if existing == nil {
			return fmt.Errorf("host %s not found", mac)
		}

		var record HostRecord
		if err := json.Unmarshal(existing, &record); err != nil {
			return fmt.Errorf("unmarshaling record: %w", err)
		}

		record.IsInCluster = inCluster
		if inCluster {
			record.Active = true // Ensure node is considered active if in cluster
		}

		data, err := json.Marshal(record)
		if err != nil {
			return fmt.Errorf("marshaling record: %w", err)
		}

		s.log.Info().
			Str("mac", mac).
			Str("hostname", record.Beacon.Hostname).
			Bool("in_cluster", inCluster).
			Msg("Cluster membership updated")

		return b.Put(key, data)
	})
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
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(hostsBucket)
		key := []byte(mac)

		existing := b.Get(key)
		if existing == nil {
			return fmt.Errorf("host %s not found", mac)
		}

		var record HostRecord
		if err := json.Unmarshal(existing, &record); err != nil {
			return fmt.Errorf("unmarshaling record: %w", err)
		}

		now := time.Now()
		record.SSHKeyPushed = true
		record.SSHKeyPushedAt = &now

		data, err := json.Marshal(record)
		if err != nil {
			return fmt.Errorf("marshaling record: %w", err)
		}

		s.log.Info().
			Str("mac", mac).
			Str("hostname", record.Beacon.Hostname).
			Msg("SSH key pushed")

		return b.Put(key, data)
	})
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
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-threshold)

	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(hostsBucket)
		return b.ForEach(func(k, v []byte) error {
			var record HostRecord
			if err := json.Unmarshal(v, &record); err != nil {
				return nil
			}

			if record.Active && record.LastSeen.Before(cutoff) {
				record.Active = false

				s.log.Info().
					Str("mac", record.Beacon.MACAddress).
					Str("hostname", record.Beacon.Hostname).
					Time("last_seen", record.LastSeen).
					Msg("Host marked inactive")

				data, err := json.Marshal(record)
				if err != nil {
					return nil
				}
				return b.Put(k, data)
			}
			return nil
		})
	})
	if err != nil {
		s.log.Error().Err(err).Msg("Database error during expiry check")
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
// Each active peer "votes" for the MACs it lists in PeerClusterMACs.
// A MAC is considered in the cluster if more than half of voters agree.
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
	s.mu.Lock()
	defer s.mu.Unlock()

	var removed []string

	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(hostsBucket)

		// Step 1: Collect all records and their votes
		var allRecords []HostRecord
		var allKeys [][]byte

		err := b.ForEach(func(k, v []byte) error {
			var record HostRecord
			if err := json.Unmarshal(v, &record); err != nil {
				return nil
			}
			allRecords = append(allRecords, record)
			keyCopy := make([]byte, len(k))
			copy(keyCopy, k)
			allKeys = append(allKeys, keyCopy)
			return nil
		})
		if err != nil {
			return err
		}

		// Step 2: Count votes — each active peer's PeerClusterMACs list is one vote
		// Also include the local node's own cluster list (its IsInCluster flags)
		votes := make(map[string]int) // MAC -> vote count
		voterCount := 0

		for _, r := range allRecords {
			if !r.Active {
				continue
			}
			// This peer's vote
			if len(r.PeerClusterMACs) > 0 {
				voterCount++
				for _, mac := range r.PeerClusterMACs {
					votes[mac]++
				}
			}
		}

		// The local node also votes (its own IsInCluster knowledge)
		// Collect the local cluster list as a vote
		localClusterMACs := []string{}
		for _, r := range allRecords {
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
			return nil
		}

		// Step 3: Approval Adoption — if any active peer (already HMAC-validated)
		// reports a MAC in its ClusterMACs, we automatically adopt it.
		// This ensures that a single "lanmon cluster add" propagates cluster-wide.
		trustedVotes := make(map[string]bool) // MACs vouched for by valid peers
		for _, r := range allRecords {
			// We trust any active peer because they must have the shared_secret to pass HMAC validation
			if !r.Active {
				continue
			}
			for _, mac := range r.PeerClusterMACs {
				trustedVotes[mac] = true
			}
		}

		// Build a lookup of existing records by MAC for quick access
		recordByMAC := make(map[string]int)
		for i, r := range allRecords {
			recordByMAC[r.Beacon.MACAddress] = i
		}

		// Adopt additions from peers. 
		// If WE are the one who just received the cluster approval from ANYONE who has the secret, we join.
		for mac := range trustedVotes {
			idx, exists := recordByMAC[mac]
			if !exists {
				continue 
			}
			if !allRecords[idx].IsInCluster {
				s.log.Info().
					Str("mac", mac).
					Str("hostname", allRecords[idx].Beacon.Hostname).
					Msg("Quorum: adopting cluster addition from secret-validated peer")
				allRecords[idx].IsInCluster = true
				allRecords[idx].Active = true // Force active if in cluster
				data, err := json.Marshal(allRecords[idx])
				if err != nil {
					continue
				}
				b.Put(allKeys[idx], data)
			}
		}

		// Note: We no longer perform automatic eviction (removal) based on majority vote.
		// Removals must be explicit via 'lanmon cluster remove', which then propagates via RPC push.

		return nil
	})

	if err != nil {
		s.log.Error().Err(err).Msg("Database error during quorum evaluation")
	}

	return removed
}
