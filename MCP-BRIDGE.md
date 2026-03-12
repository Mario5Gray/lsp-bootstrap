# LSP MCP Bridge

A lightweight HTTP MCP server that routes tool calls to the correct language server process by file extension. Replaces the per-language `mcp-language-server` stdio entries in `.mcp.json` with a single persistent HTTP endpoint.

Implements the Phase 3 bridge described in `PLAN_language_server_mcp.md`.

---

## Why Go

- Go is already a prerequisite (`mcp-language-server` install)
- Compiles to a single binary — no venv, no Python path, no runtime dependency
- stdlib covers subprocess management, HTTP, JSON-RPC, and sync primitives
- Goroutines map naturally to per-client stdio reader loops

---

## Dependencies

| Module | Purpose |
|---|---|
| `github.com/mark3labs/mcp-go` | MCP server + HTTP transport |
| `github.com/sergi/go-diff/diffmatchpatch` | Unified diff for `rename` dry-run — use `DiffLinesToChars` + `DiffMain` + `DiffCharsToLines` + `PatchMake` + `PatchToText`; no `DiffLines` function exists |

Everything else is stdlib: `os/exec`, `bufio`, `net/http`, `encoding/json`, `sync`, `path/filepath`.

---

## File Structure

```
lsp-mcp-bridge/
  main.go        # entry point: parse CLI flags, load Config, build Manager, start HTTP server
  config.go      # reads env.lsp then env.custom into a Config struct; CLI flags override
  lsp_client.go  # one subprocess per language; JSON-RPC multiplexed by id; reader goroutine
  manager.go     # extension → slot routing; lazy-start; crash restart; clean shutdown
  tools_3a.go    # hover, definition, references, diagnostics — registered in main.go
  tools_3b.go    # rename, call_hierarchy_in, call_hierarchy_out, signature_help — registered in main.go
  util.go        # PathToURI, URIToPath, MakePosition, WorkspaceEditToDiff, extensionToLanguageID
  go.mod
  go.sum
```

`bridge.go` from earlier drafts is removed — its responsibilities (MCP server construction and tool registration) belong in `main.go`. Keeping them separate added indirection without benefit.

---

## Extension Routing Table

Defined as a constant map in `manager.go`. Each slot maps to a binary path sourced from `Config`.

| Extension(s) | Slot | Binary (from env.lsp) | Args |
|---|---|---|---|
| `.py` | `python` | `LSP_PYRIGHT_BIN` | `["--stdio"]` |
| `.js` `.jsx` `.ts` `.tsx` `.mjs` | `typescript` | `LSP_TSS_BIN` | `["--stdio"]` |
| `.rs` | `rust` | `LSP_RUST_BIN` | `[]` |
| `.go` | `go` | `LSP_GOPLS_BIN` | `[]` |
| `.kt` `.kts` | `kotlin` | `LSP_KOTLIN_BIN` | `[]` |
| `.scala` `.sc` | `scala` | `LSP_METALS_BIN` | `[]` |
| `.sh` `.bash` `.zsh` | `shell` | `LSP_BASH_BIN` | `["--stdio"]` |
| `.yml` `.yaml` | `yaml` | `LSP_YAML_BIN` | `["--stdio"]` |
| `.toml` | `toml` | `LSP_TAPLO_BIN` | `["lsp", "stdio"]` |
| `.json` `.jsonc` | `json` | `LSP_JSON_BIN` | `["--stdio"]` |
| `.html` `.htm` | `html` | `LSP_HTML_BIN` | `["--stdio"]` |
| `.css` `.scss` `.less` | `css` | `LSP_CSS_BIN` | `["--stdio"]` |

If a slot's binary is missing from `Config`, the tool returns an error naming the binary and its install command rather than silently returning empty results.

---

## Architecture

```
Claude Code
    │  MCP HTTP  POST /mcp
    ▼
lsp-mcp-bridge  (localhost:7890)
    │
    ├── manager.get_client(".py")  ──► pyright-langserver  (stdio, persistent)
    ├── manager.get_client(".go")  ──► gopls               (stdio, persistent)
    ├── manager.get_client(".ts")  ──► tsserver            (stdio, persistent)
    └── ...
```

One process per language slot, started lazily on first use and kept alive across tool calls. The bridge is a separate daemon from Claude Code — multiple sessions share it without re-indexing.

---

## Tool Surface

### Phase 3a — MVP

