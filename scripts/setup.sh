#!/bin/bash
# lanmon-setup.sh — Install lanmon as a systemd service
# Requirements: lanmon binary and config.toml in the same directory as this script
# Usage: sudo ./lanmon-setup.sh
set -euo pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

[[ $EUID -eq 0 ]] || { echo "Run as root: sudo $0"; exit 1; }
[[ -f "$DIR/lanmon" ]]     || { echo "Error: 'lanmon' binary not found in $DIR"; exit 1; }
[[ -f "$DIR/config.toml" ]] || { echo "Error: 'config.toml' not found in $DIR"; exit 1; }

echo "[1/5] Installing binary..."
install -m 0755 "$DIR/lanmon" /usr/local/bin/lanmon

echo "[2/5] Creating directories..."
mkdir -p /etc/lanmon /var/lib/lanmon /run/lanmon /var/log/lanmon

echo "[3/5] Installing config..."
if [[ -f /etc/lanmon/config.toml ]]; then
  echo "      Config already exists — skipping."
else
  install -m 0640 "$DIR/config.toml" /etc/lanmon/config.toml
fi

echo "[4/5] Installing systemd service..."
cat > /etc/systemd/system/lanmon.service <<'EOF'
[Unit]
Description=LANmon P2P Node Discovery Service
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=root
ExecStart=/usr/local/bin/lanmon node --config /etc/lanmon/config.toml
WorkingDirectory=/etc/lanmon
Restart=on-failure
RestartSec=10s
RuntimeDirectory=lanmon
StateDirectory=lanmon

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable lanmon

echo "[5/5] Starting service..."
systemctl restart lanmon
sleep 2
systemctl status lanmon --no-pager

echo ""
echo "Done! Edit config: /etc/lanmon/config.toml"
echo "Logs:              journalctl -u lanmon -f"
