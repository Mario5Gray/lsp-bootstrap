#!/usr/bin/env bash
# generate-java-lsp.sh — bootstrap jdtls (Eclipse JDT Language Server) for a Java project.
#
# jdtls cannot be wired directly into mcp-language-server because it requires
# JVM flags, a launcher JAR glob, an OS-specific config dir, and a per-workspace
# data directory. This script resolves all of that and writes a jdtls-wrapper.sh,
# then wires the wrapper into .mcp.json and ~/.codex/config.toml.
#
# Copy this script alongside generate-env-lsp.sh and run it once per project.
# Pass --force to overwrite existing generated files.
set -euo pipefail

FORCE=0
for arg in "$@"; do
    [ "$arg" = "--force" ] && FORCE=1
done

REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
cd "$REPO_ROOT"

# ── helpers ────────────────────────────────────────────────────────────────

ok()   { printf "  \033[32mok\033[0m     %s\n" "$*"; }
miss() { printf "  \033[31mMISSING\033[0m %s\n" "$*"; }
skip() { printf "  \033[33mskip\033[0m   %s (already exists; --force to overwrite)\n" "$*"; }
wrote(){ printf "  \033[36mwrote\033[0m  %s\n" "$*"; }
warn() { printf "  \033[33mwarn\033[0m   %s\n" "$*"; }

write_file() {
    local path="$1"; shift
    if [ -f "$path" ] && [ "$FORCE" -eq 0 ]; then
        skip "$path"
        return
    fi
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

# ── 1. resolve java ────────────────────────────────────────────────────────

echo ""
echo "Resolving Java runtime..."

JAVA_BIN="$(which java 2>/dev/null || true)"
if [ -z "$JAVA_BIN" ]; then
    miss "java — install JDK 17+ (https://adoptium.net) and ensure it is on PATH"
    exit 1
fi

JAVA_VERSION="$("$JAVA_BIN" -version 2>&1 | head -1)"
ok "java → $JAVA_BIN ($JAVA_VERSION)"

# jdtls requires Java 17+
JAVA_MAJOR="$("$JAVA_BIN" -version 2>&1 | grep -oE '"[0-9]+' | head -1 | tr -d '"')"
if [ -n "$JAVA_MAJOR" ] && [ "$JAVA_MAJOR" -lt 17 ]; then
    miss "Java 17+ required (found $JAVA_MAJOR) — install a newer JDK"
    exit 1
fi

# ── 2. locate jdtls installation ───────────────────────────────────────────

echo ""
echo "Locating jdtls..."

JDTLS_HOME=""

# Common install locations — checked in order
CANDIDATES=(
    "$HOME/.local/share/jdtls"
    "/usr/local/share/jdtls"
    "/opt/jdtls"
    "/usr/share/jdtls"
)

# Homebrew (macOS)
if command -v brew &>/dev/null; then
    BREW_PREFIX="$(brew --prefix 2>/dev/null || true)"
    CANDIDATES+=("$BREW_PREFIX/share/jdtls")
fi

# JDTLS_HOME env override
[ -n "${JDTLS_HOME_OVERRIDE:-}" ] && CANDIDATES=("$JDTLS_HOME_OVERRIDE" "${CANDIDATES[@]}")

for candidate in "${CANDIDATES[@]}"; do
    if [ -d "$candidate/plugins" ]; then
        JDTLS_HOME="$candidate"
        ok "jdtls → $JDTLS_HOME"
        break
    fi
done

if [ -z "$JDTLS_HOME" ]; then
    miss "jdtls not found in any of: ${CANDIDATES[*]}"
    echo ""
    echo "Install jdtls, then re-run:"
    echo "  # Option A — homebrew (macOS)"
    echo "  brew install jdtls"
    echo ""
    echo "  # Option B — manual"
    echo "  mkdir -p ~/.local/share/jdtls"
    echo "  curl -L https://www.eclipse.org/downloads/download.php?file=/jdtls/milestones/\$(curl -s https://www.eclipse.org/jdtls/download/?c=1 | grep -oP 'jdtls_\K[^\"]+(?=.tar.gz)' | head -1).tar.gz | tar xz -C ~/.local/share/jdtls"
    echo ""
    echo "  # Option C — set JDTLS_HOME_OVERRIDE before running this script"
    echo "  JDTLS_HOME_OVERRIDE=/path/to/jdtls ./generate-java-lsp.sh"
    exit 1
fi

# Find the launcher JAR (version number in filename)
LAUNCHER_JAR="$(ls "$JDTLS_HOME/plugins/org.eclipse.equinox.launcher_"*.jar 2>/dev/null | sort -V | tail -1 || true)"
if [ -z "$LAUNCHER_JAR" ]; then
    miss "launcher JAR not found in $JDTLS_HOME/plugins/"
    exit 1
fi
ok "launcher → $(basename "$LAUNCHER_JAR")"

# OS-specific config directory
case "$(uname -s)" in
    Darwin) JDTLS_CONFIG="$JDTLS_HOME/config_mac" ;;
    Linux)  JDTLS_CONFIG="$JDTLS_HOME/config_linux" ;;
    MINGW*|MSYS*|CYGWIN*) JDTLS_CONFIG="$JDTLS_HOME/config_win" ;;
    *)      JDTLS_CONFIG="$JDTLS_HOME/config_linux" ;;
esac

if [ ! -d "$JDTLS_CONFIG" ]; then
    miss "config dir not found: $JDTLS_CONFIG"
    exit 1
fi
ok "config → $JDTLS_CONFIG"

# ── 3. locate mcp-language-server ─────────────────────────────────────────

echo ""
echo "Resolving mcp-language-server..."

