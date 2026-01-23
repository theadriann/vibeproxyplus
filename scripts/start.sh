#!/bin/bash
# start.sh - Start both proxies

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

cd "$PROJECT_DIR"

# Build if needed
if [ ! -f bin/thinking-proxy ]; then
    echo "Building thinking-proxy..."
    go build -o bin/thinking-proxy ./cmd/thinking-proxy
fi

# Check for CLIProxyAPIPlus
if [ ! -f bin/cli-proxy-api-plus ]; then
    echo "Error: bin/cli-proxy-api-plus not found"
    echo "Run 'make download-cliproxy' first"
    exit 1
fi

# Start CLIProxyAPIPlus in background
echo "Starting CLIProxyAPIPlus on :8318..."
./bin/cli-proxy-api-plus -config config/cliproxy.yaml &
CLIPROXY_PID=$!

# Give it a moment
sleep 1

# Start ThinkingProxy in foreground
echo "Starting ThinkingProxy on :8317..."
echo "Press Ctrl+C to stop"
./bin/thinking-proxy

# Cleanup
kill $CLIPROXY_PID 2>/dev/null
echo "Stopped."