| MCP Tool | LSP Method | Input | Output |
|---|---|---|---|
| `hover` | `textDocument/hover` | `filePath`, `line`, `column` | plain text string: type signature + doc comment |
| `definition` | `textDocument/definition` | `filePath`, `line`, `column` | `[{filePath, line, column}]` — absolute paths, 1-based; multiple results for overloads |
| `references` | `textDocument/references` | `filePath`, `line`, `column` | `[{filePath, line, column}]` — absolute paths, 1-based |
| `diagnostics` | `textDocument/publishDiagnostics` | `filePath` | `[{severity, message, line, column}]` — severity: `"error"`, `"warning"`, `"info"`, `"hint"` |

### Phase 3b — Extended

| MCP Tool | LSP Method | Input | Output |
|---|---|---|---|
| `rename` | `textDocument/rename` | `filePath`, `line`, `column`, `newName` | unified diff string (one diff block per affected file, no apply) |
| `call_hierarchy_in` | `callHierarchy/incomingCalls` | `filePath`, `line`, `column` | `[{name, filePath, line, column}]` — name is the calling function/method |
| `call_hierarchy_out` | `callHierarchy/outgoingCalls` | `filePath`, `line`, `column` | `[{name, filePath, line, column}]` — name is the callee |
| `signature_help` | `textDocument/signatureHelp` | `filePath`, `line`, `column` | plain text string: active signature with parameter names and types |

---

## Key Design Notes

### JSON-RPC multiplexing
Each `LspClient` owns one subprocess. Requests are sent with an incrementing integer `id`. A `map[int]chan Response` holds in-flight requests; the reader goroutine routes incoming messages to the correct channel by `id`. Notifications (no `id` field) are dispatched to registered one-shot listeners.

### Diagnostics are pull-based
`diagnostics` sends `textDocument/didOpen`, registers a one-shot listener for `textDocument/publishDiagnostics` on that URI, then waits (10s timeout). Returns whatever arrived, or an empty list on timeout. Sends `textDocument/didClose` after.

### didOpen / didClose per call
Every tool call opens the file before the request and closes it after. Simple, no cached open-document state to manage. Sufficient for Phase 3.

### rename never writes to disk
`workspace_edit_to_diff` in `util.go` reads affected files into memory, applies the `WorkspaceEdit` in reverse line order to avoid offset drift, and produces a unified diff via `go-diff`. The diff is returned as the tool result. The agent reviews and applies it separately.

### Config layering
`config.go` loads in order: defaults → `env.lsp` → `env.custom` → CLI flags. Parsed directly as `KEY=VALUE` (no shell subprocess). Matches the layering in `start-lsp.sh`.

### Taplo special case
`taplo` is invoked as `taplo lsp stdio`, not `taplo --stdio`. The routing table encodes per-slot arg lists alongside binary paths to handle this.

---

## Claude Code Wiring

Once running, `.mcp.json` is updated to a single HTTP entry:

```json
{
  "mcpServers": {
    "lsp": {
      "transport": "http",
      "url": "http://localhost:7890/mcp"
    }
  }
}
```

This replaces all per-language `mcp-language-server` stdio entries. Restart Claude Code to pick it up.

---

## Build & Run

```bash
cd lsp-mcp-bridge
go build -o lsp-mcp-bridge .

# start (reads env.lsp from repo root automatically)
./lsp-mcp-bridge

# or via start-lsp.sh once the stub is replaced
./start-lsp.sh
```

`start-lsp.sh` will be updated to replace its Phase 3 TODO block with the actual launch command:

```bash
nohup ./lsp-mcp-bridge/lsp-mcp-bridge \
    --workspace "$LSP_WORKSPACE" \
    --port      "$LSP_PORT"      \
    >> "$LSP_LOG" 2>&1 &
echo $! > "$LSP_PID"
echo "Started (pid $(cat "$LSP_PID")) → http://localhost:$LSP_PORT/mcp"
```

---

## Build Order

### 1 — `config.go` · Config
Reads `env.lsp` and `env.custom` as `KEY=VALUE` pairs into a `Config` struct. Applies CLI flag overrides. No external deps — foundation everything else builds on.
**Done when:** unit test parses a fixture `env.lsp` and all fields are correctly populated; CLI flag overrides a field from the file.

### 2 — `util.go` · Util
Pure functions: `PathToURI`, `URIToPath`, `MakePosition` (0-based LSP coords from 1-based tool inputs), `extensionToLanguageID` map, `WorkspaceEditToDiff` (stub returning empty string until Step 6).
**Done when:** `PathToURI("/a/b.py")` returns `"file:///a/b.py"`; `MakePosition(1,1)` returns `{line:0, character:0}`; `extensionToLanguageID[".jsx"]` returns `"javascriptreact"`.

### 3 — `lsp_client.go` · LspClient
Owns one `exec.Cmd` subprocess. Sends `Content-Length`-framed JSON-RPC over stdin; reads responses on a goroutine. Multiplexes requests by `id` via `map[int]chan`. Dispatches notifications to one-shot listeners. Exposes `Initialize`, `Request`, `Notify`.

