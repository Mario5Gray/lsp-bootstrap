#!/usr/bin/env bash
# generate-env-lsp.sh — bootstrap LSP tooling for any repo.
#
# Copy this one file into a repo and run it. It will:
#   1. Resolve machine-specific binary paths
#   2. Write a concrete env.lsp (baked-in absolute paths, no shell substitution)
#   3. Write start-lsp.sh / stop-lsp.sh / check-types.sh if they don't exist
#   4. Scaffold pyrightconfig.json if missing and Python sources detected
#   5. Scaffold jsconfig.json if missing and a JS frontend detected
#   6. Add env.lsp and env.custom to .gitignore
#
# Re-run any time the Python environment changes (new venv, conda env, etc.).
# Pass --force to overwrite existing generated files.
set -euo pipefail

FORCE=0
CODEX=0
for arg in "$@"; do
    [ "$arg" = "--force" ] && FORCE=1
    [ "$arg" = "--codex" ] && CODEX=1
done

REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
cd "$REPO_ROOT"

# ── helpers ────────────────────────────────────────────────────────────────

ok()   { printf "  \033[32mok\033[0m     %s\n" "$*"; }
miss() { printf "  \033[31mMISSING\033[0m %s\n" "$*"; }
skip() { printf "  \033[33mskip\033[0m   %s (already exists; --force to overwrite)\n" "$*"; }
wrote(){ printf "  \033[36mwrote\033[0m  %s\n" "$*"; }

write_file() {
    local path="$1"; shift
    if [ -f "$path" ] && [ "$FORCE" -eq 0 ]; then
        skip "$path"
        return
    fi
    # $@ is the content passed as a heredoc via process substitution
    cat > "$path"
    chmod +x "$path" 2>/dev/null || true
    wrote "$path"
}

gitignore_add() {
    local entry="$1"
    local gi="$REPO_ROOT/.gitignore"
    [ -f "$gi" ] && grep -qxF "$entry" "$gi" && return
    echo "$entry" >> "$gi"
    ok ".gitignore ← $entry"
}

# ── 1. resolve binaries ────────────────────────────────────────────────────

echo ""
echo "Resolving binaries..."

fail=0

LSP_PYTHON="$(which python 2>/dev/null || true)"
if [ -z "$LSP_PYTHON" ]; then
    miss "python"
    fail=1
else
    ok "python → $LSP_PYTHON ($(${LSP_PYTHON} --version 2>&1))"
fi

LSP_PYRIGHT_BIN="$(which pyright-langserver 2>/dev/null || true)"
if [ -z "$LSP_PYRIGHT_BIN" ]; then
    miss "pyright-langserver"
    fail=1
else
    ok "pyright-langserver → $LSP_PYRIGHT_BIN"
fi

LSP_TSS_BIN="$(which typescript-language-server 2>/dev/null || true)"
if [ -z "$LSP_TSS_BIN" ]; then
    miss "typescript-language-server"
    fail=1
else
    ok "typescript-language-server → $LSP_TSS_BIN"
fi

# rust-analyzer is optional — warn but don't fail
LSP_RUST_BIN="$(which rust-analyzer 2>/dev/null || true)"
if [ -z "$LSP_RUST_BIN" ]; then
    printf "  \033[33mwarn\033[0m   rust-analyzer not found (Rust LSP will be skipped)\n"
    printf "         Install: rustup component add rust-analyzer\n"
else
    ok "rust-analyzer → $LSP_RUST_BIN"
fi

# gopls is optional — warn but don't fail
LSP_GOPLS_BIN="$(which gopls 2>/dev/null || true)"
if [ -z "$LSP_GOPLS_BIN" ]; then
    printf "  \033[33mwarn\033[0m   gopls not found (Go LSP will be skipped)\n"
    printf "         Install: go install golang.org/x/tools/gopls@latest\n"
else
    ok "gopls → $LSP_GOPLS_BIN"
fi

# kotlin-language-server is optional — warn but don't fail
LSP_KOTLIN_BIN="$(which kotlin-language-server 2>/dev/null || true)"
if [ -z "$LSP_KOTLIN_BIN" ]; then
    printf "  \033[33mwarn\033[0m   kotlin-language-server not found (Kotlin LSP will be skipped)\n"
    printf "         Install: https://github.com/fwcd/kotlin-language-server/releases\n"
else
    ok "kotlin-language-server → $LSP_KOTLIN_BIN"
fi

# metals is optional — warn but don't fail
LSP_METALS_BIN="$(which metals 2>/dev/null || true)"
if [ -z "$LSP_METALS_BIN" ]; then
    printf "  \033[33mwarn\033[0m   metals not found (Scala LSP will be skipped)\n"
    printf "         Install: https://scalameta.org/metals/docs/editors/vim#installation\n"
else
    ok "metals → $LSP_METALS_BIN"
fi

# taplo (TOML) is optional — warn but don't fail
LSP_TAPLO_BIN="$(which taplo 2>/dev/null || true)"
if [ -z "$LSP_TAPLO_BIN" ]; then
    printf "  \033[33mwarn\033[0m   taplo not found (TOML LSP will be skipped)\n"
    printf "         Install: cargo install taplo-cli\n"