LSP_MCP_BIN="$(which mcp-language-server 2>/dev/null || true)"
if [ -z "$LSP_MCP_BIN" ]; then
    warn "mcp-language-server not found — .mcp.json will be skipped"
    warn "Install: go install github.com/isaacs/mcp-language-server@latest"
else
    ok "mcp-language-server → $LSP_MCP_BIN"
fi

# ── 4. workspace data directory ────────────────────────────────────────────

# jdtls stores its index here — gitignored, never committed
JDTLS_DATA="$REPO_ROOT/.jdtls-data"
mkdir -p "$JDTLS_DATA"
gitignore_add ".jdtls-data"

# ── 5. write jdtls-wrapper.sh ─────────────────────────────────────────────

echo ""
echo "Writing jdtls-wrapper.sh..."

WRAPPER="$REPO_ROOT/jdtls-wrapper.sh"

write_file "$WRAPPER" << EOF
#!/usr/bin/env bash
# jdtls-wrapper.sh — invoke Eclipse JDT Language Server with baked-in paths.
# Generated by generate-java-lsp.sh. Re-run generator to update.
# This wrapper is consumed by mcp-language-server via .mcp.json.
exec "$JAVA_BIN" \\
  -Declipse.application=org.eclipse.jdt.ls.core.id1 \\
  -Dosgi.bundles.defaultStartLevel=4 \\
  -Declipse.product=org.eclipse.jdt.ls.core.product \\
  -Dlog.level=ALL \\
  -Xmx1G \\
  --add-modules=ALL-SYSTEM \\
  --add-opens java.base/java.util=ALL-UNNAMED \\
  --add-opens java.base/java.lang=ALL-UNNAMED \\
  -jar "$LAUNCHER_JAR" \\
  -configuration "$JDTLS_CONFIG" \\
  -data "$JDTLS_DATA" \\
  "\$@"
EOF

# wrapper contains absolute paths — gitignore it
gitignore_add "jdtls-wrapper.sh"

# ── 6. write / update .mcp.json ───────────────────────────────────────────

if [ -n "$LSP_MCP_BIN" ]; then
    echo ""
    echo "Updating .mcp.json..."

    MCP_JSON="$REPO_ROOT/.mcp.json"
    NEW_ENTRY="\"language-server-java\": {
      \"command\": \"$LSP_MCP_BIN\",
      \"args\": [\"-workspace\", \"$REPO_ROOT\", \"-lsp\", \"$WRAPPER\"],
      \"env\": { \"LOG_LEVEL\": \"INFO\" }
    }"

    if [ -f "$MCP_JSON" ]; then
        # Inject into existing .mcp.json if key not already present
        if grep -q "language-server-java" "$MCP_JSON" && [ "$FORCE" -eq 0 ]; then
            skip ".mcp.json (language-server-java already present)"
        else
            # Use python to safely merge the new entry
            python3 - "$MCP_JSON" "$LSP_MCP_BIN" "$REPO_ROOT" "$WRAPPER" "$FORCE" << 'PYEOF'
import sys, json

path, mcp_bin, workspace, wrapper, force = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4], sys.argv[5] == "1"

with open(path) as f:
    data = json.load(f)

servers = data.setdefault("mcpServers", {})

if "language-server-java" in servers and not force:
    print("  skip   language-server-java (already in .mcp.json; --force to overwrite)")
    sys.exit(0)

servers["language-server-java"] = {
    "command": mcp_bin,
    "args": ["-workspace", workspace, "-lsp", wrapper],
    "env": {"LOG_LEVEL": "INFO"}
}

with open(path, "w") as f:
    json.dump(data, f, indent=2)
    f.write("\n")

print("  wrote  .mcp.json ← language-server-java")
PYEOF
        fi
    else
        # No existing .mcp.json — create one
        printf '{\n  "mcpServers": {\n    %s\n  }\n}\n' "$NEW_ENTRY" > "$MCP_JSON"
        wrote ".mcp.json"
    fi

    gitignore_add ".mcp.json"

    # ── 7. update ~/.codex/config.toml ────────────────────────────────────

    echo ""
    echo "Updating ~/.codex/config.toml..."

    python3 - "$LSP_MCP_BIN" "$REPO_ROOT" "$WRAPPER" "$FORCE" << 'PYEOF'
import sys, os, re

mcp_bin, workspace, wrapper, force = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4] == "1"

config_path = os.path.expanduser("~/.codex/config.toml")
os.makedirs(os.path.dirname(config_path), exist_ok=True)
content = open(config_path).read() if os.path.exists(config_path) else ""

section = "[mcp_servers.language-server-java]"
if section in content and not force:
    print(f"  skip   {section} (already exists; --force to overwrite)")
    sys.exit(0)

if section in content and force:
    content = re.sub(rf'\n?{re.escape(section)}[^\[]*', '', content, count=1).rstrip() + "\n"

args = ["-workspace", workspace, "-lsp", wrapper]
import json
block = f'\n[mcp_servers.language-server-java]\ncommand = "{mcp_bin}"\ntransport = "stdio"\nargs = {json.dumps(args)}\nenv = {{"LOG_LEVEL" = "INFO"}}\n'
content += block

with open(config_path, "w") as f:
    f.write(content)

print(f"  wrote  ~/.codex/config.toml ← [mcp_servers.language-server-java]")
PYEOF

fi

# ── done ──────────────────────────────────────────────────────────────────

echo ""
echo "Done. Next steps:"
echo "  jdtls-wrapper.sh is gitignored (contains absolute paths)"
[ -n "$LSP_MCP_BIN" ] && \
    echo "  Restart Claude Code to pick up the updated .mcp.json"
echo ""
echo "To regenerate (e.g. after upgrading jdtls or Java):"
echo "  ./generate-java-lsp.sh"
echo "  ./generate-java-lsp.sh --force   # overwrite wrapper and re-inject .mcp.json"
