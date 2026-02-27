#!/bin/bash
# Generate a random 32-byte hex shared secret for lanmon HMAC authentication
set -euo pipefail

SECRET=$(openssl rand -hex 32)

echo "Generated shared secret:"
echo ""
echo "  $SECRET"
echo ""
echo "Add this to /etc/lanmon/config.toml on ALL agents and the server:"
echo ""
echo "  shared_secret = \"$SECRET\""
echo ""

# Optionally write directly to config
if [[ "${1:-}" == "--write" ]]; then
    CONFIG_FILE="/etc/lanmon/config.toml"
    if [[ -f "$CONFIG_FILE" ]]; then
        sed -i "s/shared_secret.*=.*/shared_secret = \"$SECRET\"/" "$CONFIG_FILE"
        echo "Updated $CONFIG_FILE"
    else
        echo "Config file not found: $CONFIG_FILE"
        exit 1
    fi
fi