**Concurrency requirements:**
- Request ID counter: `sync/atomic.AddInt64` — never a plain `int`, multiple goroutines call `Request` concurrently.
- `pending` map (`map[int]chan`): protected by its own `sync.Mutex`. `sync.Map` is an alternative since reader-goroutine deletes dominate.
- Notification listeners map: separate `sync.Mutex` from `pending` — mixing them serialises notification dispatch with request multiplexing unnecessarily.
- `isAlive` flag: `sync/atomic` bool — hot read path in `manager.go`; avoids lock overhead.
- **Diagnostics ordering**: allocate the listener channel and register it *before* sending `textDocument/didOpen`. The reader goroutine may deliver `publishDiagnostics` before the send returns — registering after is a guaranteed race.
- On reader-loop EOF (crash): drain all `pending` channels with a sentinel error before clearing the map, so callers unblock immediately rather than waiting for their context deadline.

**Done when:** `Initialize` against a live `pyright-langserver` completes without error; `Request("textDocument/hover", ...)` returns a non-empty result; two concurrent `Request` calls resolve independently without deadlock.

### 4 — `manager.go` · Manager
Holds the extension → slot routing table and a `map[string]*LspClient` protected by a `sync.Mutex`. `GetClient(filePath)` resolves the slot, lazy-starts the client if needed (runs LSP handshake), and returns it.

**Concurrency requirements:**
- Slot map: single `sync.Mutex`. Use a two-phase check — read lock to check existence, write lock only to insert — to reduce contention when most slots are already live.
- Crash restart: the manager replaces the slot's `*LspClient` pointer under the write lock. Tool handlers that hold a reference to the old client continue using it safely (old client drains its pending channels); the new client is a fully independent object sharing no channels with the old one.
- Do not use `sync.Once` per slot — it cannot be reset on the restart path.

**Restart policy:** on crash, restart immediately up to 3 times. After 3 consecutive failures within 30 seconds, mark the slot as permanently failed and return an error on subsequent calls (prevents infinite restart loops on a broken binary).

**Clean shutdown:** on `SIGTERM`, for each live client: send LSP `shutdown` request (5s timeout), send `exit` notification, then `SIGKILL` if the process has not exited within 2 seconds.

**Done when:** requesting `.py` and `.go` files starts two separate processes; requesting `.py` twice reuses the first process; killing the pyright process externally triggers a restart and the next call succeeds; two concurrent calls to the same slot do not spawn two processes.

### 5 — `tools_3a.go` + `main.go` · Phase 3a tools + server
Implements `hover`, `definition`, `references`, `diagnostics` as MCP tool handlers against `Manager`. Wires into `mcp-go` HTTP server in `main.go`. **First end-to-end runnable state.**
**Done when:** `curl -X POST localhost:7890/mcp` with a valid `hover` call on a Python file returns the correct type string; `diagnostics` on a file with a deliberate type error returns the error. Update `start-lsp.sh` stub.

### 6 — `util.go` · WorkspaceEditToDiff (complete)
Reads affected files, applies `WorkspaceEdit` edits in reverse line order (handles multi-line spans), produces unified diff via `go-diff`. Handles both `changes` and `documentChanges` forms. No disk writes.
**Done when:** a synthetic `WorkspaceEdit` renaming a symbol across two files produces a correct unified diff; original files on disk are unchanged after the call.

### 7 — `tools_3b.go` · Phase 3b tools
Implements `rename` (returns diff from Step 6), `call_hierarchy_in`, `call_hierarchy_out`, `signature_help`. Register in `main.go`.
**Done when:** `rename` on a Python symbol returns a unified diff covering all call sites; `call_hierarchy_in` returns the correct callers; original files are unchanged.

### 8 — Integration
Update `.mcp.json` to the single HTTP entry. Update `start-lsp.sh` to launch the binary. Verify all tools via Claude Code on a real workspace.
**Done when:** Claude Code resolves hover, definition, and diagnostics on Python and Go files through the bridge; per-language `mcp-language-server` entries removed from `.mcp.json`.

---

**Gate:** Phase 3a (Steps 1–5) ships independently. Phase 3b (Steps 6–7) starts only after 3a is validated.

---

## Testing

Tests are split into **unit** (no subprocess, no network) and **integration** (real LSP binary). Unit tests run in CI without any language toolchain installed. Integration tests are gated behind a build tag (`//go:build integration`) and require the relevant binaries on PATH.

A **mock LSP server** (`testutil/mock_lsp.go`) is needed for unit-testing `lsp_client.go` and `manager.go` without spawning real language servers. It listens on stdin/stdout, responds to `initialize` with a canned response, and records all received requests for assertion.

