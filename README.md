# lsp-bootstrap

A bootstrap kit that wires LSP (Language Server Protocol) tooling into any repo via a custom MCP bridge daemon. Drop in one script, run it, and get type-checking, go-to-definition, hover, call hierarchy, rename, and diagnostics available to Claude Code and opencode — all with machine-specific paths gitignored.

---

## How it works

```
Claude Code / opencode
    │  MCP HTTP (JSON-RPC)
    ▼
lsp-mcp-bridge  (localhost:7890)    ← built here, started by start-lsp.sh
    │  LSP (JSON-RPC over stdio)
    ▼
pyright-langserver / typescript-language-server / gopls / ...
    │  reads
    ▼
workspace files on disk
```

`generate-env-lsp.sh` resolves binary paths on this machine and writes `env.lsp`. `start-lsp.sh` reads `env.lsp` and starts the bridge daemon. Claude Code connects via `.mcp.json`; opencode connects via `.opencode/opencode.json`.

---

## Quickstart

```bash
# One-command bootstrap for any repo:
~/workspace/lsp-bootstrap/bootstrap-repo.sh ~/workspace/my-project --all
# Detects languages, writes config, builds bridge, starts daemon, checks health

# Or step-by-step:
cd ~/workspace/my-project/
~/workspace/lsp-bootstrap/generate-env-lsp.sh --all
~/workspace/lsp-bootstrap/lsp-mcp-bridge/just install
make -f Makefile.lsp lsp-start
make -f Makefile.lsp lsp-health
```

---

## Prerequisites

```bash
# Required — Python and TypeScript LSP
npm install -g pyright typescript-language-server typescript

# Optional — install only what your project needs
rustup component add rust-analyzer          # Rust
go install golang.org/x/tools/gopls@latest  # Go
npm install -g bash-language-server         # Shell
npm install -g yaml-language-server         # YAML
cargo install taplo-cli                     # TOML
npm install -g vscode-langservers-extracted # JSON, HTML, CSS

# Kotlin — download from https://github.com/fwcd/kotlin-language-server/releases
# Scala  — cs install metals
```

