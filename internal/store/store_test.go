package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"lanmon/internal/beacon"
)

func testLogger() zerolog.Logger {
	return zerolog.Nop()
}

func testStore(t *testing.T) (*Store, func()) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, err := New(dbPath, testLogger())
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	return s, func() {
		s.Close()
		os.Remove(dbPath)
	}
}

func samplePayload(mac, hostname, ip string) beacon.BeaconPayload {
	return beacon.BeaconPayload{
		Version:    1,
		Timestamp:  time.Now().Unix(),
		MACAddress: mac,
		IPAddress:  ip,
		Hostname:   hostname,
		OS: beacon.OSInfo{
			Name:   "Ubuntu 22.04",
			Kernel: "5.15.0",
			Arch:   "amd64",
		},
		Hardware: beacon.HWInfo{
			CPUModel:  "Test CPU",
			CPUCores:  4,
			MemoryGB:  16.0,
			DiskCount: 1,
		},
	}
}

func TestStore_UpsertAndGetAll(t *testing.T) {
	s, cleanup := testStore(t)
	defer cleanup()

	payload := samplePayload("aa:bb:cc:dd:ee:ff", "host1", "192.168.1.10")

	if err := s.Upsert(payload); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}

	records, err := s.GetAll()
	if err != nil {
		t.Fatalf("getall failed: %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	r := records[0]
	if r.Beacon.MACAddress != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("MAC: got %s, want aa:bb:cc:dd:ee:ff", r.Beacon.MACAddress)
	}
	if r.Beacon.Hostname != "host1" {
		t.Errorf("Hostname: got %s, want host1", r.Beacon.Hostname)
	}
	if r.PacketCount != 1 {
		t.Errorf("PacketCount: got %d, want 1", r.PacketCount)
	}
	if !r.Active {
		t.Error("expected host to be active")
	}
}

func TestStore_UpsertIncrementsPacketCount(t *testing.T) {
	s, cleanup := testStore(t)
	defer cleanup()

	payload := samplePayload("aa:bb:cc:dd:ee:ff", "host1", "192.168.1.10")

	for i := 0; i < 5; i++ {
		if err := s.Upsert(payload); err != nil {
			t.Fatalf("upsert %d failed: %v", i, err)
		}
	}

	records, err := s.GetAll()
	if err != nil {
		t.Fatalf("getall failed: %v", err)
	}

	if records[0].PacketCount != 5 {
		t.Errorf("PacketCount: got %d, want 5", records[0].PacketCount)
	}
}

func TestStore_MultipleHosts(t *testing.T) {
	s, cleanup := testStore(t)
	defer cleanup()

	s.Upsert(samplePayload("aa:bb:cc:dd:ee:01", "host1", "192.168.1.1"))
	s.Upsert(samplePayload("aa:bb:cc:dd:ee:02", "host2", "192.168.1.2"))
	s.Upsert(samplePayload("aa:bb:cc:dd:ee:03", "host3", "192.168.1.3"))

	records, err := s.GetAll()
	if err != nil {
		t.Fatalf("getall failed: %v", err)
	}

	if len(records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(records))
	}
}

func TestStore_GetActive(t *testing.T) {
	s, cleanup := testStore(t)
	defer cleanup()

	s.Upsert(samplePayload("aa:bb:cc:dd:ee:01", "host1", "192.168.1.1"))
	s.Upsert(samplePayload("aa:bb:cc:dd:ee:02", "host2", "192.168.1.2"))

	active, err := s.GetActive()
	if err != nil {
		t.Fatalf("getactive failed: %v", err)
	}

	if len(active) != 2 {
		t.Fatalf("expected 2 active, got %d", len(active))
	}
}

func TestStore_MarkKeyPushed(t *testing.T) {
	s, cleanup := testStore(t)
	defer cleanup()

	mac := "aa:bb:cc:dd:ee:ff"
	s.Upsert(samplePayload(mac, "host1", "192.168.1.10"))

	if err := s.MarkKeyPushed(mac); err != nil {
		t.Fatalf("mark key pushed failed: %v", err)
	}

	records, err := s.GetAll()
	if err != nil {
		t.Fatalf("getall failed: %v", err)
	}

	if !records[0].SSHKeyPushed {
		t.Error("expected SSHKeyPushed to be true")
	}
	if records[0].SSHKeyPushedAt == nil {
		t.Error("expected SSHKeyPushedAt to be set")
	}
}

func TestStore_MarkKeyPushed_NotFound(t *testing.T) {
	s, cleanup := testStore(t)
	defer cleanup()

	if err := s.MarkKeyPushed("nonexistent"); err == nil {
		t.Error("expected error for nonexistent MAC")
	}
}

func TestStore_Expiry(t *testing.T) {
	s, cleanup := testStore(t)
	defer cleanup()

	s.Upsert(samplePayload("aa:bb:cc:dd:ee:ff", "host1", "192.168.1.10"))

	// Force expiry with a threshold of 0 (expires everything)
	s.expireStaleHosts(0)

	records, err := s.GetAll()
	if err != nil {
		t.Fatalf("getall failed: %v", err)
	}

	if records[0].Active {
		t.Error("expected host to be inactive after expiry")
	}
}
