go build -o bin/lanmon main.go
[System_Workflow]
Unified node that does both broadcasting and listening:
Broadcast: Periodically send beacon to the subnet's broadcast address (e.g. 10.51.241.255:5678 for 10.51.240.0/23), using standard UDP broadcast (not multicast)
Listen: Bind to 0.0.0.0:<port> and accept incoming beacons from any peer
Store: Upsert received beacons into local BoltDB (reuse existing store package)
Ignores beacons from self (by MAC address)
┌─────────────────┐         UDP broadcast          ┌─────────────────┐
│  Node A          │ ───────────────────────────────▸│  Node B          │
│  10.51.240.182   │                                 │  10.51.240.55    │
│  broadcast+listen│ ◂───────────────────────────────│  broadcast+listen│
└─────────────────┘         UDP broadcast          └─────────────────┘




[node]
  network_range   = "10.51.240.0/23"
  port            = 5678
  interval        = "30s"
  shared_secret   = "CHANGE_ME"
  db_path         = "/var/lib/lanmon/hosts.db"
  rpc_socket      = "/run/lanmon/server.sock"
  stale_threshold = "90s"
  log_level       = "info"

### [NEW] Default Config & Edit Command

Improve usability by automating configuration discovery and editing.

#### [MODIFY] [main.go](file:///home/user/Node_Discovery/main.go)
- Default `--config` to `config.toml` in the current directory if it exists, otherwise `/etc/lanmon/config.toml`.
- Add `edit` subcommand to open the configuration in a system editor.

#### [NEW] [edit.go](file:///home/user/Node_Discovery/cmd/edit/edit.go)
- Logic to find the config file.
- Support for `$EDITOR` environment variable.

## Verification Plan

### Automated Tests
- `go test ./pkg/config/...` for path expansion.
- `go test ./internal/hosts/...` (new tests).

### Manual Verification
1. Run `./lanmon node` without `--config`.
2. Run `./lanmon connect` without `--config`.
3. Run `./lanmon edit` and verify it opens the config.
4. Verify `/etc/hosts` contains discovered peers.
5. Verify `ssh <hostname>` works.

