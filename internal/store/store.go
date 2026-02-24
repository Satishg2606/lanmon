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
	Beacon         beacon.BeaconPayload `json:"beacon"`
	FirstSeen      time.Time            `json:"first_seen"`
	LastSeen       time.Time            `json:"last_seen"`
	PacketCount    uint64               `json:"packet_count"`
	SSHKeyPushed   bool                 `json:"ssh_key_pushed"`
	SSHKeyPushedAt *time.Time           `json:"ssh_key_pushed_at,omitempty"`
	Active         bool                 `json:"active"`
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

			s.log.Debug().
				Str("mac", payload.MACAddress).
				Str("hostname", payload.Hostname).
				Msg("Host updated")
		} else {
			record = HostRecord{
				Beacon:      payload,
				FirstSeen:   now,
				LastSeen:    now,
				PacketCount: 1,
				Active:      true,
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