else
    ok "taplo → $LSP_TAPLO_BIN"
fi

# vscode-json-language-server is optional — warn but don't fail
LSP_JSON_BIN="$(which vscode-json-language-server 2>/dev/null || true)"
LSP_HTML_BIN="$(which vscode-html-language-server 2>/dev/null || true)"
LSP_CSS_BIN="$(which vscode-css-language-server 2>/dev/null || true)"
if [ -z "$LSP_JSON_BIN" ]; then
    printf "  \033[33mwarn\033[0m   vscode-json-language-server not found (JSON/HTML/CSS LSP will be skipped)\n"
    printf "         Install: npm install -g vscode-langservers-extracted\n"
else
    ok "vscode-json-language-server → $LSP_JSON_BIN"
    [ -n "$LSP_HTML_BIN" ] && ok "vscode-html-language-server → $LSP_HTML_BIN"
    [ -n "$LSP_CSS_BIN"  ] && ok "vscode-css-language-server → $LSP_CSS_BIN"
fi

# bash-language-server is optional — warn but don't fail
LSP_BASH_BIN="$(which bash-language-server 2>/dev/null || true)"
if [ -z "$LSP_BASH_BIN" ]; then
    printf "  \033[33mwarn\033[0m   bash-language-server not found (Shell LSP will be skipped)\n"
    printf "         Install: npm install -g bash-language-server\n"
else
    ok "bash-language-server → $LSP_BASH_BIN"
fi

# yaml-language-server is optional — warn but don't fail
LSP_YAML_BIN="$(which yaml-language-server 2>/dev/null || true)"
if [ -z "$LSP_YAML_BIN" ]; then
    printf "  \033[33mwarn\033[0m   yaml-language-server not found (YAML LSP will be skipped)\n"
    printf "         Install: npm install -g yaml-language-server\n"
else
    ok "yaml-language-server → $LSP_YAML_BIN"
fi

# mcp-language-server is optional — warn but don't fail
LSP_MCP_BIN="$(which mcp-language-server 2>/dev/null || true)"
if [ -z "$LSP_MCP_BIN" ]; then
    printf "  \033[33mwarn\033[0m   mcp-language-server not found (.mcp.json will be skipped)\n"
    printf "         Install: go install github.com/isaacs/mcp-language-server@latest\n"
else
    ok "mcp-language-server → $LSP_MCP_BIN"
fi

# codex is optional — only checked when --codex passed
LSP_CODEX_BIN=""
if [ "$CODEX" -eq 1 ]; then
    LSP_CODEX_BIN="$(which codex 2>/dev/null || true)"
    if [ -z "$LSP_CODEX_BIN" ]; then
        printf "  \033[33mwarn\033[0m   codex not found (Codex MCP will be skipped)\n"
        printf "         Install: npm install -g @openai/codex\n"
    else
        ok "codex → $LSP_CODEX_BIN"
    fi
fi

if [ "$fail" -ne 0 ]; then
    echo ""
    echo "Install missing tools, then re-run:"
    echo "  npm install -g pyright typescript-language-server typescript"
    exit 1
fi

# ── 2. detect project type ─────────────────────────────────────────────────

echo ""
echo "Detecting project type..."

HAS_PYTHON=0
HAS_JS=0
HAS_RUST=0

for f in setup.py setup.cfg pyproject.toml requirements.txt; do
    [ -f "$REPO_ROOT/$f" ] && HAS_PYTHON=1 && break
done
if [ -d "$REPO_ROOT/src" ] || [ -d "$REPO_ROOT/backends" ] || \
   [ -d "$REPO_ROOT/server" ] || [ -d "$REPO_ROOT/lib" ]; then HAS_PYTHON=1; fi

[ -f "$REPO_ROOT/package.json" ] && HAS_JS=1
[ -f "$REPO_ROOT/jsconfig.json" ] && HAS_JS=1

[ -f "$REPO_ROOT/Cargo.toml" ] && HAS_RUST=1

HAS_GO=0
[ -f "$REPO_ROOT/go.mod" ] && HAS_GO=1

HAS_KOTLIN=0
if [ -f "$REPO_ROOT/build.gradle.kts" ] || [ -f "$REPO_ROOT/settings.gradle.kts" ]; then
    HAS_KOTLIN=1
elif find "$REPO_ROOT" -maxdepth 3 -name "*.kt" -print -quit 2>/dev/null | grep -q .; then
    HAS_KOTLIN=1
fi

HAS_SCALA=0
if [ -f "$REPO_ROOT/build.sbt" ]; then
    HAS_SCALA=1
elif find "$REPO_ROOT" -maxdepth 3 -name "*.scala" -print -quit 2>/dev/null | grep -q .; then
    HAS_SCALA=1
fi

HAS_SHELL=0
if find "$REPO_ROOT" -maxdepth 3 -name "*.sh" -print -quit 2>/dev/null | grep -q .; then
    HAS_SHELL=1
elif [ -f "$REPO_ROOT/Makefile" ] || [ -f "$REPO_ROOT/Dockerfile" ]; then
    HAS_SHELL=1
