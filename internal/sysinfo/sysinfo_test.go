package sysinfo

import (
	"net"
	"testing"
)

func TestCollect(t *testing.T) {
	info, err := Collect("")
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

	t.Logf("Collected default: host=%s ip=%s", info.Hostname, info.IPAddress)
}

func TestCollect_WithNetworkRange(t *testing.T) {
	// First get any IP to know what range to test with
	info, err := Collect("")
	if err != nil {
		t.Skip("skipping network range test: no interface found")
	}

	// Try to detect same interface using CIDR
	// Example: if IP is 192.168.1.5, use 192.168.0.0/16
	ip := net.ParseIP(info.IPAddress)
	if ip == nil {
		t.Fatalf("invalid IP collected: %s", info.IPAddress)
	}

	// Determine a range that likely contains this IP
	cidr := ""
	if ip.To4() != nil {
		cidr = ip.Mask(net.CIDRMask(16, 32)).String() + "/16"
	} else {
		cidr = ip.Mask(net.CIDRMask(64, 128)).String() + "/64"
	}

	t.Logf("Testing with CIDR: %s", cidr)
	info2, err := Collect(cidr)
	if err != nil {
		t.Fatalf("Collect with CIDR %s failed: %v", cidr, err)
	}

	if info2.IPAddress != info.IPAddress {
		t.Errorf("Mismatch with CIDR: got %s, want %s", info2.IPAddress, info.IPAddress)
	}
}

func TestReadOSReleasePrettyName(t *testing.T) {
	name := readOSReleasePrettyName()
	t.Logf("PRETTY_NAME: %q", name)
}

