# lsp-bootstrap

A bootstrap kit that drops LSP (Language Server Protocol) tooling into any repo. Copy one script, run it, and get type-checking, go-to-definition, and MCP-wired language intelligence in Claude Code and Codex — all with machine-specific paths baked in and gitignored.

---

## Overview

Two scripts cover all supported languages:

| Script | Languages |
|---|---|
| `generate-env-lsp.sh` | Python, JavaScript/TypeScript, Rust, Go, Kotlin, Scala |
| `generate-java-lsp.sh` | Java (jdtls requires a generated wrapper — see [Java](#java)) |

`generate-env-lsp.sh` is the main entry point. Drop it into any project directory (e.g. `~/workspace/my-project/`) and run it once. It will:

1. Resolve absolute paths to `python`, `pyright-langserver`, `typescript-language-server`, `rust-analyzer`, `gopls`, `kotlin-language-server`, `metals`, and `mcp-language-server`
2. Write **`env.lsp`** — a machine-specific env file (gitignored)
3. Write **`start-lsp.sh`**, **`stop-lsp.sh`**, **`check-types.sh`** (skip if already present)
4. Scaffold **`pyrightconfig.json`** for Python projects (if missing)
5. Scaffold **`jsconfig.json`** for JS/TS projects (if missing)
6. Write **`.mcp.json`** to wire up `mcp-language-server` into Claude Code (gitignored)
7. Update **`~/.codex/config.toml`** with the same language server entries for Codex
8. Add `env.lsp`, `env.custom`, and `.mcp.json` to `.gitignore`

Pass `--codex` to also wire the Codex CLI itself as an MCP server in `.mcp.json`:

```bash
./generate-env-lsp.sh --codex
```

Re-run any time you switch Python environments (new venv, conda, etc.) or need to refresh paths.

---

## Prerequisites

Install these before running the generator:

```bash
# Core LSP servers (required)
npm install -g pyright typescript-language-server typescript

# Rust LSP (optional — needed for Rust projects)
rustup component add rust-analyzer

# Go LSP (optional — needed for Go projects)
go install golang.org/x/tools/gopls@latest

# Kotlin LSP (optional — needed for Kotlin projects)
# Download kotlin-language-server from:
# https://github.com/fwcd/kotlin-language-server/releases
# Extract and place the `kotlin-language-server` binary on your PATH.

# Scala LSP (optional — needed for Scala projects)
# Install Metals via Coursier:
cs install metals

# MCP bridge (optional — enables .mcp.json and ~/.codex/config.toml generation)
# Option A: go install (requires Go 1.21+)
go install github.com/isaacs/mcp-language-server@latest

# Option B: clone and build
git clone https://github.com/isaacs/mcp-language-server.git
cd mcp-language-server
go build -o mcp-language-server .
# Move to somewhere on your PATH, e.g.:
mv mcp-language-server /usr/local/bin/
cd ..

# Codex CLI (optional — only needed for --codex flag)
npm install -g @openai/codex
```

Check your installs:
```bash
which pyright-langserver
which typescript-language-server
which rust-analyzer          # optional
which gopls                  # optional
which kotlin-language-server # optional
which metals                 # optional
which mcp-language-server    # optional
which codex                  # optional, for --codex flag
```

---

## Quickstart

```bash
# 1. Copy the generator into your target project
cp generate-env-lsp.sh ~/workspace/my-project/
cd ~/workspace/my-project/

# 2. Run it
./generate-env-lsp.sh

# 3. Check types immediately
./check-types.sh

# 4. Restart Claude Code to load .mcp.json (if mcp-language-server was found)
#    ~/.codex/config.toml is updated automatically (no restart needed for Codex)
```

Pass `--force` to overwrite previously generated files:

```bash
./generate-env-lsp.sh --force
```

Pass `--codex` to also add the Codex CLI as an MCP server in `.mcp.json`:

```bash
./generate-env-lsp.sh --codex
```

---

## Generated files

### `env.lsp` (gitignored)

Machine-specific configuration with baked-in absolute paths. Do not commit this file — it is regenerated per machine.

```ini
LSP_PORT=7890
LSP_WORKSPACE=/absolute/path/to/your/project
LSP_PYTHON=/usr/local/bin/python
LSP_PYRIGHT_BIN=/usr/local/bin/pyright-langserver
LSP_TSS_BIN=/usr/local/bin/typescript-language-server
LSP_LOG=/absolute/path/to/your/project/logs/lsp-bridge.log
LSP_PID=/tmp/lsp-bridge.pid
```

### `env.custom` (gitignored, optional)

Create this file to override any `env.lsp` value without modifying the generated file. It is sourced before `env.lsp` by all scripts.

```bash
# env.custom — local overrides, never committed
LSP_PYTHON=/opt/homebrew/bin/python3.12
LSP_PORT=7891
```

### `.mcp.json` (gitignored)

Wires `mcp-language-server` into Claude Code. Contains absolute paths, so it is gitignored. **Restart Claude Code after this file is written** for it to take effect.

One entry is generated per detected language. Example for a Python + Go + Scala project:
```json
{
  "mcpServers": {
    "language-server-python": {
      "command": "/path/to/mcp-language-server",
      "args": ["-workspace", "/path/to/project", "-lsp", "/path/to/pyright-langserver", "--", "--stdio"],
      "env": { "LOG_LEVEL": "INFO" }
    },
    "language-server-go": {
      "command": "/path/to/mcp-language-server",
      "args": ["-workspace", "/path/to/project", "-lsp", "/path/to/gopls"],
      "env": { "LOG_LEVEL": "INFO" }
    },
    "language-server-scala": {
      "command": "/path/to/mcp-language-server",
      "args": ["-workspace", "/path/to/project", "-lsp", "/path/to/metals"],
      "env": { "LOG_LEVEL": "INFO" }
    }
  }
}
```

| Language | Detection | LSP binary | Install |
|---|---|---|---|
| Python | `setup.py`, `pyproject.toml`, `requirements.txt`, common dirs | `pyright-langserver` | `npm i -g pyright` |
| JavaScript/TypeScript | `package.json`, `jsconfig.json` | `typescript-language-server` | `npm i -g typescript-language-server` |
| Rust | `Cargo.toml` | `rust-analyzer` | `rustup component add rust-analyzer` |
| Go | `go.mod` | `gopls` | `go install golang.org/x/tools/gopls@latest` |
| Kotlin | `build.gradle.kts`, `settings.gradle.kts`, `*.kt` | `kotlin-language-server` | [GitHub releases](https://github.com/fwcd/kotlin-language-server/releases) |
| Scala | `build.sbt`, `*.scala` | `metals` | `cs install metals` |

Mixed projects get all relevant entries. LSP servers read their native build files (`Cargo.toml`, `go.mod`, `build.sbt`, etc.) directly — no extra config needed.

---

## Java

Java uses `jdtls` (Eclipse JDT Language Server), which cannot be wired directly — it requires JVM flags, a launcher JAR, an OS-specific config directory, and a per-workspace data directory. A separate script handles this.

### Why a separate script

`mcp-language-server` takes a single `-lsp <binary>` argument. `jdtls` is a JAR invoked with ~10 JVM flags. The solution is a generated `jdtls-wrapper.sh` with all paths baked in, which `mcp-language-server` then treats as a plain binary.

### Setup

```bash
# 1. Install JDK 17+ if needed
#    macOS:  brew install temurin   (or adoptium.net)
#    Linux:  apt install openjdk-21-jdk

# 2. Install jdtls
#    macOS:  brew install jdtls
#    Linux:  download from https://www.eclipse.org/jdtls/ and extract to ~/.local/share/jdtls

# 3. Run the generator
./generate-java-lsp.sh
```

If jdtls is installed in a non-standard location, set `JDTLS_HOME_OVERRIDE`:

```bash
JDTLS_HOME_OVERRIDE=/path/to/jdtls ./generate-java-lsp.sh
```

### What it generates

| File | Purpose | Committed? |
|---|---|---|
| `jdtls-wrapper.sh` | Invokes jdtls with baked-in JVM flags and paths | No (gitignored) |
| `.jdtls-data/` | Per-workspace index written by jdtls at runtime | No (gitignored) |

The wrapper is injected into `.mcp.json` and `~/.codex/config.toml` alongside any entries already written by `generate-env-lsp.sh`. Re-run with `--force` after upgrading jdtls or Java.

When `--codex` is passed and the `codex` binary is found, an additional entry is added to `.mcp.json` only (not to `~/.codex/config.toml` — Codex does not need itself as a server):

```json
"codex": {
  "command": "/path/to/codex",
  "args": ["--mcp-server"],
  "env": {}
}
```

### `~/.codex/config.toml` (global, not committed)

Updated automatically whenever `mcp-language-server` is found — no `--codex` flag required. The same language server entries written to `.mcp.json` are appended to your global Codex config in TOML format:

```toml
[mcp_servers.language-server-python]
command = "/path/to/mcp-language-server"
transport = "stdio"
args = ["-workspace", "/path/to/project", "-lsp", "/path/to/pyright-langserver", "--", "--stdio"]
env = {"LOG_LEVEL" = "INFO"}
```

Existing entries are skipped unless `--force` is passed. Because this file is global (`~/.codex/`), it is not gitignored — each machine manages its own copy.

### `pyrightconfig.json` (committed)

Scaffolded for Python projects. Review the `include` list — the generator tries to detect top-level packages but may need adjustment:

```json
{
  "include": ["src"],
  "exclude": ["node_modules", "**/__pycache__", ".git", ".mypy_cache"],
  "pythonVersion": "3.11",
  "pythonPlatform": "Linux",
  "typeCheckingMode": "basic",
  "reportMissingImports": true,
  "reportMissingTypeStubs": false,
  "executionEnvironments": [{ "root": "." }]
}
```

### `jsconfig.json` (committed)

Scaffolded for JS/TS projects. Adjust `paths` aliases as needed.

---

## Day-to-day usage

| Command | What it does |
|---|---|
| `./generate-env-lsp.sh` | Re-resolve paths and regenerate `env.lsp`, `.mcp.json`, `~/.codex/config.toml` |
| `./generate-env-lsp.sh --codex` | Also wire the Codex CLI as an MCP server in `.mcp.json` |
| `./generate-env-lsp.sh --force` | Overwrite all generated files (including scripts) |
| `./check-types.sh` | Run pyright over the whole project |
| `./check-types.sh path/to/file.py` | Check a single file |
| `./check-types.sh --outputjson . \| jq` | JSON output for tooling |
| `./start-lsp.sh` | Verify prerequisites (bridge not yet built — see Phase 3) |
| `./stop-lsp.sh` | Stop the LSP bridge daemon |

---

## Runtime behaviour

### Architecture

```
Claude Code / Codex
      │  MCP (JSON-RPC over stdio)
      ▼
mcp-language-server          ← MCP server process (one per language)
      │  LSP (JSON-RPC over stdio)
      ▼
gopls / pyright / rust-analyzer / tsserver
      │  reads
      ▼
workspace files on disk
```

Each language gets its own `mcp-language-server` process. That process owns a single LSP child process and translates between the MCP tool protocol and LSP.

### MCP connection

Claude Code (or Codex) reads `.mcp.json` / `~/.codex/config.toml` at startup and spawns each listed server as a subprocess connected via stdio. The agents calls MCP tools (`definition`, `references`, `hover`, `diagnostics`, etc.); `mcp-language-server` forwards them as LSP requests and returns the results.

### Data lifecycle

1. **File open** — the LSP server is notified of the workspace root at startup; it indexes files lazily on first request.
2. **Request** — agents calls an MCP tool (e.g. `hover` at a symbol). `mcp-language-server` sends `textDocument/hover` to the LSP process.
3. **Response** — LSP replies with structured data (type info, location, diagnostics). `mcp-language-server` converts it to MCP JSON and returns it to the agents.
4. **No persistent state in the bridge** — `mcp-language-server` is stateless beyond the stdio pipe; all language intelligence lives in the LSP process. Restarting Claude Code kills and respawns the entire chain.

### Available MCP tools

Once `.mcp.json` is loaded, the agent has access to these tools:

| Tool | What it returns |
|---|---|
| `hover` | Type signature and docstring at a file/line/col position |
| `definition` | Complete source of a named symbol |
| `references` | Every file and location where a named symbol appears |
| `diagnostics` | Type errors, warnings, and missing imports for a file |
| `document_symbols` | All symbols (functions, types, variables) defined in a file |
| `rename_symbol` | Renames a symbol and updates all references across the project |
| `incoming_calls` | All callers of a function at a given position |
| `outgoing_calls` | All functions called by a function at a given position |
| `find_implementations` | All concrete implementations of an interface or abstract method |

> **Note on `rename_symbol`:** this applies changes immediately — it does not produce a preview diff. Review the call sites with `references` first.

---

## Project phases

| Phase | Status | Description |
|---|---|---|
| 1 | Done | Design env contract, script skeleton |
| 2 | Done | `generate-env-lsp.sh`, `check-types.sh`, `start-lsp.sh`, `.mcp.json` |
| 3 | Pending | Build `lsp_mcp_bridge` daemon; replace stub in `start-lsp.sh` |

Until Phase 3 is complete, `start-lsp.sh` validates prerequisites but does not start a daemon. Use `./check-types.sh` and the MCP-wired `.mcp.json` (via Claude Code) in the meantime.

---

## Applying to a new project

```bash
# From this repo
cp generate-env-lsp.sh ~/workspace/MyNewProject/
cd ~/workspace/MyNewProject/

# Activate the right Python env first if needed
source .venv/bin/activate        # venv
# conda activate myenv           # conda

./generate-env-lsp.sh
```

The generator auto-detects project types:
- **Python** — `setup.py`, `pyproject.toml`, `requirements.txt`, or common dirs (`src/`, `backends/`, `server/`)
- **JavaScript/TypeScript** — `package.json` or `jsconfig.json`
- **Rust** — `Cargo.toml`
- **Go** — `go.mod`
- **Kotlin** — `build.gradle.kts`, `settings.gradle.kts`, or any `*.kt` file (up to 3 dirs deep)
- **Scala** — `build.sbt` or any `*.scala` file (up to 3 dirs deep)

---

## Gitignore behaviour

The generator appends to `.gitignore` automatically:

```
env.lsp
env.custom
.mcp.json
```

Commit `pyrightconfig.json`, `jsconfig.json`, and the shell scripts — they are safe to share. Never commit `env.lsp` or `.mcp.json` (they contain machine-specific absolute paths).
