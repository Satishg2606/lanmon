// Package beacon defines the beacon payload structures and broadcast logic.
package beacon

// BeaconPayload is the data broadcast by each agent over UDP multicast.
type BeaconPayload struct {
	Version    uint8  `msgpack:"version"`
	Timestamp  int64  `msgpack:"timestamp"`
	MACAddress string `msgpack:"mac_address"`
	IPAddress  string `msgpack:"ip_address"`
	Hostname   string `msgpack:"hostname"`
	OS         OSInfo `msgpack:"os"`
	Hardware   HWInfo `msgpack:"hardware"`
}

// OSInfo holds operating system metadata.
type OSInfo struct {
	Name   string `msgpack:"name"`
	Kernel string `msgpack:"kernel"`
	Arch   string `msgpack:"arch"`
}

// HWInfo holds hardware metadata.
type HWInfo struct {
	CPUModel  string  `msgpack:"cpu_model"`
	CPUCores  int     `msgpack:"cpu_cores"`
	MemoryGB  float64 `msgpack:"memory_gb"`
	DiskCount int     `msgpack:"disk_count"`
}