fi

HAS_YAML=0
if find "$REPO_ROOT" -maxdepth 3 \( -name "*.yml" -o -name "*.yaml" \) -print -quit 2>/dev/null | grep -q .; then
    HAS_YAML=1
fi

HAS_TOML=0
if find "$REPO_ROOT" -maxdepth 3 -name "*.toml" -print -quit 2>/dev/null | grep -q .; then
    HAS_TOML=1
fi

HAS_JSON=0
if find "$REPO_ROOT" -maxdepth 2 -name "*.json" -not -path "*/node_modules/*" -print -quit 2>/dev/null | grep -q .; then
    HAS_JSON=1
fi

HAS_HTML=0
if find "$REPO_ROOT" -maxdepth 3 -name "*.html" -not -path "*/node_modules/*" -print -quit 2>/dev/null | grep -q .; then
    HAS_HTML=1
fi

HAS_CSS=0
if find "$REPO_ROOT" -maxdepth 3 \( -name "*.css" -o -name "*.scss" -o -name "*.less" \) -not -path "*/node_modules/*" -print -quit 2>/dev/null | grep -q .; then
    HAS_CSS=1
fi

[ "$HAS_PYTHON" -eq 1 ] && ok "Python project detected"
[ "$HAS_JS" -eq 1 ]     && ok "JavaScript project detected"
[ "$HAS_RUST" -eq 1 ]   && ok "Rust project detected"
[ "$HAS_GO" -eq 1 ]     && ok "Go project detected"
[ "$HAS_KOTLIN" -eq 1 ] && ok "Kotlin project detected"
[ "$HAS_SCALA" -eq 1 ]  && ok "Scala project detected"
[ "$HAS_SHELL" -eq 1 ]  && ok "Shell scripts detected"
[ "$HAS_YAML" -eq 1 ]   && ok "YAML files detected"
[ "$HAS_TOML" -eq 1 ]   && ok "TOML files detected"
[ "$HAS_JSON" -eq 1 ]   && ok "JSON files detected"
[ "$HAS_HTML" -eq 1 ]   && ok "HTML files detected"
[ "$HAS_CSS" -eq 1 ]    && ok "CSS/SCSS files detected"
[ "$HAS_PYTHON" -eq 0 ] && [ "$HAS_JS" -eq 0 ] && [ "$HAS_RUST" -eq 0 ] && [ "$HAS_GO" -eq 0 ] && \
[ "$HAS_KOTLIN" -eq 0 ] && [ "$HAS_SCALA" -eq 0 ] && [ "$HAS_SHELL" -eq 0 ] && [ "$HAS_YAML" -eq 0 ] && \
[ "$HAS_TOML" -eq 0 ] && [ "$HAS_JSON" -eq 0 ] && [ "$HAS_HTML" -eq 0 ] && [ "$HAS_CSS" -eq 0 ] && \
ok "No specific project type detected (generic)"

# ── 3. write env.lsp ──────────────────────────────────────────────────────

echo ""
echo "Writing env.lsp..."

# env.lsp is always regenerated (it's machine-specific and gitignored)

# Derive a stable workspace-scoped port (7890–7989) and PID path so that
# multiple workspaces can run their own bridge instances simultaneously.
_workspace_hash=$(echo -n "$REPO_ROOT" | cksum | cut -d' ' -f1)
LSP_GEN_PORT=$(( 7890 + _workspace_hash % 100 ))
_workspace_slug=$(basename "$REPO_ROOT" | tr '[:upper:]' '[:lower:]' | tr -cs 'a-z0-9' '-' | sed 's/-$//')
LSP_GEN_PID="/tmp/lsp-bridge-${_workspace_slug}.pid"

cat > "$REPO_ROOT/env.lsp" << EOF
# env.lsp — LSP MCP bridge configuration
# Generated by generate-env-lsp.sh on $(date +%Y-%m-%d)
# Regenerate: ./generate-env-lsp.sh
#
# NOT compatible with docker --env-file (absolute paths, no shell substitution).
# Override anything in env.custom (gitignored).

LSP_PORT=$LSP_GEN_PORT
LSP_WORKSPACE=$REPO_ROOT
LSP_PYTHON=$LSP_PYTHON
LSP_PYRIGHT_BIN=$LSP_PYRIGHT_BIN
LSP_TSS_BIN=$LSP_TSS_BIN
LSP_LOG=$REPO_ROOT/logs/lsp-bridge.log
LSP_PID=$LSP_GEN_PID
EOF

wrote "$REPO_ROOT/env.lsp"
gitignore_add "env.lsp"
gitignore_add "env.custom"

# ── 4. write start-lsp.sh ─────────────────────────────────────────────────

echo ""
echo "Writing start-lsp.sh..."

write_file "$REPO_ROOT/start-lsp.sh" << 'SCRIPT'
#!/usr/bin/env bash
# start-lsp.sh — verify LSP prerequisites and start the MCP bridge daemon.
# Generated by generate-env-lsp.sh. Re-run generator to update.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
[ -f "$SCRIPT_DIR/env.custom" ] && source "$SCRIPT_DIR/env.custom"

