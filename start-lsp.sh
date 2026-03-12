#!/usr/bin/env bash
# start-lsp.sh — verify LSP prerequisites and start the MCP bridge daemon.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

# Allow env.custom to override defaults before env.lsp resolves them
[ -f "$SCRIPT_DIR/env.custom" ] && source "$SCRIPT_DIR/env.custom"
source "$SCRIPT_DIR/env.lsp"

# ---------------------------------------------------------------------------
# Prerequisite checks
# ---------------------------------------------------------------------------
fail=0

check_bin() {
    local name="$1" path="$2"
    if [ -z "$path" ] || ! command -v "$path" &>/dev/null; then
        echo "  MISSING  $name"
        fail=1
    else
        echo "  ok       $name → $path"
    fi
}

echo "Checking prerequisites..."
check_bin "pyright-langserver" "$LSP_PYRIGHT_BIN"
check_bin "typescript-language-server" "$LSP_TSS_BIN"

if [ -z "$LSP_PYTHON" ] || [ ! -x "$LSP_PYTHON" ]; then
    echo "  MISSING  python (set LSP_PYTHON or add python to PATH)"
    fail=1
else
    echo "  ok       python → $LSP_PYTHON ($("$LSP_PYTHON" --version 2>&1))"
fi

if ! command -v node &>/dev/null; then
    echo "  MISSING  node"
    fail=1
else
    echo "  ok       node → $(node --version)"
fi

if [ "$fail" -ne 0 ]; then
    echo ""
    echo "Install missing tools:"
    echo "  npm install -g pyright typescript-language-server typescript"
    exit 1
fi

echo ""
echo "Environment:"
echo "  workspace   $LSP_WORKSPACE"
echo "  port        $LSP_PORT"
echo "  log         $LSP_LOG"
echo "  pid         $LSP_PID"

# ---------------------------------------------------------------------------
# Guard: already running?
# ---------------------------------------------------------------------------
if [ -f "$LSP_PID" ]; then
    pid=$(cat "$LSP_PID")
    if kill -0 "$pid" 2>/dev/null; then
        echo ""
        echo "LSP bridge already running (pid $pid). Use stop-lsp.sh to restart."
        exit 0
    else
        echo "(Stale PID file removed)"
        rm "$LSP_PID"
    fi
fi

# ---------------------------------------------------------------------------
# Start bridge daemon
# ---------------------------------------------------------------------------
if ! BRIDGE=$(command -v lsp-mcp-bridge 2>/dev/null); then
    echo "Error: lsp-mcp-bridge not found on PATH."
    echo "  Install it with:  just install   (from the lsp-mcp-bridge source directory)"
    echo "  Or manually:      cp /path/to/lsp-mcp-bridge ~/.local/bin/"
    exit 1
fi
mkdir -p "$(dirname "$LSP_LOG")"
nohup "$BRIDGE" \
    --workspace "$LSP_WORKSPACE" \
    --port      "$LSP_PORT"      \
    --env-lsp   "$SCRIPT_DIR/env.lsp" \
    >> "$LSP_LOG" 2>&1 &
echo $! > "$LSP_PID"
echo "Started (pid $(cat "$LSP_PID")) → http://localhost:$LSP_PORT/mcp"
