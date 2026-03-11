# Top-Level Assumptions

High-level assumptions underpinning the LSP MCP bridge project. If any of these change, revisit the affected documents before continuing.

---

## Dependencies

### External Go modules
| Module | Version | Purpose |
|---|---|---|
| `github.com/mark3labs/mcp-go` | v0.45.0 | MCP server, HTTP transport, tool registration |
| `github.com/sergi/go-diff/diffmatchpatch` | v1.4.0 | Line-mode diff for `rename` dry-run output |

### Language server binaries (host-installed, resolved at runtime)
| Binary | Language | Required |
|---|---|---|
| `pyright-langserver` | Python | Yes |
| `typescript-language-server` | JS/TS | Yes |
| `rust-analyzer` | Rust | Optional |
| `gopls` | Go | Optional |
| `kotlin-language-server` | Kotlin | Optional |
| `metals` | Scala | Optional |
| `bash-language-server` | Shell | Optional |
| `yaml-language-server` | YAML | Optional |
| `taplo` | TOML | Optional |
| `vscode-json-language-server` | JSON | Optional |
| `vscode-html-language-server` | HTML | Optional |
| `vscode-css-language-server` | CSS | Optional |

All binaries are resolved at bootstrap time by `generate-env-lsp.sh` and written to `env.lsp`. The bridge reads `env.lsp` at startup — it does not shell out to `which`.

### Toolchain
- **Go 1.22+** — required to build the bridge
- **Python 3.11+** — present on host (used by `generate-env-lsp.sh`'s codex config writer, not by the bridge itself)
- **Node.js** — present on host (required by npm-distributed LSP servers; not a bridge dependency)

---

## Project Architecture

### Shape
Single compiled Go binary (`lsp-mcp-bridge`) acting as an HTTP MCP server. It owns one persistent subprocess per language slot, multiplexes JSON-RPC over stdio, and translates MCP tool calls into LSP requests.

### Process model
```
Claude Code / Codex  ──HTTP POST /mcp──►  lsp-mcp-bridge (daemon)
                                               │
                              ┌────────────────┼────────────────┐
                              ▼                ▼                ▼
                         pyright          gopls            tsserver
                        (stdio)          (stdio)           (stdio)
```

- One bridge process, shared across Claude Code sessions
- One LSP subprocess per language slot, lazy-started on first use
- No shared state between tool calls beyond the live LSP processes

### Files
```
lsp-mcp-bridge/
  main.go        — entry point, server wiring
  config.go      — env.lsp / env.custom / CLI flag loading
  lsp_client.go  — subprocess + JSON-RPC stdio client
  manager.go     — slot routing, lazy-start, crash restart, shutdown
  tools_3a.go    — hover, definition, references, diagnostics
  tools_3b.go    — rename (dry-run), call_hierarchy_in/out, signature_help
  util.go        — URI/path conversion, position encoding, diff, languageID map
  testutil/      — mock LSP server, fixtures, test helpers
```

---

## Test Areas

### Unit (no subprocess, no network)
- `config.go` — env file parsing, override layering, path-with-spaces
- `util.go` — URI round-trip, position encoding, `languageID` edge cases, diff correctness, no disk writes
- `lsp_client.go` — JSON-RPC multiplexing, notification ordering, EOF drain (mock LSP server)
- `manager.go` — routing, lazy-start, concurrent same-slot, crash restart, restart limit, shutdown (mock LSP server)

### Integration (`//go:build integration`, real LSP binaries required)
- `tools_3a.go` — hover type correctness, diagnostics error detection, clean file empty result, notification ordering under real server
- `tools_3b.go` — rename diff correctness, no disk writes, call hierarchy, signature help

### End-to-end (`//go:build integration`)
- Full HTTP round-trip via POST to `/mcp`
- Concurrent requests, same and different languages
- Crash recovery mid-request
- Multiple `publishDiagnostics` batch merge

### Explicit non-goals for testing
- Testing the LSP servers themselves (pyright, gopls, etc.) — assumed correct
- Testing `generate-env-lsp.sh` — outside bridge scope
- Load / performance testing — Phase 4 concern

---

## IO Needs

### Network
- **Inbound:** HTTP on `localhost:7890` (MCP HTTP transport). Loopback only — no external exposure assumed.
- **Outbound:** none. The bridge does not make outgoing network calls.

### Disk
- **Read:** `env.lsp`, `env.custom` at startup; workspace source files per tool call (for `didOpen` text and `WorkspaceEditToDiff`)
- **Write:** `env.lsp`, `.mcp.json` (written by `generate-env-lsp.sh`, not the bridge); log file (`LSP_LOG`); PID file (`LSP_PID`)
- **Never writes:** workspace source files — the bridge is strictly read-only on user code

### Subprocess stdio
- One read goroutine + one write path per LSP subprocess
- No PTY — plain stdin/stdout pipes
- stderr of LSP processes is discarded or redirected to the bridge log (configurable)

### Database
- None. No persistent storage beyond the log file.

### State
- In-memory only: `pending` map, notification listeners, slot map
- All state is lost on bridge restart — LSP processes are also restarted, re-indexing from scratch

---

## Build System

- **`go build`** — single command, no Makefile required for basic builds
- **`go test ./...`** — unit tests; integration tests excluded without `-tags integration`
- **`go test -tags integration ./...`** — full test suite, requires language server binaries on PATH
- **No generated code** — no `go generate`, no protobuf, no wire
- Output binary: `lsp-mcp-bridge/lsp-mcp-bridge` (or platform-appropriate name)
- The bridge is built and placed in the repo alongside `generate-env-lsp.sh`; it is not distributed separately

---

## Deployment Method

- **Host-local daemon** — started by `start-lsp.sh`, stopped by `stop-lsp.sh`
- PID tracked in `LSP_PID` (default `/tmp/lsp-bridge.pid`)
- Log written to `LSP_LOG` (default `<repo>/logs/lsp-bridge.log`)
- Not containerised — runs directly on the developer's machine with access to the full workspace filesystem and installed language runtimes
- No process supervisor (systemd, launchd) — manual start/stop via the shell scripts is sufficient for local dev tooling
- One instance per workspace — multiple workspaces run separate bridge instances on different ports

### When to revisit
- Remote dev environment → add Docker + volume mounts for workspace + venv
- Team shared tooling server → add process supervisor and network binding

---

## Runtime Goal

- **Latency:** first tool call on a cold slot (LSP not yet started) may take 2–8 seconds (indexing). Subsequent calls within a session target <500ms.
- **Memory:** ~80–400 MB per live LSP process (language-dependent); bridge itself <20 MB. Acceptable on 16 GB+ machines.
- **Concurrency:** tool calls are serialised per language slot (one JSON-RPC connection per LSP process). Concurrent calls to different slots proceed in parallel.
- **Availability:** best-effort. Crash-restart with a 3-attempt limit. No HA, no replication.
- **Correctness over speed:** the bridge is a dev tool. Returning accurate results matters more than sub-100ms latency.
- **No authentication:** loopback-only, single-user, local machine. No auth layer assumed or planned.
