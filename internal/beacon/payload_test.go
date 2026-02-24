package beacon

import (
	"testing"

	"github.com/vmihailenco/msgpack/v5"
)

func TestBeaconPayload_MsgpackRoundTrip(t *testing.T) {
	original := BeaconPayload{
		Version:    1,
		Timestamp:  1708444800,
		MACAddress: "aa:bb:cc:dd:ee:ff",
		IPAddress:  "192.168.1.100",
		Hostname:   "test-host",
		OS: OSInfo{
			Name:   "Ubuntu 22.04.3 LTS",
			Kernel: "5.15.0-91-generic",
			Arch:   "amd64",
		},
		Hardware: HWInfo{
			CPUModel:  "Intel Core i7-12700",
			CPUCores:  20,
			MemoryGB:  31.85,
			DiskCount: 2,
		},
	}

	// Marshal
	data, err := msgpack.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	if len(data) == 0 {
		t.Fatal("marshaled data is empty")
	}

	// Unmarshal
	var decoded BeaconPayload
	if err := msgpack.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	// Verify all fields
	if decoded.Version != original.Version {
		t.Errorf("Version: got %d, want %d", decoded.Version, original.Version)
	}
	if decoded.Timestamp != original.Timestamp {
		t.Errorf("Timestamp: got %d, want %d", decoded.Timestamp, original.Timestamp)
	}
	if decoded.MACAddress != original.MACAddress {
		t.Errorf("MACAddress: got %s, want %s", decoded.MACAddress, original.MACAddress)
	}
	if decoded.IPAddress != original.IPAddress {
		t.Errorf("IPAddress: got %s, want %s", decoded.IPAddress, original.IPAddress)
	}
	if decoded.Hostname != original.Hostname {
		t.Errorf("Hostname: got %s, want %s", decoded.Hostname, original.Hostname)
	}
	if decoded.OS.Name != original.OS.Name {
		t.Errorf("OS.Name: got %s, want %s", decoded.OS.Name, original.OS.Name)
	}
	if decoded.OS.Kernel != original.OS.Kernel {
		t.Errorf("OS.Kernel: got %s, want %s", decoded.OS.Kernel, original.OS.Kernel)
	}
	if decoded.OS.Arch != original.OS.Arch {
		t.Errorf("OS.Arch: got %s, want %s", decoded.OS.Arch, original.OS.Arch)
	}
	if decoded.Hardware.CPUModel != original.Hardware.CPUModel {
		t.Errorf("CPUModel: got %s, want %s", decoded.Hardware.CPUModel, original.Hardware.CPUModel)
	}
	if decoded.Hardware.CPUCores != original.Hardware.CPUCores {
		t.Errorf("CPUCores: got %d, want %d", decoded.Hardware.CPUCores, original.Hardware.CPUCores)
	}
	if decoded.Hardware.MemoryGB != original.Hardware.MemoryGB {
		t.Errorf("MemoryGB: got %f, want %f", decoded.Hardware.MemoryGB, original.Hardware.MemoryGB)
	}
	if decoded.Hardware.DiskCount != original.Hardware.DiskCount {
		t.Errorf("DiskCount: got %d, want %d", decoded.Hardware.DiskCount, original.Hardware.DiskCount)
	}
}

func TestBeaconPayload_SignedPacketRoundTrip(t *testing.T) {
	payload := BeaconPayload{
		Version:    1,
		Timestamp:  1708444800,
		MACAddress: "aa:bb:cc:dd:ee:ff",
		IPAddress:  "192.168.1.100",
		Hostname:   "test-host",
	}

	secret := "test-shared-secret"

	// Simulate what the agent does: marshal + sign
	data, err := msgpack.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	hmacSig := ComputeHMAC(data, secret)
	packet := append(hmacSig, data...)

	// Simulate what the listener does: extract sig + verify + unmarshal
	if len(packet) <= HMACSize {
		t.Fatal("packet too small")
	}

	sig := packet[:HMACSize]
	payloadData := packet[HMACSize:]

	if !VerifyHMAC(sig, payloadData, secret) {
		t.Fatal("HMAC verification failed")
	}

	var decoded BeaconPayload
	if err := msgpack.Unmarshal(payloadData, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.Hostname != "test-host" {
		t.Errorf("Hostname: got %s, want test-host", decoded.Hostname)
	}
	if decoded.MACAddress != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("MACAddress: got %s, want aa:bb:cc:dd:ee:ff", decoded.MACAddress)
	}
}
