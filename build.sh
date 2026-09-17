#!/usr/bin/env bash
# Build the FX trading bot as native binaries for Linux and Windows.
# The app is pure Go (no CGO), so it cross-compiles cleanly.
set -euo pipefail
cd "$(dirname "$0")"

mkdir -p dist

echo "==> Building for Linux (amd64)"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" \
  -o dist/fxbot-linux-amd64 ./cmd/fxbot

echo "==> Building for Windows (amd64)"
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w" \
  -o dist/fxbot-windows-amd64.exe ./cmd/fxbot

echo "==> Building for macOS (amd64)"
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags "-s -w" \
  -o dist/fxbot-darwin-amd64 ./cmd/fxbot

echo
echo "Build complete. Binaries in ./dist:"
ls -lh dist/

cat <<'EOF'

Run it from the repo root (it needs the pepperstone-ctrader-python folder):
  ./dist/fxbot-linux-amd64
  dist/fxbot-windows-amd64.exe   (on Windows)
EOF