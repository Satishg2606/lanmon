#!/bin/bash
# =============================================================================
# lanmon uninstall script — removes binary, service, and optionally data/config
#
# Usage:
#   sudo ./scripts/uninstall.sh           # Remove binary + service (keep data/config)
#   sudo ./scripts/uninstall.sh --purge   # Remove everything including data and config
# =============================================================================
set -euo pipefail

PURGE=false
for arg in "$@"; do
  [[ "$arg" == "--purge" ]] && PURGE=true
done

GREEN='\033[0;32m'; YELLOW='\033[1;33m'; RED='\033[0;31m'; NC='\033[0m'
ok()   { echo -e "${GREEN}[✓]${NC} $*"; }
warn() { echo -e "${YELLOW}[!]${NC} $*"; }
die()  { echo -e "${RED}[✗]${NC} $*"; exit 1; }

[[ $EUID -eq 0 ]] || die "This script must be run as root (use sudo)"

echo ""
echo "╔══════════════════════════════════════════════╗"
echo "║       lanmon — Uninstall Script              ║"
echo "╚══════════════════════════════════════════════╝"
echo ""
$PURGE && warn "PURGE mode: config and data will also be removed."

# Stop and disable service
if systemctl is-active lanmon &>/dev/null; then
  systemctl stop lanmon
  ok "Service stopped"
fi
if systemctl is-enabled lanmon &>/dev/null; then
  systemctl disable lanmon
  ok "Service disabled"
fi

# Remove service unit
rm -f /etc/systemd/system/lanmon.service
systemctl daemon-reload
ok "Service unit removed"

# Remove binary
rm -f /usr/local/bin/lanmon
ok "Binary removed"

# Purge data and config
if $PURGE; then
  rm -rf /etc/lanmon /var/lib/lanmon /var/log/lanmon /run/lanmon
  ok "Config, data, and log directories removed"
else
  warn "Config and data preserved: /etc/lanmon  /var/lib/lanmon  /var/log/lanmon"
  warn "Run with --purge to remove them."
fi

echo ""
ok "lanmon uninstalled successfully."
echo ""
