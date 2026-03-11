#!/bin/bash
# dev-push.sh - Build and install sbox dev binary to a workspace
set -e

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

DEST="`pwd`"
ARCH="$(go env GOARCH)"
DEBUG=${DEBUG:-""}

mkdir -p "$DEST/.sbox"

OUT="${OUT:-$DEST/.sbox/sbox-dev}"

if [[ "$DEBUG" == "true" ]]; then
    echo "Overriding to have a development sbox entrypoint (sbox-dev)..."
    echo "  Target: linux/$ARCH"
    echo "  Dest:   $OUT"
    echo ""
fi

# Build to temp location first
cd "$REPO_ROOT"
GOOS=linux GOARCH=$ARCH go -C "$ROOT/.." build -o $OUT ./cmd/sbox

if [[ "$DEBUG" == "true" ]]; then
    echo ""
    echo "✓ Dev binary installed successfully"
    echo ""
    echo "Next steps:"
    echo "  sbox stop"
    echo "  sbox run"
    echo ""
    echo "The entrypoint will automatically use your dev binary."
fi