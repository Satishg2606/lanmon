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

# ── Step 1: Install Go (if missing) ──────────────────────────────────────────
install_go() {
  echo "── Installing latest Go ─────────────────────────"
  GO_VERSION=$(curl -s https://go.dev/VERSION?m=text | head -n1)
  ARCH=$(uname -m)
  case $ARCH in
    x86_64) GO_ARCH="amd64" ;;
    aarch64) GO_ARCH="arm64" ;;
    *) die "Unsupported architecture for Go auto-install: $ARCH" ;;
  esac

  GO_TAR="${GO_VERSION}.linux-${GO_ARCH}.tar.gz"
  GO_URL="https://go.dev/dl/${GO_TAR}"

  echo "Downloading $GO_VERSION for $GO_ARCH..."
  curl -L "$GO_URL" -o "/tmp/$GO_TAR"
  
  echo "Extracting to /usr/local..."
  rm -rf /usr/local/go
  tar -C /usr/local -xzf "/tmp/$GO_TAR"
  rm "/tmp/$GO_TAR"

  export PATH=$PATH:/usr/local/go/bin
  ok "Go installed: $(go version)"
}

if $DO_BUILD; then
  echo "── Checking for Go ──────────────────────────────"
  if ! command -v go >/dev/null 2>&1 && [[ ! -x /usr/local/go/bin/go ]]; then
    warn "Go not found. Attempting to install latest version..."
    install_go
  else
    export PATH=$PATH:/usr/local/go/bin
    ok "Go is already installed: $(go version)"
  fi

  echo "── Building lanmon from source ──────────────────"
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

# ── Step 5: Install rqlite ───────────────────────────────────────────────────
echo ""
echo "── Checking for rqlite ──────────────────────────"

# Helper: build rqlite from source (works on any GLIBC version if Go is present)
build_rqlite_from_source() {
  local version="$1"
  warn "Building rqlite v${version} from source..."
  if ! command -v go > /dev/null 2>&1 && [[ ! -x /usr/local/go/bin/go ]]; then
    die "Go is required to build rqlite from source but was not found."
  fi
  export PATH=$PATH:/usr/local/go/bin

  # Rocky Linux 8 / RHEL 8 Fix: Activate newer gcc-toolset if available to avoid 'as --gdwarf-4' error
  if [[ -f /opt/rh/gcc-toolset-13/enable ]]; then
    source /opt/rh/gcc-toolset-13/enable
    ok "Activated gcc-toolset-13 for build"
  elif [[ -f /opt/rh/gcc-toolset-11/enable ]]; then
    source /opt/rh/gcc-toolset-11/enable
    ok "Activated gcc-toolset-11 for build"
  fi

  local tmpdir
  tmpdir=$(mktemp -d)
  # Clone and build rqlite at the specified tag
  cd "$tmpdir"
  echo "Cloning rqlite v${version}..."
  git clone --depth 1 --branch "v${version}" https://github.com/rqlite/rqlite.git rqlite-src
  cd rqlite-src
  echo "Building rqlited..."
  go build -ldflags="-s -w" -o /usr/local/bin/rqlited ./cmd/rqlited
  echo "Building rqlite..."
  go build -ldflags="-s -w" -o /usr/local/bin/rqlite  ./cmd/rqlite
  cd /
  rm -rf "$tmpdir"
  ok "rqlite built from source and installed to /usr/local/bin"
}

RQLITE_VERSION="9.4.5"

if ! command -v rqlited > /dev/null 2>&1; then
  warn "rqlite not found. Installing..."
  GO_OS="linux"
  ARCH=$(uname -m)
  case $ARCH in
    x86_64) GO_ARCH="amd64" ;;
    aarch64) GO_ARCH="arm64" ;;
    *) die "Unsupported architecture for rqlite auto-install: $ARCH" ;;
  esac

  RQLITE_TAR="rqlite-v${RQLITE_VERSION}-${GO_OS}-${GO_ARCH}.tar.gz"
  RQLITE_URL="https://github.com/rqlite/rqlite/releases/download/v${RQLITE_VERSION}/${RQLITE_TAR}"

  echo "Downloading rqlite v$RQLITE_VERSION..."
  curl -L "$RQLITE_URL" -o "/tmp/$RQLITE_TAR"
  tar -C /tmp -xzf "/tmp/$RQLITE_TAR"
  mv "/tmp/rqlite-v${RQLITE_VERSION}-${GO_OS}-${GO_ARCH}/rqlited" "/usr/local/bin/"
  mv "/tmp/rqlite-v${RQLITE_VERSION}-${GO_OS}-${GO_ARCH}/rqlite" "/usr/local/bin/"
  rm -rf "/tmp/rqlite-v${RQLITE_VERSION}-${GO_OS}-${GO_ARCH}" "/tmp/$RQLITE_TAR"

  # Verify the binary actually runs (catches GLIBC version mismatches)
  if ! rqlited -version > /dev/null 2>&1; then
    warn "Pre-built rqlite binary is incompatible with this system's GLIBC version."
    warn "Falling back to building rqlite from source..."
    rm -f /usr/local/bin/rqlited /usr/local/bin/rqlite
    build_rqlite_from_source "$RQLITE_VERSION"
  else
    ok "rqlite installed to /usr/local/bin"
  fi
else
  # Already installed — verify it actually works on this system
  if ! rqlited -version > /dev/null 2>&1; then
    warn "Installed rqlited binary is incompatible with this system's GLIBC version."
    warn "Replacing with a from-source build..."
    rm -f /usr/local/bin/rqlited /usr/local/bin/rqlite
    build_rqlite_from_source "$RQLITE_VERSION"
  else
    ok "rqlite already installed: $(rqlited -version | head -n1)"
  fi
fi

# Install rqlite systemd service
RQLITE_SERVICE_SRC="$PROJECT_DIR/systemd/rqlite.service"
if [[ -f "$RQLITE_SERVICE_SRC" ]]; then
  install -m 0644 "$RQLITE_SERVICE_SRC" "$SYSTEMD_DIR/rqlite.service"
  systemctl daemon-reload
  systemctl enable rqlite >/dev/null 2>&1
  systemctl restart rqlite
  ok "rqlite service installed and started"
else
  warn "rqlite.service file not found at $RQLITE_SERVICE_SRC. Service not configured."
fi

# ── Step 6: Install systemd service ─────────────────────────────────────────
echo ""
echo "── Installing lanmon systemd service ────────────"
[[ -f "$SERVICE_FILE" ]] || die "Service file not found: $SERVICE_FILE"
install -m 0644 "$SERVICE_FILE" "$SYSTEMD_DIR/$SERVICE_NAME.service"
systemctl daemon-reload
ok "Service installed: $SYSTEMD_DIR/$SERVICE_NAME.service"

systemctl enable "$SERVICE_NAME" >/dev/null 2>&1
ok "Service enabled (auto-start on boot)"

# ── Step 7: Start service ────────────────────────────────────────────────────
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
