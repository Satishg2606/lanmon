package sysinfo

import (
	"testing"
)

func TestCollect(t *testing.T) {
	info, err := Collect()
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	if info == nil {
		t.Fatal("Collect returned nil")
	}

	// Hostname should always be available
	if info.Hostname == "" {
		t.Error("Hostname is empty")
	}

	// Arch should be set from runtime.GOARCH
	if info.Arch == "" {
		t.Error("Arch is empty")
	}

	// CPU cores should be > 0
	if info.CPUCores <= 0 {
		t.Errorf("CPUCores should be > 0, got %d", info.CPUCores)
	}

	// Memory should be > 0
	if info.MemoryGB <= 0 {
		t.Errorf("MemoryGB should be > 0, got %f", info.MemoryGB)
	}

	t.Logf("Collected: host=%s arch=%s cores=%d mem=%.1fGB mac=%s ip=%s os=%s kernel=%s disks=%d",
		info.Hostname, info.Arch, info.CPUCores, info.MemoryGB,
		info.MACAddress, info.IPAddress, info.OSName, info.Kernel, info.DiskCount)
}

func TestReadOSReleasePrettyName(t *testing.T) {
	name := readOSReleasePrettyName()
	// On Linux this should return something; on other platforms it may be empty
	t.Logf("PRETTY_NAME: %q", name)
}
