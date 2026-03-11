# lsp-bootstrap

A single-file bootstrap kit that drops LSP (Language Server Protocol) tooling into any repo. Copy one script, run it, and get type-checking, go-to-definition, and MCP-wired language intelligence in Claude Code — all with machine-specific paths baked in and gitignored.

---

## Overview

`generate-env-lsp.sh` is the entry point. Drop it into any project directory (e.g. `~/workspace/Stability-Toys/`) and run it once. It will:

1. Resolve absolute paths to `python`, `pyright-langserver`, `typescript-language-server`, and `mcp-language-server`
2. Write **`env.lsp`** — a machine-specific env file (gitignored)
3. Write **`start-lsp.sh`**, **`stop-lsp.sh`**, **`check-types.sh`** (skip if already present)
4. Scaffold **`pyrightconfig.json`** for Python projects (if missing)
5. Scaffold **`jsconfig.json`** for JS/TS projects (if missing)
6. Write **`.mcp.json`** to wire up `mcp-language-server` into Claude Code (gitignored)
7. Add `env.lsp`, `env.custom`, and `.mcp.json` to `.gitignore`

Re-run any time you switch Python environments (new venv, conda, etc.) or need to refresh paths.

---

## Prerequisites

Install these before running the generator:

```bash
# Core LSP servers (required)
npm install -g pyright typescript-language-server typescript

# Rust LSP (optional — needed for Rust projects)
rustup component add rust-analyzer

# MCP bridge (optional — enables .mcp.json generation)
# Option A: go install (requires Go 1.21+)
go install github.com/isaacs/mcp-language-server@latest

# Option B: clone and build
git clone https://github.com/isaacs/mcp-language-server.git
cd mcp-language-server
go build -o mcp-language-server .
# Move to somewhere on your PATH, e.g.:
mv mcp-language-server /usr/local/bin/
cd ..
```

Check your installs:
```bash
which pyright-langserver
which typescript-language-server
which rust-analyzer          # optional
which mcp-language-server    # optional
```

---

## Quickstart

```bash
# 1. Copy the generator into your target project
cp generate-env-lsp.sh ~/workspace/Stability-Toys/
cd ~/workspace/Stability-Toys/

# 2. Run it
./generate-env-lsp.sh

# 3. Check types immediately
./check-types.sh

# 4. Restart Claude Code to load .mcp.json (if mcp-language-server was found)
```

Pass `--force` to overwrite previously generated files:

```bash
./generate-env-lsp.sh --force
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

One entry is generated per detected language. Example for a Python + Rust project:
```json
{
  "mcpServers": {
    "language-server-python": {
      "command": "/path/to/mcp-language-server",
      "args": ["-workspace", "/path/to/project", "-lsp", "/path/to/pyright-langserver", "--", "--stdio"],
      "env": { "LOG_LEVEL": "INFO" }
    },
    "language-server-rust": {
      "command": "/path/to/mcp-language-server",
      "args": ["-workspace", "/path/to/project", "-lsp", "/path/to/rust-analyzer"],
      "env": { "LOG_LEVEL": "INFO" }
    }
  }
}
```

| Language | Detection | LSP binary |
|---|---|---|
| Python | `setup.py`, `pyproject.toml`, `requirements.txt`, common dirs | `pyright-langserver` |
| JavaScript/TypeScript | `package.json`, `jsconfig.json` | `typescript-language-server` |
| Rust | `Cargo.toml` | `rust-analyzer` |

Mixed projects (e.g. Python backend + Rust extension) get all relevant entries. No extra config file is needed for Rust — `rust-analyzer` reads `Cargo.toml` natively.

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
| `./generate-env-lsp.sh` | Re-resolve paths and regenerate `env.lsp` + `.mcp.json` |
| `./generate-env-lsp.sh --force` | Overwrite all generated files (including scripts) |
| `./check-types.sh` | Run pyright over the whole project |
| `./check-types.sh path/to/file.py` | Check a single file |
| `./check-types.sh --outputjson . \| jq` | JSON output for tooling |
| `./start-lsp.sh` | Verify prerequisites (bridge not yet built — see Phase 3) |
| `./stop-lsp.sh` | Stop the LSP bridge daemon |

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

The generator auto-detects Python projects (looks for `setup.py`, `pyproject.toml`, `requirements.txt`, or common dirs like `src/`, `backends/`, `server/`) and JS projects (`package.json`).

---

## Gitignore behaviour

The generator appends to `.gitignore` automatically:

```
env.lsp
env.custom
.mcp.json
```

Commit `pyrightconfig.json`, `jsconfig.json`, and the shell scripts — they are safe to share. Never commit `env.lsp` or `.mcp.json` (they contain machine-specific absolute paths).
