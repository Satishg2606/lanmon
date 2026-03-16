#!/bin/bash
# =============================================================================
# lanmon install script
# Builds the lanmon binary from source, installs it to the system,
# copies the config, and registers it as a systemd service.
#
# Usage:
#   sudo ./scripts/install.sh            # Build + install + start service
#   sudo ./scripts/install.sh --no-build # Skip build (use existing bin/lanmon)
#   sudo ./scripts/install.sh --no-start # Install but don't start service
# =============================================================================
set -euo pipefail

# Ensure /usr/local/go/bin is in PATH (common for manual Go installs)
export PATH=$PATH:/usr/local/go/bin


# ── Defaults ────────────────────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

BINARY_SRC="$PROJECT_DIR/bin/lanmon"
INSTALL_BIN="/usr/local/bin/lanmon"
CONFIG_DIR="/etc/lanmon"
CONFIG_DST="$CONFIG_DIR/config.toml"
CONFIG_SRC="$PROJECT_DIR/config.toml"
DATA_DIR="/var/lib/lanmon"
RUN_DIR="/run/lanmon"
LOG_DIR="/var/log/lanmon"
SYSTEMD_DIR="/etc/systemd/system"
SERVICE_NAME="lanmon"
SERVICE_FILE="$PROJECT_DIR/systemd/lanmon.service"

DO_BUILD=true
DO_START=true

# ── Parse flags ──────────────────────────────────────────────────────────────
for arg in "$@"; do
  case "$arg" in
    --no-build) DO_BUILD=false ;;
    --no-start) DO_START=false ;;
    --help|-h)
      echo "Usage: sudo $0 [--no-build] [--no-start]"
      echo "  --no-build  Skip 'go build'; use existing bin/lanmon"
      echo "  --no-start  Install service but do not start it"
      exit 0
      ;;
  esac
done

# ── Colour helpers ───────────────────────────────────────────────────────────
GREEN='\033[0;32m'; YELLOW='\033[1;33m'; RED='\033[0;31m'; NC='\033[0m'
ok()   { echo -e "${GREEN}[✓]${NC} $*"; }
warn() { echo -e "${YELLOW}[!]${NC} $*"; }
die()  { echo -e "${RED}[✗]${NC} $*"; exit 1; }

echo ""
echo "╔══════════════════════════════════════════════╗"
echo "║        lanmon — Installation Script          ║"
echo "╚══════════════════════════════════════════════╝"
echo ""

# ── Root check ───────────────────────────────────────────────────────────────
[[ $EUID -eq 0 ]] || die "This script must be run as root (use sudo)"

# ── Step 1: Build ────────────────────────────────────────────────────────────
if $DO_BUILD; then
  echo "── Building lanmon from source ──────────────────"
  command -v go >/dev/null 2>&1 || die "Go is not installed. Install Go and try again."
  cd "$PROJECT_DIR"
  mkdir -p bin
  CGO_ENABLED=0 go build -ldflags="-s -w" -o "$BINARY_SRC" main.go
  ok "Build complete: $BINARY_SRC ($(du -sh "$BINARY_SRC" | cut -f1))"
else
  warn "Skipping build (--no-build)"
  [[ -f "$BINARY_SRC" ]] || die "Binary not found at $BINARY_SRC. Run without --no-build first."
fi

# ── Step 2: Install binary ───────────────────────────────────────────────────
echo ""
echo "── Installing binary ────────────────────────────"
install -m 0755 "$BINARY_SRC" "$INSTALL_BIN"
ok "Binary installed: $INSTALL_BIN"

# ── Step 3: Create directories ───────────────────────────────────────────────
echo ""
echo "── Creating system directories ──────────────────"
mkdir -p "$CONFIG_DIR" "$DATA_DIR" "$RUN_DIR" "$LOG_DIR"
chmod 750 "$CONFIG_DIR" "$DATA_DIR" "$LOG_DIR"
chmod 755 "$RUN_DIR"
ok "Directories ready: $CONFIG_DIR  $DATA_DIR  $LOG_DIR"

# ── Step 4: Install config ───────────────────────────────────────────────────
echo ""
echo "── Installing configuration ─────────────────────"
if [[ -f "$CONFIG_DST" ]]; then
  warn "Config already exists at $CONFIG_DST — skipping overwrite."
  warn "Delete it manually and re-run if you want a fresh config."
else
  install -m 0640 "$CONFIG_SRC" "$CONFIG_DST"
  ok "Config installed: $CONFIG_DST"
  # Check if shared_secret is still the default
  if grep -q "CHANGE_ME" "$CONFIG_DST" 2>/dev/null; then
    warn "⚠  shared_secret is still 'CHANGE_ME' in $CONFIG_DST"
    warn "   Edit the config before starting the service!"
  fi
fi

# ── Step 5: Install systemd service ─────────────────────────────────────────
echo ""
echo "── Installing systemd service ───────────────────"
[[ -f "$SERVICE_FILE" ]] || die "Service file not found: $SERVICE_FILE"
install -m 0644 "$SERVICE_FILE" "$SYSTEMD_DIR/$SERVICE_NAME.service"
systemctl daemon-reload
ok "Service installed: $SYSTEMD_DIR/$SERVICE_NAME.service"

systemctl enable "$SERVICE_NAME" >/dev/null 2>&1
ok "Service enabled (auto-start on boot)"

# ── Step 6: Start service ────────────────────────────────────────────────────
if $DO_START; then
  echo ""
  echo "── Starting service ─────────────────────────────"
  systemctl restart "$SERVICE_NAME"
  sleep 2
  STATUS=$(systemctl is-active "$SERVICE_NAME" 2>/dev/null || true)
  if [[ "$STATUS" == "active" ]]; then
    ok "Service is running (PID: $(systemctl show -p MainPID --value "$SERVICE_NAME"))"
  else
    warn "Service status: $STATUS"
    echo "    Check logs with: journalctl -u $SERVICE_NAME -n 20"
  fi
else
  warn "Service not started (--no-start). Run: systemctl start $SERVICE_NAME"
fi

# ── Summary ──────────────────────────────────────────────────────────────────
echo ""
echo "╔══════════════════════════════════════════════════════════════╗"
echo "║  Installation complete!                                      ║"
echo "╟──────────────────────────────────────────────────────────────╢"
printf "║  Binary:   %-50s ║\n" "$INSTALL_BIN"
printf "║  Config:   %-50s ║\n" "$CONFIG_DST"
printf "║  Data:     %-50s ║\n" "$DATA_DIR"
printf "║  Logs:     %-50s ║\n" "$LOG_DIR"
echo "╟──────────────────────────────────────────────────────────────╢"
echo "║  Useful commands:                                            ║"
echo "║    lanmon service status                                     ║"
echo "║    lanmon service restart                                    ║"
echo "║    lanmon cluster list                                       ║"
echo "║    journalctl -u lanmon -f                                   ║"
echo "╚══════════════════════════════════════════════════════════════╝"
echo ""
