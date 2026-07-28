#!/usr/bin/env bash
# bootstrap-repo.sh — bootstrap LSP for a target repo in one command.
#
# Usage:
#   ./bootstrap-repo.sh /path/to/repo
#   ./bootstrap-repo.sh /path/to/repo --codex
#   ./bootstrap-repo.sh /path/to/repo --opencode
#   ./bootstrap-repo.sh /path/to/repo --all
#
# This script:
#   1. Validates the target is a git repo
#   2. Runs generate-env-lsp.sh in the target repo
#   3. Builds and installs lsp-mcp-bridge (if not already installed)
#   4. Starts the bridge daemon
#   5. Reports health/status
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# ── arguments ──────────────────────────────────────────────────────────────

if [ $# -lt 1 ]; then
    echo "Usage: $0 <repo-path> [--codex|--opencode|--all]"
    echo ""
    echo "  <repo-path>  Path to the git repository to bootstrap"
    echo "  --codex      Also wire Codex CLI as MCP server"
    echo "  --opencode   Also write .opencode/opencode.json"
    echo "  --all        Wire Claude Code + Codex + opencode"
    exit 1
fi

TARGET_REPO="$1"; shift
EXTRA_ARGS="$@"

if [ ! -d "$TARGET_REPO" ]; then
    echo "Error: '$TARGET_REPO' is not a directory"
    exit 1
fi

# Resolve to absolute path
TARGET_REPO="$(cd "$TARGET_REPO" && pwd)"

# Verify it's a git repo
if ! git -C "$TARGET_REPO" rev-parse --show-toplevel &>/dev/null; then
    echo "Error: '$TARGET_REPO' is not a git repository"
    exit 1
fi

echo "=== Bootstrap LSP for: $TARGET_REPO ==="

# ── 1. run generate-env-lsp.sh ─────────────────────────────────────────────

echo ""
echo "→ Running generate-env-lsp.sh..."
"$SCRIPT_DIR/generate-env-lsp.sh" $EXTRA_ARGS --force
GEN_STATUS=$?

if [ $GEN_STATUS -ne 0 ]; then
    echo ""
    echo "Error: generate-env-lsp.sh failed (exit $GEN_STATUS)"
    echo "Check that required binaries are installed:"
    echo "  npm install -g pyright typescript-language-server typescript"
    exit 1
fi

# ── 2. build/install bridge binary ─────────────────────────────────────────

echo ""
echo "→ Checking lsp-mcp-bridge..."

if command -v lsp-mcp-bridge &>/dev/null; then
    echo "  lsp-mcp-bridge already installed: $(which lsp-mcp-bridge)"
else
    echo "  Installing lsp-mcp-bridge..."
    BRIDGE_DIR="$SCRIPT_DIR/lsp-mcp-bridge"
    if [ -d "$BRIDGE_DIR" ]; then
        if [ -f "$BRIDGE_DIR/justfile" ]; then
            (cd "$BRIDGE_DIR" && just install)
        elif [ -f "$BRIDGE_DIR/Makefile" ]; then
            (cd "$BRIDGE_DIR" && make install)
        else
            echo "  Error: No justfile or Makefile found in $BRIDGE_DIR"
            echo "  Build and install lsp-mcp-bridge manually, then retry."
            exit 1
        fi
    else
        echo "  Error: lsp-mcp-bridge source not found at $BRIDGE_DIR"
        echo "  Clone the bridge repo or build it manually, then retry."
        exit 1
    fi

    if ! command -v lsp-mcp-bridge &>/dev/null; then
        echo "  Warning: lsp-mcp-bridge still not found on PATH after install."
        echo "  Check that ~/.local/bin (or your install dir) is in \$PATH."
    fi
fi

# ── 3. start the bridge ────────────────────────────────────────────────────

echo ""
echo "→ Starting LSP bridge..."
(
    cd "$TARGET_REPO"
    ./start-lsp.sh
)

# ── 4. health check ────────────────────────────────────────────────────────

echo ""
echo "→ Checking bridge health..."

# Read the port from env.lsp
LSP_PORT=$(grep '^LSP_PORT=' "$TARGET_REPO/env.lsp" | cut -d= -f2)
HEALTH_URL="http://127.0.0.1:${LSP_PORT}/health"

sleep 1
if curl -fsS "$HEALTH_URL" &>/dev/null; then
    echo "  Bridge is healthy: $HEALTH_URL"
else
    echo "  Warning: Bridge health check failed at $HEALTH_URL"
    echo "  The bridge may still be starting up. Check logs:"
    LSP_LOG=$(grep '^LSP_LOG=' "$TARGET_REPO/env.lsp" | cut -d= -f2)
    echo "    $LSP_LOG"
fi

# ── done ───────────────────────────────────────────────────────────────────

echo ""
echo "=== Bootstrap complete ==="
echo ""
echo "Files written to $TARGET_REPO:"
echo "  env.lsp              — machine-specific paths (gitignored)"
echo "  .mcp.json            — Claude Code MCP config (gitignored)"
[ -f "$TARGET_REPO/.opencode/opencode.json" ] && \
    echo "  .opencode/opencode.json — opencode MCP config (gitignored)"
echo ""
echo "To use:"
echo "  1. Restart Claude Code / opencode to pick up MCP servers"
echo "  2. Day-to-day: make -f Makefile.lsp lsp-start|stop|health"