---

### `config.go`

| Test | Scenario |
|---|---|
| `TestConfigParsesEnvLsp` | Valid `env.lsp` fixture — all fields populated correctly |
| `TestConfigEnvCustomOverrides` | `env.custom` overrides a value from `env.lsp` |
| `TestConfigCLIFlagOverride` | CLI `--port` overrides value from both files |
| `TestConfigPathWithSpaces` | Value containing spaces is preserved verbatim |
| `TestConfigMissingEnvCustomIsOk` | Missing `env.custom` does not error |
| `TestConfigIgnoresCommentsAndBlanks` | Lines starting with `#` and blank lines are skipped |

---

### `util.go`

| Test | Scenario |
|---|---|
| `TestPathToURI` | `/a/b.py` → `file:///a/b.py` |
| `TestPathToURISpaces` | `/a/my project/b.py` → `file:///a/my%20project/b.py` |
| `TestURIToPath` | Round-trip: `URIToPath(PathToURI(p)) == p` |
| `TestMakePosition` | `(1,1)` → `{line:0, character:0}`; `(10,5)` → `{line:9, character:4}` |
| `TestExtensionToLanguageID` | `.jsx` → `javascriptreact`; `.tsx` → `typescriptreact`; `.scss` → `scss`; `.sc` → `scala`; `.less` → `less` |
| `TestWorkspaceEditToDiffSingleLine` | Single character-range replacement produces correct unified diff |
| `TestWorkspaceEditToDiffMultiLine` | Edit spanning multiple lines (start.line != end.line) produces correct diff |
| `TestWorkspaceEditToDiffMultiFile` | Edit across two files produces two diff blocks |
| `TestWorkspaceEditToDiffDocumentChanges` | `documentChanges` form produces same output as `changes` form |
| `TestWorkspaceEditToDiffNoDiskWrite` | Files on disk are unchanged after call |

---

### `lsp_client.go` (uses mock LSP server)

| Test | Scenario |
|---|---|
| `TestInitializeCompletes` | `Initialize` sends correct request shape; mock responds; no error |
| `TestRequestResponseRoundtrip` | `Request` sends with an id; response resolves the correct channel |
| `TestConcurrentRequests` | Two goroutines call `Request` simultaneously; each resolves to its own response without cross-contamination |
| `TestNotificationDispatch` | Listener registered before notification arrives; receives it |
| `TestNotificationRaceRegisterBeforeSend` | Listener registered, then `didOpen` sent — no notification lost (the critical ordering test) |
| `TestReaderLoopDrainsOnEOF` | Mock server closes stdout; all pending channels receive sentinel error; no goroutine leaks |
| `TestIsAliveFalseAfterEOF` | `isAlive` is false after reader loop exits |
| `TestRequestAfterCrashReturnsError` | `Request` called after EOF returns error immediately rather than blocking |

---

### `manager.go` (uses mock LSP server)

| Test | Scenario |
|---|---|
| `TestGetClientStartsProcess` | First call for `.py` spawns a process |
| `TestGetClientReusesProcess` | Second call for `.py` returns same `*LspClient` |
| `TestGetClientTwoSlots` | `.py` and `.go` calls produce two separate processes |
| `TestGetClientConcurrentSameSlot` | 10 goroutines call `GetClient(".py")` simultaneously — exactly one process spawned |
| `TestGetClientUnsupportedExtension` | `.xyz` returns a descriptive error |
| `TestGetClientMissingBinary` | Slot binary empty in Config returns error naming the binary and install command |
| `TestCrashTriggerRestart` | Kill mock process; next `GetClient` returns a new live client |
| `TestRestartLimitPermanentFailure` | Mock crashes on every `initialize`; after 3 attempts slot is marked failed; subsequent calls return error without spawning |
| `TestShutdownSendsLSPShutdown` | `Shutdown()` sends `shutdown` request + `exit` notification to each live client |
| `TestShutdownSIGKILLFallback` | Mock ignores `shutdown`; manager SIGKILLs after 2s |
| `TestGoplsRootURIWalksToGoMod` | File in `backend/pkg/foo.go`; `go.mod` in `backend/`; `rootUri` set to `backend/` not repo root |
| `TestGoplsRootURIFallback` | No `go.mod` found; `rootUri` falls back to `LSP_WORKSPACE` |

---

### `tools_3a.go` (integration, requires real LSP binary)

