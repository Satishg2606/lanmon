// Package sysinfo collects system metadata for the beacon payload.
package sysinfo

import (
	"fmt"
	"math"
	"net"
	"os"
	"runtime"
	"strings"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
)

// SystemInfo holds all collected system information.
// This is kept separate from BeaconPayload to avoid circular imports.
type SystemInfo struct {
	MACAddress string
	IPAddress  string
	Hostname   string
	OSName     string
	Kernel     string
	Arch       string
	CPUModel   string
	CPUCores   int
	MemoryGB   float64
	DiskCount  int
}

// Collect gathers local system information for an interface matching the provided network range.
// If networkRange is empty, it falls back to the first non-loopback interface.
func Collect(networkRange string) (*SystemInfo, error) {
	macAddr, ipAddr, err := getNetworkInfo(networkRange)
	if err != nil {
		return nil, err
	}

	hostname, _ := os.Hostname()
	osName, kernel := getOSInfo()

	info := &SystemInfo{
		MACAddress: macAddr,
		IPAddress:  ipAddr,
		Hostname:   hostname,
		OSName:     osName,
		Kernel:     kernel,
		Arch:       runtime.GOARCH,
		CPUCores:   runtime.NumCPU(),
	}

	// CPU model
	cpuInfo, err := cpu.Info()
	if err == nil && len(cpuInfo) > 0 {
		info.CPUModel = cpuInfo[0].ModelName
	}

	// Memory
	memInfo, err := mem.VirtualMemory()
	if err == nil {
		info.MemoryGB = math.Round(float64(memInfo.Total)/(1024*1024*1024)*100) / 100
	}

	// Disk count
	partitions, err := disk.Partitions(false)
	if err == nil {
		info.DiskCount = len(partitions)
	}

	return info, nil
}

// getNetworkInfo returns the MAC and IPv4 address of an interface.
// If networkRange is provided (CIDR), it finds an interface matching that range.
// Otherwise, it returns the first non-loopback interface.
func getNetworkInfo(networkRange string) (string, string, error) {
	var targetNet *net.IPNet
	if networkRange != "" {
		_, tn, err := net.ParseCIDR(networkRange)
		if err != nil {
			return "", "", fmt.Errorf("parsing network range %s: %w", networkRange, err)
		}
		targetNet = tn
	}

	ifaces, err := net.Interfaces()
	if err != nil {
		return "", "", err
	}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		if iface.HardwareAddr == nil || len(iface.HardwareAddr) == 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipNet.IP.To4()
			if ip == nil {
				continue
			}

			// If target network provided, check if IP fits
			if targetNet != nil {
				if targetNet.Contains(ip) {
					return iface.HardwareAddr.String(), ip.String(), nil
				}
				continue
			}

			// No target network, return first non-loopback IPv4
			return iface.HardwareAddr.String(), ip.String(), nil
		}
	}

	if networkRange != "" {
		return "", "", fmt.Errorf("no interface found matching network range %s", networkRange)
	}
	return "", "", fmt.Errorf("no suitable network interface found")
}


// getOSInfo retrieves OS name and kernel version.
func getOSInfo() (string, string) {
	var osName, kernel string

	hostInfo, err := host.Info()
	if err == nil {
		osName = hostInfo.Platform
		if hostInfo.PlatformVersion != "" {
			osName += " " + hostInfo.PlatformVersion
		}
		kernel = hostInfo.KernelVersion
	} else {
		osName = runtime.GOOS
	}

	if runtime.GOOS == "linux" {
		if prettyName := readOSReleasePrettyName(); prettyName != "" {
			osName = prettyName
		}
	}

	return osName, kernel
}

// readOSReleasePrettyName parses /etc/os-release for the PRETTY_NAME field.
func readOSReleasePrettyName() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			val := strings.TrimPrefix(line, "PRETTY_NAME=")
			val = strings.Trim(val, "\"")
			return val
		}
	}
	return ""
}
