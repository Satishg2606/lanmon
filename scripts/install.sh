#!/bin/bash
# lanmon install script
# Usage: sudo ./install.sh [agent|server|both]
set -euo pipefail

ROLE="${1:-both}"
BINARY="./bin/lanmon"
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/lanmon"
DATA_DIR="/var/lib/lanmon"
RUN_DIR="/run/lanmon"
SYSTEMD_DIR="/etc/systemd/system"

echo "=== lanmon installer ==="

# Check for root
if [[ $EUID -ne 0 ]]; then
    echo "Error: This script must be run as root"
    exit 1
fi

# Check binary exists
if [[ ! -f "$BINARY" ]]; then
    echo "Error: Binary not found at $BINARY"
    echo "Run 'make build' first"
    exit 1
fi

# Create system user
if ! id "lanmon" &>/dev/null; then
    echo "[+] Creating system user 'lanmon'"
    useradd -r -s /sbin/nologin -d /var/lib/lanmon lanmon
fi

# Install binary
echo "[+] Installing binary to $INSTALL_DIR"
install -m 0755 "$BINARY" "$INSTALL_DIR/lanmon"

# Create directories
echo "[+] Creating directories"
install -m 0700 -o lanmon -g lanmon -d "$DATA_DIR" "$RUN_DIR"
install -m 0700 -d "$CONFIG_DIR"

# Install config if not present
if [[ ! -f "$CONFIG_DIR/config.toml" ]]; then
    echo "[+] Installing example config"
    install -m 0600 ./config.toml.example "$CONFIG_DIR/config.toml"
    echo "    ⚠ Edit $CONFIG_DIR/config.toml and set shared_secret!"
fi

# Install systemd units
echo "[+] Installing systemd service units"
if [[ "$ROLE" == "agent" || "$ROLE" == "both" ]]; then
    install -m 0644 ./systemd/lanmon-agent.service "$SYSTEMD_DIR/"
fi
if [[ "$ROLE" == "server" || "$ROLE" == "both" ]]; then
    install -m 0644 ./systemd/lanmon-server.service "$SYSTEMD_DIR/"
fi

systemctl daemon-reload

# Enable and start services
if [[ "$ROLE" == "agent" || "$ROLE" == "both" ]]; then
    echo "[+] Enabling lanmon-agent"
    systemctl enable --now lanmon-agent
fi
if [[ "$ROLE" == "server" || "$ROLE" == "both" ]]; then
    echo "[+] Enabling lanmon-server"
    systemctl enable --now lanmon-server
fi

echo ""
echo "=== Installation complete ==="
echo "  Binary:  $INSTALL_DIR/lanmon"
echo "  Config:  $CONFIG_DIR/config.toml"
echo "  Data:    $DATA_DIR/"
echo "  Logs:    journalctl -u lanmon-agent / lanmon-server"
