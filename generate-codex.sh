#!/usr/bin/env bash
# generate-codex.sh — add OpenAI Codex MCP server to .mcp.json.
#
# Run this in any repo to inject a "codex" entry into .mcp.json so Claude Code
# can use Codex as an MCP tool. Merges safely with any existing .mcp.json entries.
# Also available as: ./generate-env-lsp.sh --codex
#
# Prerequisites: npm install -g @openai/codex
# Re-run with --force to overwrite an existing codex entry.
set -euo pipefail

FORCE=0
for arg in "$@"; do [ "$arg" = "--force" ] && FORCE=1; done

REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
cd "$REPO_ROOT"

ok()    { printf "  \033[32mok\033[0m     %s\n" "$*"; }
miss()  { printf "  \033[31mMISSING\033[0m %s\n" "$*"; }
wrote() { printf "  \033[36mwrote\033[0m  %s\n" "$*"; }
skip()  { printf "  \033[33mskip\033[0m   %s (already exists; --force to overwrite)\n" "$*"; }

gitignore_add() {
    local entry="$1"
    local gi="$REPO_ROOT/.gitignore"
    [ -f "$gi" ] && grep -qxF "$entry" "$gi" && return
    echo "$entry" >> "$gi"
    ok ".gitignore ← $entry"
}

# ── 1. locate codex ───────────────────────────────────────────────────────────

echo ""
echo "Locating codex..."

CODEX_BIN="$(which codex 2>/dev/null || true)"
if [ -z "$CODEX_BIN" ]; then
    miss "codex"
    echo ""
    echo "Install codex, then re-run:"
    echo "  npm install -g @openai/codex"
    exit 1
fi
ok "codex → $CODEX_BIN"

# ── 2. check for existing codex entry ────────────────────────────────────────

MCP_FILE="$REPO_ROOT/.mcp.json"

if [ -f "$MCP_FILE" ] && [ "$FORCE" -eq 0 ]; then
    # Check if codex key already present
    if python3 -c "
import json, sys
data = json.load(open('$MCP_FILE'))
sys.exit(0 if 'codex' in data.get('mcpServers', {}) else 1)
" 2>/dev/null; then
        skip ".mcp.json[mcpServers.codex]"
        echo ""
        echo "Codex MCP already present. Use --force to overwrite."
        exit 0
    fi
fi

# ── 3. merge codex entry into .mcp.json ──────────────────────────────────────

echo ""
echo "Writing codex entry to .mcp.json..."

python3 - "$MCP_FILE" "$CODEX_BIN" << 'PYEOF'
import sys, json, os

mcp_path, codex_bin = sys.argv[1], sys.argv[2]

if os.path.exists(mcp_path):
    with open(mcp_path) as f:
        data = json.load(f)
else:
    data = {}

data.setdefault("mcpServers", {})["codex"] = {
    "command": codex_bin,
    "args": ["--mcp-server"],
    "env": {}
}

with open(mcp_path, "w") as f:
    json.dump(data, f, indent=2)
    f.write("\n")
PYEOF

wrote ".mcp.json"

# .mcp.json contains machine-specific absolute paths — gitignore it
gitignore_add ".mcp.json"

# ── done ──────────────────────────────────────────────────────────────────────

echo ""
echo "Done. Codex MCP server added to .mcp.json"
echo "  Restart Claude Code to pick up the new server."
echo ""
echo "To remove the entry later, edit .mcp.json and delete the \"codex\" key."
echo "To regenerate: ./generate-codex.sh --force"
echo ""