if [ ! -f "$SCRIPT_DIR/env.lsp" ]; then
    echo "env.lsp not found. Run: ./generate-env-lsp.sh"
    exit 1
fi
source "$SCRIPT_DIR/env.lsp"

echo "Checking prerequisites..."
fail=0
for bin in "$LSP_PYRIGHT_BIN" "$LSP_TSS_BIN" "$LSP_PYTHON"; do
    if [ -z "$bin" ] || [ ! -x "$bin" ]; then
        echo "  MISSING  $bin  (re-run ./generate-env-lsp.sh)"
        fail=1
    else
        echo "  ok       $bin"
    fi
done
[ "$fail" -ne 0 ] && exit 1

echo ""
echo "Environment:"
echo "  workspace   $LSP_WORKSPACE"
echo "  port        $LSP_PORT  (bridge, Phase 3)"
echo "  log         $LSP_LOG"

if [ -f "$LSP_PID" ]; then
    pid=$(cat "$LSP_PID")
    if kill -0 "$pid" 2>/dev/null; then
        echo "LSP bridge already running (pid $pid). Use stop-lsp.sh to restart."
        exit 0
    else
        echo "(Stale PID file removed)"
        rm "$LSP_PID"
    fi
fi

# Phase 3 TODO: replace this block with the bridge start command.
#
#   mkdir -p "$(dirname "$LSP_LOG")"
#   nohup python -m lsp_mcp_bridge \
#       --workspace "$LSP_WORKSPACE" \
#       --python    "$LSP_PYTHON"    \
#       --pyright   "$LSP_PYRIGHT_BIN" \
#       --tss       "$LSP_TSS_BIN"  \
#       --port      "$LSP_PORT"     \
#       >> "$LSP_LOG" 2>&1 &
#   echo $! > "$LSP_PID"
#   echo "Started (pid $(cat "$LSP_PID")) → http://localhost:$LSP_PORT/mcp"

echo ""
echo "[Phase 3 pending] Bridge not yet built. Run ./check-types.sh for now."
SCRIPT

# ── 5. write stop-lsp.sh ──────────────────────────────────────────────────

echo ""
echo "Writing stop-lsp.sh..."

write_file "$REPO_ROOT/stop-lsp.sh" << 'SCRIPT'
#!/usr/bin/env bash
# stop-lsp.sh — stop the LSP MCP bridge daemon.
# Generated by generate-env-lsp.sh.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
[ -f "$SCRIPT_DIR/env.custom" ] && source "$SCRIPT_DIR/env.custom"

if [ ! -f "$SCRIPT_DIR/env.lsp" ]; then
    echo "env.lsp not found. Run: ./generate-env-lsp.sh"
    exit 1
fi
source "$SCRIPT_DIR/env.lsp"

if [ ! -f "$LSP_PID" ]; then
    echo "LSP bridge not running (no PID file at $LSP_PID)"
    exit 0
fi

pid=$(cat "$LSP_PID")
if kill -0 "$pid" 2>/dev/null; then
    kill "$pid"
    rm "$LSP_PID"
    echo "Stopped LSP bridge (pid $pid)"
else
    rm "$LSP_PID"
    echo "PID file was stale (pid $pid already gone), removed"
fi
SCRIPT

# ── 6. write check-types.sh ───────────────────────────────────────────────

echo ""
echo "Writing check-types.sh..."

write_file "$REPO_ROOT/check-types.sh" << 'SCRIPT'
#!/usr/bin/env bash
# check-types.sh — run pyright with the resolved Python path.
# Generated by generate-env-lsp.sh.
# Usage: ./check-types.sh [pyright args...]
#   ./check-types.sh                       # check whole project
#   ./check-types.sh path/to/file.py       # check one file
#   ./check-types.sh --outputjson . | jq   # JSON output
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR"
[ -f "$SCRIPT_DIR/env.custom" ] && source "$SCRIPT_DIR/env.custom"
source "$SCRIPT_DIR/env.lsp"

exec pyright --pythonpath "$LSP_PYTHON" "$@"
SCRIPT

# ── 7. scaffold pyrightconfig.json ────────────────────────────────────────

if [ "$HAS_PYTHON" -eq 1 ] && [ ! -f "$REPO_ROOT/pyrightconfig.json" ]; then
    echo ""
    echo "Scaffolding pyrightconfig.json..."

    # Guess include dirs: any top-level Python package (has __init__.py)
    INCLUDE_DIRS=()
    for d in "$REPO_ROOT"/*/; do
        dir="$(basename "$d")"
        [ -f "$d/__init__.py" ] && INCLUDE_DIRS+=("\"$dir\"")
    done
    # Fallback to src/ or . if nothing found
    if [ ${#INCLUDE_DIRS[@]} -eq 0 ]; then
        [ -d "$REPO_ROOT/src" ] && INCLUDE_DIRS=('"src"') || INCLUDE_DIRS=('"."')
    fi
    INCLUDE_JSON="$(IFS=', '; echo "${INCLUDE_DIRS[*]}")"

    PYTHON_VERSION="$("$LSP_PYTHON" -c 'import sys; print(f"{sys.version_info.major}.{sys.version_info.minor}")')"

    write_file "$REPO_ROOT/pyrightconfig.json" << EOF
{
  "include": [$INCLUDE_JSON],
  "exclude": ["node_modules", "**/__pycache__", ".git", ".mypy_cache"],
  "pythonVersion": "$PYTHON_VERSION",
  "pythonPlatform": "Linux",
  "typeCheckingMode": "basic",
  "reportMissingImports": true,
  "reportMissingTypeStubs": false,
  "executionEnvironments": [{ "root": "." }]
}
EOF
    echo "  Review and adjust the 'include' list for this project."