Java requires a generated wrapper script — see [Java](#java).

---

## The bridge (`lsp-mcp-bridge`)

### Building

```bash
cd lsp-mcp-bridge
just            # list available commands
just build      # compile → releases/lsp-mcp-bridge
just install    # build + copy to ~/.local/bin  (override: INSTALL_DIR=/your/path just install)
just test
just test-verbose
```

### Starting and stopping

```bash
./start-lsp.sh    # checks prerequisites, starts daemon, writes /tmp/lsp-bridge.pid
./stop-lsp.sh     # sends SIGTERM and removes PID file
```

`start-lsp.sh` is idempotent — running it twice when already started is a no-op.

### Health check

```bash
curl http://localhost:7890/health
```

```json
{
  "status": "ok",
  "uptime": "2m30s",
  "version": "0.1.0",
  "slots": {
    "python":     {"configured": true, "running": true,  "dead": false},
    "typescript": {"configured": true, "running": false, "dead": false}
  }
}
```

Slots are `running: false` until the first tool call triggers lazy LSP startup. `dead: true` means the slot exceeded its failure threshold and will not be retried.

> **Note:** `GET /mcp` hangs — that endpoint only handles MCP JSON-RPC POST requests. Always use `/health` to check liveness.

### MCP tools

Once `.mcp.json` is loaded in Claude Code, the `lsp` server exposes:

| Tool | Description |
|---|---|
| `hover` | Type signature and docs at a file/line/col |
| `definition` | Jump-to-definition location |
| `references` | All locations where a symbol is referenced |
| `diagnostics` | Errors and warnings for a file |
| `rename` | Rename a symbol — returns a unified diff, nothing written to disk |
| `call_hierarchy_in` | All callers of a function |
| `call_hierarchy_out` | All callees of a function |
| `signature_help` | Parameter names and types for a call site |

All tools take `filePath` (absolute path), `line` (1-based), and `column` (1-based) where applicable.

---

## Generator scripts

### `bootstrap-repo.sh` (Recommended)

One-command bootstrap: detects languages, writes config, builds/installs the bridge, starts the daemon, and verifies health — all targeting any repo.

```bash
~/workspace/lsp-bootstrap/bootstrap-repo.sh ~/workspace/my-project --all
~/workspace/lsp-bootstrap/bootstrap-repo.sh ~/workspace/my-project --codex
~/workspace/lsp-bootstrap/bootstrap-repo.sh ~/workspace/my-project --opencode
```

Accepts the same flags as `generate-env-lsp.sh` (`--codex`, `--opencode`, `--all`). Always passes `--force` internally so generated files are overwritten on each run.

### `generate-env-lsp.sh`

Run once per machine (re-run after switching Python envs or upgrading language servers).

```bash
./generate-env-lsp.sh           # detect languages, write env.lsp + scripts + Makefile.lsp + .mcp.json
./generate-env-lsp.sh --force   # overwrite all previously generated files
./generate-env-lsp.sh --codex   # also wire the Codex CLI as an MCP server in .mcp.json
./generate-env-lsp.sh --opencode  # also write .opencode/opencode.json for opencode
./generate-env-lsp.sh --all     # wire Claude Code + Codex + opencode (equivalent to --codex --opencode)
```

Writes:

| File | Committed? | Purpose |
|---|---|---|
| `env.lsp` | No | Machine-specific binary paths |
| `env.custom` | No | Optional local overrides (sourced before `env.lsp`) |
| `.mcp.json` | No | Wires bridge into Claude Code |
| `.opencode/opencode.json` | No | Wires bridge into opencode (with `--opencode`) |
| `start-lsp.sh` | Yes | Starts the bridge daemon |
| `stop-lsp.sh` | Yes | Stops the bridge daemon |
| `check-types.sh` | Yes | Runs pyright over the project |
| `Makefile.lsp` | Yes | Optional make targets (`lsp-start`, `lsp-stop`, `lsp-check`, `lsp-health`) |
| `pyrightconfig.json` | Yes | Python type-check config (scaffolded if missing) |
| `jsconfig.json` | Yes | JS/TS config (scaffolded if missing) |

Language auto-detection:

| Language | Detected by |
|---|---|
| Python | `setup.py`, `pyproject.toml`, `requirements.txt`, or common dirs |
| JavaScript/TypeScript | `package.json`, `jsconfig.json` |
| Rust | `Cargo.toml` |
| Go | `go.mod`, or `*.go` files (up to 3 levels deep) |
| Kotlin | `build.gradle.kts`, `settings.gradle.kts`, `*.kt` |
| Scala | `build.sbt`, `*.scala` |
| Shell | `*.sh`, `Makefile`, `Dockerfile` |
| YAML | `*.yml`, `*.yaml` |
| TOML | `*.toml` |
| JSON | `*.json` (excluding `node_modules`) |
| HTML | `*.html` (excluding `node_modules`) |
| CSS/SCSS/Less | `*.css`, `*.scss`, `*.less` |

### Multiple workspaces

Each workspace gets its own bridge instance. `generate-env-lsp.sh` derives a stable port (in the range `7890–7989`) and a workspace-scoped PID file from the workspace path, so two terminals running their own `start-lsp.sh` will not conflict:

```
workspace/base1  →  port 7927  /tmp/lsp-bridge-base1.pid
workspace/base2  →  port 7928  /tmp/lsp-bridge-base2.pid
```

Each workspace's `.mcp.json` points to its own port, so Claude Code sessions pick up the right bridge automatically.

### `env.lsp` and `env.custom`

`env.lsp` is regenerated — never edit it directly. Put local overrides in `env.custom`:

```bash
# env.custom — gitignored, never committed
LSP_PYTHON=/opt/homebrew/bin/python3.12
LSP_PORT=7891  # manual override if the auto-assigned port conflicts
```

---

## Java

Java uses `jdtls`, which requires JVM flags, a launcher JAR, and a per-workspace data directory. A separate script generates a wrapper that `lsp-mcp-bridge` can launch like any other binary.

```bash
# 1. Install JDK 17+
#    macOS: brew install temurin
#    Linux: apt install openjdk-21-jdk

# 2. Install jdtls
#    macOS: brew install jdtls
#    Linux: download from https://www.eclipse.org/jdtls/

# 3. Generate the wrapper
./generate-java-lsp.sh

# Override jdtls location if non-standard:
JDTLS_HOME_OVERRIDE=/path/to/jdtls ./generate-java-lsp.sh
```

Generates `jdtls-wrapper.sh` (gitignored) and a `.jdtls-data/` workspace index directory. The wrapper is injected into `.mcp.json` alongside other language servers.

---

## Day-to-day

| Command | What it does |
|---|---|
| `bootstrap-repo.sh <repo>` | Full bootstrap for a target repo (recommended) |
| `./start-lsp.sh` | Start the bridge daemon |
| `./stop-lsp.sh` | Stop the bridge daemon |
| `curl localhost:7890/health` | Check bridge liveness and slot status |
| `./check-types.sh` | Run pyright over the whole project |
| `./check-types.sh path/to/file.py` | Check a single file |
| `make -f Makefile.lsp lsp-start` | Start bridge via make |
| `make -f Makefile.lsp lsp-stop` | Stop bridge via make |
| `make -f Makefile.lsp lsp-check ARGS='path/to/file.py'` | Type-check one file via make |
| `make -f Makefile.lsp lsp-health` | Health check using the project's generated port |
| `./generate-env-lsp.sh` | Refresh `env.lsp` and `.mcp.json` after path changes |
| `just install` | Rebuild and reinstall the bridge binary |

---

## Gitignore

The generator appends to `.gitignore` automatically:

```
env.lsp
env.custom
.mcp.json
.opencode/opencode.json
```

Safe to commit: `start-lsp.sh`, `stop-lsp.sh`, `check-types.sh`, `pyrightconfig.json`, `jsconfig.json`. Never commit `env.lsp` or `.mcp.json` — they contain machine-specific absolute paths.

---

## Troubleshooting

### Go not detected

If your Go project isn't detected, check:

1. **`go.mod` exists at repo root** — the primary detection signal
2. **`*.go` files exist within 3 levels** — fallback detection (same as Kotlin/Scala)
3. If neither applies, initialize a module: `go mod init <module-name>`

Then re-run: `./generate-env-lsp.sh --force`