| Test | Tag | Scenario |
|---|---|---|
| `TestHoverReturnType` | `integration` | Hover on a typed Python symbol returns the correct type string |
| `TestHoverUnknownPosition` | `integration` | Hover at whitespace returns empty result without error |
| `TestDefinitionReturnsLocation` | `integration` | Definition on a function call returns the file and line where it is defined |
| `TestDefinitionMultipleResults` | `integration` | Overloaded symbol returns multiple locations |
| `TestReferencesReturnsList` | `integration` | References on a symbol used in three places returns three locations |
| `TestDiagnosticsTypeError` | `integration` | File with deliberate type error returns `{severity:"error", ...}` |
| `TestDiagnosticsCleanFile` | `integration` | File with no errors returns empty list |
| `TestDiagnosticsListenerBeforeDidOpen` | `integration` | Listener registered before `didOpen`; diagnostics not lost on fast server response |
| `TestDidCloseAfterResult` | `integration` | `didClose` is sent after the tool returns; verify via mock that open/close are balanced |

---

### `tools_3b.go` (integration, requires real LSP binary)

| Test | Tag | Scenario |
|---|---|---|
| `TestRenameDiffCorrect` | `integration` | Rename symbol used in two files; diff covers both; original files unchanged |
| `TestRenameSingleFile` | `integration` | Symbol only in one file; diff has one block |
| `TestRenameNoDiskWrite` | `integration` | Files on disk are byte-for-byte identical before and after call |
| `TestRenameDocumentChangesForm` | `integration` | Server returns `documentChanges`; diff still produced correctly |
| `TestCallHierarchyInReturnsCallers` | `integration` | Callers of a function are returned with correct name, file, and line |
| `TestCallHierarchyOutReturnsCallees` | `integration` | Callees of a function are returned |
| `TestCallHierarchyNonCallable` | `integration` | Position on a variable (not a function) returns empty result without error |
| `TestSignatureHelpReturnsParams` | `integration` | Hover inside a function call returns parameter names and types |

---

### Critical path integration tests (end-to-end, `//go:build integration`)

| Test | Scenario |
|---|---|
| `TestE2EHoverViaCurl` | Full HTTP round-trip: POST to `/mcp`, valid `hover` call, correct type returned |
| `TestE2EConcurrentSameLanguage` | 5 concurrent `hover` calls on Python files; all resolve correctly; one process running |
| `TestE2EConcurrentDifferentLanguages` | Concurrent `hover` on `.py` and `.go`; two processes; correct results |
| `TestE2EClientCrashRecovery` | Kill pyright mid-request; next call succeeds after restart |
| `TestE2ERestartLimitExceeded` | Binary replaced with a script that always exits 1; after limit, tool returns error |
| `TestE2EDiagnosticsMultipleBatches` | Pyright emits two `publishDiagnostics` batches; both merged before return |
| `TestE2ERenameNoDiskWrite` | Rename via HTTP; diff returned; files unchanged |

---

### Test infrastructure needed

- `testutil/mock_lsp.go` — mock LSP server: reads JSON-RPC from stdin, responds to `initialize`, records all requests, can be scripted to crash or delay responses
- `testutil/fixtures/` — sample `env.lsp`, small `.py` and `.go` files with known types for hover/definition/diagnostics assertions
- `testutil/env.go` — helpers to spin up a mock LSP server and return a connected `LspClient`

---

## Concurrency Reference

All contention points, their locations, and the correct primitive. Resolve these during the build step where the relevant struct is first written — not after.

| What | Where | Primitive | Notes |
|---|---|---|---|
| Request ID counter | `lsp_client.go` | `sync/atomic.AddInt64` | Multiple goroutines call `Request` concurrently |
| `pending` map | `lsp_client.go` | `sync.Mutex` | Reader goroutine deletes; request goroutines insert |
| Notification listeners map | `lsp_client.go` | `sync.Mutex` (shared with `pending`) | Both maps are small and the lock is held briefly; split only if profiling shows contention |
| `isAlive` flag | `lsp_client.go` | `sync/atomic` bool | Hot read path in manager; avoid lock overhead |
| Diagnostics listener registration | `lsp_client.go` / `tools_3a.go` | Sequencing, not a lock | Register listener **before** sending `didOpen` — not after |
| Pending channel drain on crash | `lsp_client.go` | Drain under mutex | Send sentinel error to all `pending` channels on EOF so callers unblock |
| Slot map | `manager.go` | `sync.Mutex` | Two-phase: read lock to check, write lock to insert |
| Client pointer replacement on restart | `manager.go` | Write lock | Old client drains independently; new client shares no state with old |

**Do not use `sync.Once` for slots** — it cannot be reset when a crashed slot needs to restart.

**The diagnostics race is a sequencing problem, not a locking problem.** No mutex fixes it — only moving listener registration before `didOpen` does.

---

## Protocol Notes

### LSP JSON-RPC framing (lsp_client.go)

