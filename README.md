# lanmon: LAN Discovery & SSH Key Exchange System

`lanmon` is a lightweight, LAN-hosted system designed for automatic discovery of machines on a local network, collection of hardware/system metadata, and seamless passwordless SSH key distribution from a central management server to discovered hosts.

Built with **Go**, it requires no complex orchestration or cloud dependencies, operating entirely within the LAN broadcast domain using LLDP-inspired UDP multicast.

---

## 🚀 Features

- **Zero-Install Client**: A single static binary with no runtime dependencies.
- **Automatic Discovery**: LLDP-inspired UDP multicast beacons (L3) with HMAC-SHA256 security.
- **Hardware Metadata**: Rich collection of system info (OS, Kernel, CPU, RAM, Disks, MAC/IP).
- **Secure by Design**: HMAC-protected beacons, timestamp anti-replay, and rate limiting.
- **BoltDB Storage**: High-performance, embedded, ACID-compliant state storage for the server.
- **Interactive SSH Pushing**: CLI-based host selection for pushing SSH public keys with TOFU (Trust On First Use) host key verification.
- **Hardened Deployment**: Comprehensive systemd units and installation scripts included.

---

## 🏗 Architecture

The system consists of three services bundled in a single binary:

1.  **LANBeacon (`lanmon agent`)**: Runs on every managed host. Broadcasts system metadata.
2.  **LANListener (`lanmon server`)**: Runs on the management server. Captures beacons, stores records in BoltDB, and exposes a Unix socket RPC.
3.  **LANConnect (`lanmon connect`)**: Interactive CLI to list discovered hosts and push the server's SSH public key.

---

## 🛠 Installation

### Prerequisites
- **Go 1.22+** (for building from source)
- **Linux** (Target platform for agent/server services)

### Build
```bash
git clone <repository-url> lanmon
cd lanmon
make build
```
The binary will be generated at `./bin/lanmon`.

### Systematic Deployment
```bash
# Generate a shared secret for HMAC authentication
./scripts/keygen.sh

# Install on a Management Server
sudo ./scripts/install.sh server

# Install on a Managed Host (Agent)
sudo ./scripts/install.sh agent
```

---

## ⚙️ Configuration

Configurations are stored in `/etc/lanmon/config.toml`. Both the agent and server **must** share the same `shared_secret`.

### Example Agent Config
```toml
[agent]
  interface = "eth0"
  shared_secret = "YOUR_64_CHAR_HEX_SECRET"
  interval = "30s"
```

### Example Server Config
```toml
[server]
  interface = "eth0"
  shared_secret = "YOUR_64_CHAR_HEX_SECRET"
  db_path = "/var/lib/lanmon/hosts.db"
  stale_threshold = "90s"
```

---

## 📖 Usage

### Running the Agent
```bash
sudo lanmon agent
```

### Running the Server
```bash
sudo lanmon server
```

### Connecting to Hosts
Launch the interactive CLI to push your SSH key to a discovered host:
```bash
sudo lanmon connect
```
*Note: This will list all active hosts, prompt for a selection, target user, and password to perform the initial key injection.*

---

## 🧪 Testing

`lanmon` includes a comprehensive test suite with race detection.

```bash
make test
```

---

## 🔒 Security Considerations

- **HMAC-SHA256**: All UDP beacons are signed; unsigned or incorrectly signed packets are silently discarded.
- **Anti-Replay**: Packets with timestamps older than 60 seconds are rejected.
- **Strict SSH**: Host key verification is enforced. New hosts use the TOFU model, while changed host keys trigger an alert.
- **Least Privilege**: The systemd units are hardened with `ProtectSystem`, `ProtectHome`, and limited capabilities.

---

## 📦 Dependencies

| Package | Purpose |
|---------|---------|
| `bbolt` | Embedded ACID database |
| `msgpack` | Compact binary serialization |
| `gopsutil` | System/Hardware introspection |
| `x/crypto/ssh` | Secure SSH key distribution |
| `zerolog` | Structured JSON logging |

---

## 📄 License
This project uses several open-source components under MIT and BSD licenses. Please see individual dependency repositories for details.