fi

# ── 8. scaffold jsconfig.json ─────────────────────────────────────────────

if [ "$HAS_JS" -eq 1 ]; then
    # Look for a frontend subdirectory with src/ inside
    JS_ROOT="$REPO_ROOT"
    for d in "$REPO_ROOT"/*/src; do
        parent="$(dirname "$d")"
        [ -f "$parent/package.json" ] && JS_ROOT="$parent" && break
    done
    JSCONFIG="$JS_ROOT/jsconfig.json"

    if [ ! -f "$JSCONFIG" ]; then
        echo ""
        echo "Scaffolding $JSCONFIG..."
        write_file "$JSCONFIG" << 'EOF'
{
  "compilerOptions": {
    "target": "ES2022",
    "module": "ESNext",
    "moduleResolution": "bundler",
    "jsx": "react-jsx",
    "checkJs": false,
    "allowJs": true,
    "noEmit": true,
    "paths": {
      "@/*": ["./src/*"]
    }
  },
  "include": ["src"],
  "exclude": ["node_modules", "dist"]
}
EOF
    fi
fi

# ── 9. write .mcp.json ────────────────────────────────────────────────────

if [ -n "$LSP_MCP_BIN" ]; then
    echo ""
    echo "Writing .mcp.json..."

    # Build mcpServers entries for each detected language
    MCP_ENTRIES=""
    MCP_SEP=""
    # Parallel JSON array for ~/.codex/config.toml (same servers, different format)
    CODEX_SERVERS="["
    CODEX_SEP=""

    if [ "$HAS_PYTHON" -eq 1 ]; then
        MCP_ENTRIES="${MCP_ENTRIES}${MCP_SEP}
    \"language-server-python\": {
      \"command\": \"$LSP_MCP_BIN\",
      \"args\": [\"-workspace\", \"$REPO_ROOT\", \"-lsp\", \"$LSP_PYRIGHT_BIN\", \"--\", \"--stdio\"],
      \"env\": { \"LOG_LEVEL\": \"INFO\" }
    }"
        CODEX_SERVERS="${CODEX_SERVERS}${CODEX_SEP}{\"name\":\"language-server-python\",\"command\":\"$LSP_MCP_BIN\",\"args\":[\"-workspace\",\"$REPO_ROOT\",\"-lsp\",\"$LSP_PYRIGHT_BIN\",\"--\",\"--stdio\"],\"env\":{\"LOG_LEVEL\":\"INFO\"}}"
        MCP_SEP=","
        CODEX_SEP=","
    fi

    if [ "$HAS_JS" -eq 1 ]; then
        MCP_ENTRIES="${MCP_ENTRIES}${MCP_SEP}
    \"language-server-js\": {
      \"command\": \"$LSP_MCP_BIN\",
      \"args\": [\"-workspace\", \"$REPO_ROOT\", \"-lsp\", \"$LSP_TSS_BIN\", \"--\", \"--stdio\"],
      \"env\": { \"LOG_LEVEL\": \"INFO\" }
    }"
        CODEX_SERVERS="${CODEX_SERVERS}${CODEX_SEP}{\"name\":\"language-server-js\",\"command\":\"$LSP_MCP_BIN\",\"args\":[\"-workspace\",\"$REPO_ROOT\",\"-lsp\",\"$LSP_TSS_BIN\",\"--\",\"--stdio\"],\"env\":{\"LOG_LEVEL\":\"INFO\"}}"
        MCP_SEP=","
        CODEX_SEP=","
    fi

    if [ "$HAS_RUST" -eq 1 ] && [ -n "$LSP_RUST_BIN" ]; then
        MCP_ENTRIES="${MCP_ENTRIES}${MCP_SEP}
    \"language-server-rust\": {
      \"command\": \"$LSP_MCP_BIN\",
      \"args\": [\"-workspace\", \"$REPO_ROOT\", \"-lsp\", \"$LSP_RUST_BIN\"],
      \"env\": { \"LOG_LEVEL\": \"INFO\" }
    }"
        CODEX_SERVERS="${CODEX_SERVERS}${CODEX_SEP}{\"name\":\"language-server-rust\",\"command\":\"$LSP_MCP_BIN\",\"args\":[\"-workspace\",\"$REPO_ROOT\",\"-lsp\",\"$LSP_RUST_BIN\"],\"env\":{\"LOG_LEVEL\":\"INFO\"}}"
        MCP_SEP=","
        CODEX_SEP=","
    fi

    if [ "$HAS_GO" -eq 1 ] && [ -n "$LSP_GOPLS_BIN" ]; then
        MCP_ENTRIES="${MCP_ENTRIES}${MCP_SEP}
    \"language-server-go\": {
      \"command\": \"$LSP_MCP_BIN\",
      \"args\": [\"-workspace\", \"$REPO_ROOT\", \"-lsp\", \"$LSP_GOPLS_BIN\"],
      \"env\": { \"LOG_LEVEL\": \"INFO\" }
    }"
        CODEX_SERVERS="${CODEX_SERVERS}${CODEX_SEP}{\"name\":\"language-server-go\",\"command\":\"$LSP_MCP_BIN\",\"args\":[\"-workspace\",\"$REPO_ROOT\",\"-lsp\",\"$LSP_GOPLS_BIN\"],\"env\":{\"LOG_LEVEL\":\"INFO\"}}"
        MCP_SEP=","
        CODEX_SEP=","
    fi

    if [ "$HAS_KOTLIN" -eq 1 ] && [ -n "$LSP_KOTLIN_BIN" ]; then
        MCP_ENTRIES="${MCP_ENTRIES}${MCP_SEP}
    \"language-server-kotlin\": {
      \"command\": \"$LSP_MCP_BIN\",
      \"args\": [\"-workspace\", \"$REPO_ROOT\", \"-lsp\", \"$LSP_KOTLIN_BIN\"],
      \"env\": { \"LOG_LEVEL\": \"INFO\" }
    }"
        CODEX_SERVERS="${CODEX_SERVERS}${CODEX_SEP}{\"name\":\"language-server-kotlin\",\"command\":\"$LSP_MCP_BIN\",\"args\":[\"-workspace\",\"$REPO_ROOT\",\"-lsp\",\"$LSP_KOTLIN_BIN\"],\"env\":{\"LOG_LEVEL\":\"INFO\"}}"
        MCP_SEP=","
        CODEX_SEP=","
    fi

    if [ "$HAS_SCALA" -eq 1 ] && [ -n "$LSP_METALS_BIN" ]; then
        MCP_ENTRIES="${MCP_ENTRIES}${MCP_SEP}
    \"language-server-scala\": {
      \"command\": \"$LSP_MCP_BIN\",
      \"args\": [\"-workspace\", \"$REPO_ROOT\", \"-lsp\", \"$LSP_METALS_BIN\"],
      \"env\": { \"LOG_LEVEL\": \"INFO\" }
    }"
        CODEX_SERVERS="${CODEX_SERVERS}${CODEX_SEP}{\"name\":\"language-server-scala\",\"command\":\"$LSP_MCP_BIN\",\"args\":[\"-workspace\",\"$REPO_ROOT\",\"-lsp\",\"$LSP_METALS_BIN\"],\"env\":{\"LOG_LEVEL\":\"INFO\"}}"
        MCP_SEP=","
        CODEX_SEP=","
    fi

    if [ "$HAS_TOML" -eq 1 ] && [ -n "$LSP_TAPLO_BIN" ]; then
        MCP_ENTRIES="${MCP_ENTRIES}${MCP_SEP}
    \"language-server-toml\": {
      \"command\": \"$LSP_MCP_BIN\",
      \"args\": [\"-workspace\", \"$REPO_ROOT\", \"-lsp\", \"$LSP_TAPLO_BIN\", \"--\", \"lsp\", \"stdio\"],
      \"env\": { \"LOG_LEVEL\": \"INFO\" }
    }"
        CODEX_SERVERS="${CODEX_SERVERS}${CODEX_SEP}{\"name\":\"language-server-toml\",\"command\":\"$LSP_MCP_BIN\",\"args\":[\"-workspace\",\"$REPO_ROOT\",\"-lsp\",\"$LSP_TAPLO_BIN\",\"--\",\"lsp\",\"stdio\"],\"env\":{\"LOG_LEVEL\":\"INFO\"}}"
        MCP_SEP=","
        CODEX_SEP=","
    fi

    if [ "$HAS_JSON" -eq 1 ] && [ -n "$LSP_JSON_BIN" ]; then
        MCP_ENTRIES="${MCP_ENTRIES}${MCP_SEP}
    \"language-server-json\": {
      \"command\": \"$LSP_MCP_BIN\",
      \"args\": [\"-workspace\", \"$REPO_ROOT\", \"-lsp\", \"$LSP_JSON_BIN\", \"--\", \"--stdio\"],
      \"env\": { \"LOG_LEVEL\": \"INFO\" }
    }"
        CODEX_SERVERS="${CODEX_SERVERS}${CODEX_SEP}{\"name\":\"language-server-json\",\"command\":\"$LSP_MCP_BIN\",\"args\":[\"-workspace\",\"$REPO_ROOT\",\"-lsp\",\"$LSP_JSON_BIN\",\"--\",\"--stdio\"],\"env\":{\"LOG_LEVEL\":\"INFO\"}}"
        MCP_SEP=","
        CODEX_SEP=","
    fi

    if [ "$HAS_HTML" -eq 1 ] && [ -n "$LSP_HTML_BIN" ]; then
        MCP_ENTRIES="${MCP_ENTRIES}${MCP_SEP}
    \"language-server-html\": {
      \"command\": \"$LSP_MCP_BIN\",
      \"args\": [\"-workspace\", \"$REPO_ROOT\", \"-lsp\", \"$LSP_HTML_BIN\", \"--\", \"--stdio\"],
      \"env\": { \"LOG_LEVEL\": \"INFO\" }
    }"
        CODEX_SERVERS="${CODEX_SERVERS}${CODEX_SEP}{\"name\":\"language-server-html\",\"command\":\"$LSP_MCP_BIN\",\"args\":[\"-workspace\",\"$REPO_ROOT\",\"-lsp\",\"$LSP_HTML_BIN\",\"--\",\"--stdio\"],\"env\":{\"LOG_LEVEL\":\"INFO\"}}"
        MCP_SEP=","
        CODEX_SEP=","
    fi

    if [ "$HAS_CSS" -eq 1 ] && [ -n "$LSP_CSS_BIN" ]; then
        MCP_ENTRIES="${MCP_ENTRIES}${MCP_SEP}
    \"language-server-css\": {
      \"command\": \"$LSP_MCP_BIN\",
      \"args\": [\"-workspace\", \"$REPO_ROOT\", \"-lsp\", \"$LSP_CSS_BIN\", \"--\", \"--stdio\"],
      \"env\": { \"LOG_LEVEL\": \"INFO\" }
    }"
        CODEX_SERVERS="${CODEX_SERVERS}${CODEX_SEP}{\"name\":\"language-server-css\",\"command\":\"$LSP_MCP_BIN\",\"args\":[\"-workspace\",\"$REPO_ROOT\",\"-lsp\",\"$LSP_CSS_BIN\",\"--\",\"--stdio\"],\"env\":{\"LOG_LEVEL\":\"INFO\"}}"
        MCP_SEP=","
        CODEX_SEP=","
    fi

    if [ "$HAS_SHELL" -eq 1 ] && [ -n "$LSP_BASH_BIN" ]; then
        MCP_ENTRIES="${MCP_ENTRIES}${MCP_SEP}
    \"language-server-shell\": {
      \"command\": \"$LSP_MCP_BIN\",
      \"args\": [\"-workspace\", \"$REPO_ROOT\", \"-lsp\", \"$LSP_BASH_BIN\", \"--\", \"--stdio\"],
      \"env\": { \"LOG_LEVEL\": \"INFO\" }
    }"
        CODEX_SERVERS="${CODEX_SERVERS}${CODEX_SEP}{\"name\":\"language-server-shell\",\"command\":\"$LSP_MCP_BIN\",\"args\":[\"-workspace\",\"$REPO_ROOT\",\"-lsp\",\"$LSP_BASH_BIN\",\"--\",\"--stdio\"],\"env\":{\"LOG_LEVEL\":\"INFO\"}}"
        MCP_SEP=","
        CODEX_SEP=","
    fi

    if [ "$HAS_YAML" -eq 1 ] && [ -n "$LSP_YAML_BIN" ]; then
        MCP_ENTRIES="${MCP_ENTRIES}${MCP_SEP}
    \"language-server-yaml\": {
      \"command\": \"$LSP_MCP_BIN\",
      \"args\": [\"-workspace\", \"$REPO_ROOT\", \"-lsp\", \"$LSP_YAML_BIN\", \"--\", \"--stdio\"],
      \"env\": { \"LOG_LEVEL\": \"INFO\" }
    }"
        CODEX_SERVERS="${CODEX_SERVERS}${CODEX_SEP}{\"name\":\"language-server-yaml\",\"command\":\"$LSP_MCP_BIN\",\"args\":[\"-workspace\",\"$REPO_ROOT\",\"-lsp\",\"$LSP_YAML_BIN\",\"--\",\"--stdio\"],\"env\":{\"LOG_LEVEL\":\"INFO\"}}"
        MCP_SEP=","
        CODEX_SEP=","
    fi

    if [ "$CODEX" -eq 1 ] && [ -n "$LSP_CODEX_BIN" ]; then
        MCP_ENTRIES="${MCP_ENTRIES}${MCP_SEP}
    \"codex\": {
      \"command\": \"$LSP_CODEX_BIN\",
      \"args\": [\"--mcp-server\"],
      \"env\": {}
    }"
        MCP_SEP=","
    fi

    CODEX_SERVERS="${CODEX_SERVERS}]"

    # Fallback: if no language detected, wire pyright as default
    if [ -z "$MCP_ENTRIES" ]; then
        MCP_ENTRIES="
    \"language-server\": {
      \"command\": \"$LSP_MCP_BIN\",
      \"args\": [\"-workspace\", \"$REPO_ROOT\", \"-lsp\", \"$LSP_PYRIGHT_BIN\", \"--\", \"--stdio\"],
      \"env\": { \"LOG_LEVEL\": \"INFO\" }
    }"
        CODEX_SERVERS="[{\"name\":\"language-server\",\"command\":\"$LSP_MCP_BIN\",\"args\":[\"-workspace\",\"$REPO_ROOT\",\"-lsp\",\"$LSP_PYRIGHT_BIN\",\"--\",\"--stdio\"],\"env\":{\"LOG_LEVEL\":\"INFO\"}}]"
    fi

    if [ -f "$REPO_ROOT/.mcp.json" ] && [ "$FORCE" -eq 0 ]; then
        skip ".mcp.json"
    else
        printf '{\n  "mcpServers": {%s\n  }\n}\n' "$MCP_ENTRIES" > "$REPO_ROOT/.mcp.json"
        wrote ".mcp.json"
    fi

    # .mcp.json contains machine-specific absolute paths — gitignore it
    gitignore_add ".mcp.json"
fi

# ── 10. update ~/.codex/config.toml ───────────────────────────────────────

if [ -n "$LSP_MCP_BIN" ] && [ -n "$CODEX_SERVERS" ] && [ "$CODEX_SERVERS" != "[]" ]; then
    echo ""
    echo "Updating ~/.codex/config.toml..."

    python3 - "$CODEX_SERVERS" "$FORCE" << 'PYEOF'
import sys, json, os

servers = json.loads(sys.argv[1])
force   = sys.argv[2] == "1"

config_path = os.path.expanduser("~/.codex/config.toml")
os.makedirs(os.path.dirname(config_path), exist_ok=True)

content = open(config_path).read() if os.path.exists(config_path) else ""

added = []
for s in servers:
    section = f'[mcp_servers.{s["name"]}]'
    if section in content and not force:
        print(f"  skip   {section} (already exists; --force to overwrite)")
        continue
    if section in content and force:
        # Remove existing block before re-appending
        import re
        content = re.sub(
            rf'\n?{re.escape(section)}[^\[]*', '', content, count=1
        ).rstrip() + "\n"

    args_toml = json.dumps(s["args"])
    env_pairs = ", ".join(f'"{k}" = "{v}"' for k, v in s.get("env", {}).items())
    env_line  = f"\nenv = {{{env_pairs}}}" if env_pairs else ""
    block = f'\n[mcp_servers.{s["name"]}]\ncommand = "{s["command"]}"\ntransport = "stdio"\nargs = {args_toml}{env_line}\n'
    content += block
    added.append(s["name"])

if added:
    with open(config_path, "w") as f:
        f.write(content)
    for name in added:
        print(f"  wrote  ~/.codex/config.toml ← [mcp_servers.{name}]")
PYEOF
fi

# ── done ──────────────────────────────────────────────────────────────────

echo ""
echo "Done. Next steps:"
echo "  ./check-types.sh          # run pyright now"
echo "  ./start-lsp.sh            # verify prereqs (bridge stub)"
[ -n "$LSP_MCP_BIN" ] && \
    echo "  .mcp.json written         # restart Claude Code to pick up the MCP server(s)"
[ "$CODEX" -eq 1 ] && [ -n "$LSP_CODEX_BIN" ] && \
    echo "  codex MCP added           # Codex AI coding agent available via .mcp.json"
[ "$CODEX" -eq 1 ] && [ -z "$LSP_CODEX_BIN" ] && \
    echo "  codex missing             # run: npm install -g @openai/codex, then re-run with --codex"
[ "$HAS_PYTHON" -eq 1 ] && [ "$FORCE" -eq 0 ] && \
    echo "  Review pyrightconfig.json — adjust 'include'/'exclude' for this project"
[ "$HAS_RUST" -eq 1 ] && [ -z "$LSP_RUST_BIN" ] && \
    echo "  rust-analyzer missing     # run: rustup component add rust-analyzer, then re-run"
[ "$HAS_GO" -eq 1 ] && [ -z "$LSP_GOPLS_BIN" ] && \
    echo "  gopls missing             # run: go install golang.org/x/tools/gopls@latest, then re-run"
[ "$HAS_KOTLIN" -eq 1 ] && [ -z "$LSP_KOTLIN_BIN" ] && \
    echo "  kotlin-language-server missing  # see https://github.com/fwcd/kotlin-language-server/releases"
[ "$HAS_SCALA" -eq 1 ] && [ -z "$LSP_METALS_BIN" ] && \
    echo "  metals missing            # see https://scalameta.org/metals/docs/editors/vim#installation"
[ "$HAS_SHELL" -eq 1 ] && [ -z "$LSP_BASH_BIN" ] && \
    echo "  bash-language-server missing  # run: npm install -g bash-language-server, then re-run"
[ "$HAS_YAML" -eq 1 ] && [ -z "$LSP_YAML_BIN" ] && \
    echo "  yaml-language-server missing  # run: npm install -g yaml-language-server, then re-run"
[ "$HAS_TOML" -eq 1 ] && [ -z "$LSP_TAPLO_BIN" ] && \
    echo "  taplo missing                 # run: cargo install taplo-cli, then re-run"
{ [ "$HAS_JSON" -eq 1 ] || [ "$HAS_HTML" -eq 1 ] || [ "$HAS_CSS" -eq 1 ]; } && [ -z "$LSP_JSON_BIN" ] && \
    echo "  vscode-langservers missing    # run: npm install -g vscode-langservers-extracted, then re-run"
echo ""
echo "To regenerate (e.g. after switching Python environments):"
echo "  ./generate-env-lsp.sh"