LSP uses JSON-RPC 2.0 over stdio with a HTTP-style header prefix:

```
Content-Length: 123\r\n
\r\n
{"jsonrpc":"2.0","id":1,"method":"...","params":{...}}
```

Reading: parse `Content-Length` from the header, then read exactly that many bytes for the body.
Writing: `fmt.Fprintf(stdin, "Content-Length: %d\r\n\r\n%s", len(body), body)`.

Messages have three shapes:
- **Request** — has `id`, `method`, `params`. Expects a response.
- **Response** — has `id`, `result` or `error`. No `method`.
- **Notification** — has `method`, no `id`. No response expected.

The reader goroutine distinguishes them by presence of `id` and `method`:
```go
if msg.ID != nil && msg.Method == "" { /* response → resolve pending[id] */ }
if msg.ID == nil && msg.Method != "" { /* notification → dispatch to listeners */ }
```

### LSP initialize handshake (lsp_client.go)

Must be the first request sent. Minimal required shape:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "initialize",
  "params": {
    "processId": 12345,
    "rootUri": "file:///absolute/path/to/workspace",
    "workspaceFolders": [
      { "uri": "file:///absolute/path/to/workspace", "name": "workspace" }
    ],
    "capabilities": {
      "textDocument": {
        "hover":          { "contentFormat": ["plaintext"] },
        "definition":     {},
        "references":     {},
        "rename":         { "prepareSupport": true },
        "signatureHelp":  {},
        "publishDiagnostics": {}
      },
      "workspace": {
        "workspaceFolders": true
      }
    }
  }
}
```

After the response arrives, send the `initialized` notification (no `id`, no response):

```json
{ "jsonrpc": "2.0", "method": "initialized", "params": {} }
```

### textDocument/didOpen and didClose (tools_3a.go, tools_3b.go)

Send before any per-file request, close after:

```json
{
  "jsonrpc": "2.0",
  "method": "textDocument/didOpen",
  "params": {
    "textDocument": {
      "uri":        "file:///path/to/file.py",
      "languageId": "python",
      "version":    1,
      "text":       "<full file contents>"
    }
  }
}
```

`languageId` per extension — defined as a map in `util.go` (`extensionToLanguageID`):

| Extension(s) | `languageId` |
|---|---|
| `.py` | `python` |
| `.js` `.mjs` | `javascript` |
| `.jsx` | `javascriptreact` |
| `.ts` | `typescript` |
| `.tsx` | `typescriptreact` |
| `.rs` | `rust` |
| `.go` | `go` |
| `.kt` `.kts` | `kotlin` |
| `.scala` `.sc` | `scala` |
| `.sh` `.bash` `.zsh` | `shellscript` |
| `.yml` `.yaml` | `yaml` |
| `.toml` | `toml` |
| `.json` `.jsonc` | `json` |
| `.html` `.htm` | `html` |
| `.css` | `css` |
| `.scss` | `scss` |
| `.less` | `less` |

```json
{ "jsonrpc": "2.0", "method": "textDocument/didClose",
  "params": { "textDocument": { "uri": "file:///path/to/file.py" } } }
```

### Position encoding

LSP positions are **0-based** line and character. MCP tool inputs use **1-based** line and column. `MakePosition` in `util.go` subtracts 1 from both:

```go
func MakePosition(line, col int) map[string]any {
    return map[string]any{"line": line - 1, "character": col - 1}
}
```

### WorkspaceEdit structure (util.go — WorkspaceEditToDiff)

Returned by `textDocument/rename`:

```json
{
  "changes": {
    "file:///path/to/file.py": [
      {
        "range": {
          "start": { "line": 10, "character": 4 },
          "end":   { "line": 10, "character": 15 }
        },
        "newText": "new_name"
      }
    ]
  }
}
```

To apply without writing to disk:
1. For each URI in `changes`, read the file into `[]string` lines.
2. Sort edits for that file by `start.line` **descending** (avoids offset drift).
3. For each edit, replace the character range in the affected line(s) with `newText`.
4. Produce the unified diff via `go-diff`. There is no `DiffLines` function — use the three-step line-mode approach:
```go
dmp := diffmatchpatch.New()
a, b, lines := dmp.DiffLinesToChars(original, modified)
diffs := dmp.DiffMain(a, b, false)
diffs = dmp.DiffCharsToLines(diffs, lines)
patches := dmp.PatchMake(original, diffs)
diff := dmp.PatchToText(patches)
// PatchToText produces GNU patch hunks (@@ -x,y +a,b @@).
// Prepend "--- a/<relpath>\n+++ b/<relpath>\n" manually for git-style headers.
```

### Call hierarchy two-step (tools_3b.go)

`call_hierarchy_in` and `call_hierarchy_out` require two sequential LSP requests:

**Step 1** — prepare (returns a `CallHierarchyItem`):
```json
{
  "method": "textDocument/prepareCallHierarchy",
  "params": { "textDocument": { "uri": "..." }, "position": { "line": 0, "character": 0 } }
}
```

**Step 2** — query with the item from step 1:
```json
{ "method": "callHierarchy/incomingCalls", "params": { "item": <CallHierarchyItem> } }
{ "method": "callHierarchy/outgoingCalls", "params": { "item": <CallHierarchyItem> } }
```

If step 1 returns null or an empty array, the symbol is not callable — return an empty result.

### mcp-go tool registration (main.go, bridge.go)

```go
s := server.NewMCPServer("lsp", "0.1.0")

s.AddTool(mcp.NewTool("hover",
    mcp.WithDescription("Get type and documentation for a symbol"),
    mcp.WithString("filePath", mcp.Required(), mcp.Description("Absolute path to the file")),
    mcp.WithNumber("line",     mcp.Required(), mcp.Description("Line number (1-based)")),
    mcp.WithNumber("column",   mcp.Required(), mcp.Description("Column number (1-based)")),
), hoverHandler(mgr))

// hoverHandler signature
func hoverHandler(mgr *Manager) server.ToolHandlerFunc {
    return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
        // req.Params.Arguments is typed `any` — direct map assertion will not compile.
        // Use the accessor methods:
        filePath, err := req.RequireString("filePath")
        if err != nil { return mcp.NewToolResultError(err.Error()), nil }
        line, err := req.RequireInt("line")
        if err != nil { return mcp.NewToolResultError(err.Error()), nil }
        column, err := req.RequireInt("column")
        if err != nil { return mcp.NewToolResultError(err.Error()), nil }
        // ... call mgr.GetClient(filePath), issue LSP request, return result
        return mcp.NewToolResultText(result), nil
    }
}
```

Start the HTTP server:
```go
httpServer := server.NewStreamableHTTPServer(s)
log.Fatal(httpServer.Start(":7890"))
```

### Error handling convention

Return `mcp.NewToolResultError(msg)` for recoverable errors (unsupported file type, binary not installed, LSP timeout). Return a Go `error` only for unrecoverable server-level failures.

---

## Edge Cases & Implementation Traps

### 1. `languageId` mapping is not 1:1 with extension (didOpen — tools_3a.go, tools_3b.go)

Several extensions have non-obvious `languageId` values that LSP servers enforce:

| Extension | Correct `languageId` | Wrong assumption |
|---|---|---|
| `.jsx` | `javascriptreact` | `javascript` |
| `.tsx` | `typescriptreact` | `typescript` |
| `.scss` | `scss` | `css` |
| `.less` | `less` | `css` |
| `.mjs` | `javascript` | — |
| `.kts` | `kotlin` | — |

`manager.go` needs a separate `extensionToLanguageID` map alongside the routing table. Using the wrong `languageId` causes tsserver and vscode-css-language-server to silently return empty results.

---

### 2. Diagnostics listener must be registered BEFORE didOpen (lsp_client.go, tools_3a.go)

The current design says: send `didOpen`, then register the `publishDiagnostics` listener. This is a race — fast servers (or files already in cache) may emit `publishDiagnostics` before the listener is registered, and the notification is lost, returning an empty result.

**Fix:** register the one-shot listener for `textDocument/publishDiagnostics` on the URI *before* sending `textDocument/didOpen`.

---

### 3. Multiple publishDiagnostics batches (tools_3a.go)

Pyright (and others) emit `publishDiagnostics` in multiple phases: one pass for syntax errors, another after full semantic analysis. Waiting for only the first notification may return incomplete diagnostics.

**Fix:** after the first `publishDiagnostics` notification arrives, reset a 200ms idle timer. Collect all further notifications for the same URI until the timer fires with no new notification, then merge and return. Cap total wait at 10s regardless. This adapts to server speed rather than using a fixed window.

---

### 4. WorkspaceEdit uses `documentChanges`, not just `changes` (util.go)

The doc covers only the `changes` field. Modern LSP servers (gopls, pyright, metals) prefer `documentChanges` — an array of `TextDocumentEdit` objects — which also carries a version field per file. Both forms must be handled:

```json
{
  "documentChanges": [
    {
      "textDocument": { "uri": "file:///path/to/file.go", "version": 3 },
      "edits": [ { "range": {...}, "newText": "..." } ]
    }
  ]
}
```

`WorkspaceEditToDiff` should check `documentChanges` first, fall back to `changes`.

---

### 5. Multi-line edits in WorkspaceEditToDiff (util.go)

The reverse-sort-by-line approach works for single-line edits. Multi-line replacements (where `start.line != end.line`) require replacing a span of lines, not just a character range within one line. The algorithm needs to handle this explicitly — collect lines from `start.line` to `end.line`, splice `newText` in, then replace that slice in the buffer.

---

### 6. LSP character offsets are UTF-16 code units by default (util.go)

LSP 3.17 uses UTF-16 `character` offsets unless the client and server negotiate `positionEncoding: "utf-8"` during `initialize`. For ASCII files this is identical. For files with non-ASCII characters (e.g. string literals with emoji, CJK identifiers), a Go `string` byte index differs from the UTF-16 code unit offset.

**Fix:** add `positionEncoding: "utf-8"` to `initialize` capabilities and assert it in the response:

```json
"capabilities": {
  "general": { "positionEncodings": ["utf-8"] },
  ...
}
```

If the server doesn't support it, fall back to UTF-16 conversion in `MakePosition` and `WorkspaceEditToDiff`.

---

### 7. `callHierarchy` capability missing from initialize (lsp_client.go)

The `initialize` capabilities block in the doc is missing `callHierarchy`. Without it, some servers (notably gopls) will not respond to `textDocument/prepareCallHierarchy`.

Add to `textDocument` capabilities:

```json
"callHierarchy": {}
```

---

### 8. Concurrent lazy-start race in manager.go

Two simultaneous tool calls for the same file extension will both hit `GetClient`, find no live client, and both attempt to spawn and initialise the same slot. This produces two competing LSP processes and likely a corrupted slot entry.

**Fix:** use a `sync.Mutex` around the slot map for both the initial lazy-start and the crash-restart path. `sync.Once` is not suitable here because it cannot be reset on restart.

---

### 9. In-flight requests must be drained on crash (lsp_client.go)

When the LSP process exits unexpectedly, the reader goroutine will get EOF. All channels in `pending` must be closed (or sent an error) at that point — otherwise callers block until their context deadline.

**Fix:** on reader-loop exit, iterate `pending` and close all channels with a sentinel error before clearing the map.

---

### 10. `processId` must be the real PID (lsp_client.go)

The `initialize` example shows `"processId": 12345`. This must be `os.Getpid()`. Some servers (gopls, rust-analyzer) monitor the parent PID and self-terminate if the parent exits, which is the correct behaviour for cleanup. A hardcoded value breaks this.

---

### 11. `didClose` timing for diagnostics (tools_3a.go)

The doc says send `didClose` after receiving `publishDiagnostics`. Sending it too early (before the server has finished its analysis pass) can cause some servers to cancel the analysis and emit an empty second batch that overwrites the results.

**Fix:** send `didClose` after the tool handler has finished assembling the result and is about to return — not as soon as the first notification arrives.

---

### 12. Config parser and paths with spaces (config.go)

A naive `KEY=VALUE` line parser will break on paths containing spaces (e.g. `/Users/John Doe/...`). The bootstrap script writes unquoted values, so this is a real edge case on macOS.

**Fix:** trim only the key, preserve everything after the first `=` as the value verbatim (including spaces). Do not split on spaces.

---

### 13. metals may require BSP discovery (manager.go)

`metals` in stdio LSP mode works for basic operations but may emit warnings or degrade on projects that require BSP (Build Server Protocol) discovery (e.g. complex sbt multi-module builds). This is a known rough edge — note it in the error message when metals returns empty results on a Scala file.

---

### 14. `rust-analyzer` — no `--stdio` flag needed

The routing table shows `LSP_RUST_BIN` without extra args. This is correct — rust-analyzer uses stdio by default when invoked without arguments. Do not add `-- --stdio`; some versions treat it as an unknown flag and exit.

---

### 15. gopls requires a valid module root in `rootUri`

gopls resolves imports relative to the nearest `go.mod`. If `rootUri` in `initialize` points to the repo root but the Go module is in a subdirectory (e.g. `backend/`), gopls will fail to resolve cross-package references.

**Fix:** at `GetClient` time for the `go` slot, walk up from `filePath` toward `LSP_WORKSPACE` looking for `go.mod`. Use the directory containing the first `go.mod` found as `rootUri`. If no `go.mod` is found before reaching `LSP_WORKSPACE`, fall back to `LSP_WORKSPACE`. If `filePath` is outside `LSP_WORKSPACE`, return an error.

---

## Success Criteria

| Phase | Done when |
|---|---|
| 3a | `hover` on a Python symbol returns the correct type; `diagnostics` on a file with a deliberate type error returns the error |
| 3b | `rename` on a symbol returns a correct unified diff across all affected files without modifying disk |
