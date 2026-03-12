# Implementation Guide — Phase 3a

Coding-focused guide for Steps 1–5 of the LSP MCP bridge. Reference `MCP-BRIDGE.md` for rationale, edge case detail, and protocol notes. Reference `PLAN_language_server_mcp.md` for project context.

**Deliverable:** a running HTTP MCP server at `localhost:7890` that handles `hover`, `definition`, `references`, and `diagnostics` for all languages wired in `generate-env-lsp.sh`.

---

## Repo layout

```
lsp-mcp-bridge/
  main.go
  config.go
  lsp_client.go
  manager.go
  tools_3a.go
  util.go
  testutil/
    mock_lsp.go
    env.go
    fixtures/
      env.lsp
      sample.py
      sample.go
  go.mod
  go.sum
```

---

## Step 0 — Test scaffolding

Write all acceptance test bodies and fixture files before any implementation. Tests carry `t.Skip("not yet implemented")` and a `//go:build acceptance` tag. Each subsequent step removes the skip from the tests it satisfies.

**Why first:** acceptance tests written after implementation describe what was built. Written before, they define what must be built — they are the spec in executable form.

### Files to create

```
lsp-mcp-bridge/
  go.mod
  acceptance_test.go          # A1–A10, B1–B6, F1–F4 — all skipped initially
  testutil/
    mock_lsp.go               # mock LSP server: stdin→stdout JSON-RPC, scriptable responses
    env.go                    # FixturePath(), TempEnvLsp(), RealBinPath() helpers
                              # Note: in-process client wiring is NewLspClientFromPipes() in lsp_client.go
    fixtures/
      env.lsp                 # fixture config with placeholder binary paths
      sample.py               # typed Python with a known symbol for hover/definition/references
      sample_error.py         # Python file with a deliberate type mismatch
      sample.go               # typed Go with a known symbol
```

**Skip removal schedule:**

| Tests | Remove skip after |
|---|---|
| A6, A7, F1, F2, F3 | Step 4 (manager.go) |
| A1–A5, A8–A10 | Step 5 (tools_3a.go) |
| B1–B6 | Step 7 (tools_3b.go) |
| F4 | Step 7 (tools_3b.go) |

**Done when:** `go test -tags acceptance ./...` compiles cleanly with all tests skipped; fixtures load without error.

---

## Step 1 — `config.go`

```go
type Config struct {
    Workspace   string
    Port        string
    LogPath     string
    PyrightBin  string
    TSSBin      string
    RustBin     string
    GoplsBin    string
    KotlinBin   string
    MetalsBin   string
    BashBin     string
    YamlBin     string
    TaploBin    string
    JSONBin     string
    HTMLBin     string
    CSSBin      string
}

func Load(envLsp, envCustom string, flags map[string]string) (*Config, error)
```

- Parse `KEY=VALUE` line by line; value is everything after the first `=` (preserves spaces in paths)
- Skip lines starting with `#` and blank lines
- Load order: defaults → `envLsp` → `envCustom` → `flags`
- `envCustom` missing is not an error

**Env var → field mapping:**

| env.lsp key | Config field |
|---|---|
| `LSP_WORKSPACE` | `Workspace` |
| `LSP_PORT` | `Port` |
| `LSP_LOG` | `LogPath` |
| `LSP_PYRIGHT_BIN` | `PyrightBin` |
| `LSP_TSS_BIN` | `TSSBin` |
| `LSP_RUST_BIN` | `RustBin` |
| `LSP_GOPLS_BIN` | `GoplsBin` |
| `LSP_KOTLIN_BIN` | `KotlinBin` |
| `LSP_METALS_BIN` | `MetalsBin` |
| `LSP_BASH_BIN` | `BashBin` |
| `LSP_YAML_BIN` | `YamlBin` |
| `LSP_TAPLO_BIN` | `TaploBin` |
| `LSP_JSON_BIN` | `JSONBin` |
| `LSP_HTML_BIN` | `HTMLBin` |
| `LSP_CSS_BIN` | `CSSBin` |

**Done when:**
- `TestConfigParsesEnvLsp` — all fields populated from fixture
- `TestConfigEnvCustomOverrides` — `env.custom` field wins over `env.lsp`
- `TestConfigCLIFlagOverride` — flag wins over both files
- `TestConfigPathWithSpaces` — value with spaces preserved verbatim
- `TestConfigMissingEnvCustomIsOk` — no error when `env.custom` absent

---

## Step 2 — `util.go`

```go
func PathToURI(path string) string
func URIToPath(uri string) string
func MakePosition(line, col int) map[string]any   // subtracts 1 from both
func LanguageID(ext string) (string, bool)        // ext includes dot, e.g. ".jsx"
```

**`LanguageID` map** — define as a package-level `map[string]string`:

```go
var langIDs = map[string]string{
    ".py":    "python",
    ".js":    "javascript",   ".mjs":  "javascript",
    ".jsx":   "javascriptreact",
    ".ts":    "typescript",
    ".tsx":   "typescriptreact",
    ".rs":    "rust",
    ".go":    "go",
    ".kt":    "kotlin",       ".kts":  "kotlin",
    ".scala": "scala",        ".sc":   "scala",
    ".sh":    "shellscript",  ".bash": "shellscript", ".zsh": "shellscript",
    ".yml":   "yaml",         ".yaml": "yaml",
    ".toml":  "toml",
    ".json":  "json",         ".jsonc": "json",
    ".html":  "html",         ".htm":  "html",
    ".css":   "css",
    ".scss":  "scss",
    ".less":  "less",
}
```

`WorkspaceEditToDiff` is a stub at this step — returns `""`. Completed in Step 6 (Phase 3b).

**Done when:**
- `TestPathToURISpaces` — spaces percent-encoded
- `TestURIToPathRoundtrip` — `URIToPath(PathToURI(p)) == p`
- `TestMakePosition` — `(1,1)` → `{line:0,character:0}`
- `TestLanguageIDEdgeCases` — `.jsx`→`javascriptreact`, `.tsx`→`typescriptreact`, `.scss`→`scss`, `.sc`→`scala`

---

## Step 3 — `lsp_client.go`

```go
type LspClient struct {
    cmd        *exec.Cmd
    stdin      io.WriteCloser
    stdout     *bufio.Reader
    nextID     atomic.Int64
    mu         sync.Mutex          // guards pending and listeners
    pending    map[int64]chan rpcResponse
    listeners  map[string]chan json.RawMessage  // keyed by "method:uri"
    isAlive    atomic.Bool
}

func NewLspClient(binary string, args []string) *LspClient
func (c *LspClient) Start(ctx context.Context) error  // ctx is accepted but not wired to exec.CommandContext;
                                                       // cancelling ctx does NOT kill the subprocess.
                                                       // Use Close() or Manager.Shutdown() for lifecycle control.
func (c *LspClient) Initialize(workspace string) error
func (c *LspClient) Request(method string, params any) (json.RawMessage, error)
func (c *LspClient) Notify(method string, params any) error
func (c *LspClient) RegisterNotification(key string) chan json.RawMessage
func (c *LspClient) Close()
```

**`pending` and `listeners` share one `sync.Mutex`.** Both maps are small and the lock is held only for map insert/delete, never across I/O. Split into separate mutexes only if profiling shows contention. (Note: an earlier draft of MCP-BRIDGE.md said to use separate mutexes — that was revised; the shared-mutex approach is the canonical choice.)

**Reader goroutine** (`start` spawns it):

```go
func (c *LspClient) readLoop() {
    defer c.drainPending()
    for {
        msg, err := c.readMessage()
        if err != nil { c.isAlive.Store(false); return }
        if msg.ID != nil && msg.Method == "" { c.resolveRequest(msg) }
        if msg.ID == nil && msg.Method != "" { c.dispatchNotification(msg) }
    }
}
```

**`drainPending`** — called on readLoop exit; sends a sentinel error to every channel in `pending` so callers unblock. Holds mutex while draining.

**Framing:**

```go
// write
body, _ := json.Marshal(msg)
fmt.Fprintf(c.stdin, "Content-Length: %d\r\n\r\n", len(body))
c.stdin.Write(body)

// read
// scan header lines until blank line, parse Content-Length
// io.ReadFull(c.stdout, buf[:contentLength])
```

**`initialize` params** — see `MCP-BRIDGE.md § LSP initialize handshake`. Use `os.Getpid()` for `processId`. Include `callHierarchy: {}` in capabilities.

**Diagnostics listener key:** `"textDocument/publishDiagnostics:" + uri`

**Done when:**
- `TestInitializeCompletes` — mock server; `Initialize` returns nil
- `TestConcurrentRequests` — two goroutines; responses cross-matched correctly
- `TestNotificationRaceRegisterBeforeSend` — listener registered before `didOpen`; notification received
- `TestReaderLoopDrainsOnEOF` — mock closes stdout; all pending return error; no goroutine leak

---

## Step 4 — `manager.go`

```go
type slotDef struct {
    binary string
    args   []string
}

type slotState struct {
    client       *LspClient
    failures     int
    lastFailure  time.Time
    dead         bool          // permanently failed after limit
}

type Manager struct {
    cfg   *Config
    slots map[string]*slotState // keyed by slot name
    defs  map[string]slotDef    // slot name → binary + args
    mu    sync.Mutex
}
// Note: extension → slot routing is a package-level var (extToSlot), not a struct field.

func NewManager(cfg *Config) *Manager
func (m *Manager) GetClient(filePath string) (*LspClient, error)
func (m *Manager) Shutdown(ctx context.Context)
```

**Routing table** — define as a package-level constant:

```go
var extToSlot = map[string]string{
    ".py":    "python",
    ".js":    "typescript",  ".jsx":  "typescript",
    ".ts":    "typescript",  ".tsx":  "typescript",  ".mjs": "typescript",
    ".rs":    "rust",
    ".go":    "go",
    ".kt":    "kotlin",      ".kts":  "kotlin",
    ".scala": "scala",       ".sc":   "scala",
    ".sh":    "shell",       ".bash": "shell",       ".zsh": "shell",
    ".yml":   "yaml",        ".yaml": "yaml",
    ".toml":  "toml",
    ".json":  "json",        ".jsonc": "json",
    ".html":  "html",        ".htm":  "html",
    ".css":   "css",         ".scss": "css",         ".less": "css",
}
```

**Slot → binary + args** — resolved from `*Config` at `NewManager` time:

```go
var slotArgs = map[string][]string{
    "python":     {"--stdio"},
    "typescript": {"--stdio"},
    "rust":       {},
    "go":         {},
    "kotlin":     {},
    "scala":      {},
    "shell":      {"--stdio"},
    "yaml":       {"--stdio"},
    "toml":       {"lsp", "stdio"},
    "json":       {"--stdio"},
    "html":       {"--stdio"},
    "css":        {"--stdio"},
}
```

**`GetClient` logic:**

```
lock
  ext = filepath.Ext(filePath).toLower()
  slot = extToSlot[ext]          → error if not found
  state = m.slots[slot]
  if state.dead → error
  if state.client != nil && state.client.isAlive → return client (unlock first)
  // need to start or restart
  check restart limit (3 failures in 30s → mark dead, error)
  start new LspClient, Initialize
  if error → increment failure count, return error
  store client
unlock
return client
```

**Shutdown:**

```go
func (m *Manager) Shutdown(ctx context.Context) {
    // for each live client:
    //   send LSP "shutdown" request (respect ctx deadline)
    //   send "exit" notification
    //   wait up to 2s for process exit, then SIGKILL
}
```

**gopls special case:** before initialising the `go` slot, walk up from `filePath` to find the nearest `go.mod`. Use that directory as `rootUri`. Fall back to `cfg.Workspace` if none found.

**Done when:**
- `TestGetClientStartsProcess` — first `.py` call spawns mock process
- `TestGetClientReusesProcess` — second `.py` call returns same client
- `TestGetClientConcurrentSameSlot` — 10 goroutines; exactly one process spawned
- `TestGetClientUnsupportedExtension` — `.xyz` returns descriptive error
- `TestCrashTriggerRestart` — kill mock; next call returns new live client
- `TestRestartLimitPermanentFailure` — 3 crashes in 30s; slot marked dead

---

## Step 5 — `tools_3a.go` + `main.go`

### Tool handler signature

```go
func hoverHandler(mgr *Manager) server.ToolHandlerFunc
func definitionHandler(mgr *Manager) server.ToolHandlerFunc
func referencesHandler(mgr *Manager) server.ToolHandlerFunc
func diagnosticsHandler(mgr *Manager) server.ToolHandlerFunc
```

### Common pattern for hover / definition / references

```
1. client = mgr.GetClient(filePath)
2. uri, content, langID = openFile(filePath)   // reads file; derives langID via LanguageID(ext)
3. client.Notify("textDocument/didOpen", didOpenParams(uri, langID, content))
4. result, err = client.Request("textDocument/<method>", params)
5. client.Notify("textDocument/didClose", didCloseParams(uri))
6. parse + return result
```

### Diagnostics — ordering matters

```
1. client = mgr.GetClient(filePath)
2. content = os.ReadFile(filePath)
3. uri = util.PathToURI(filePath)
4. ch = client.RegisterNotification("textDocument/publishDiagnostics:" + uri)
5. client.Notify("textDocument/didOpen", didOpenParams(uri, langID, content))
6. start 200ms idle timer immediately (before first notification arrives)
   collect notifications from ch:
     - on each receive: reset the 200ms idle timer and merge diagnostics into result
     - if no notification arrives within 200ms: stop collecting (timer fires)
     - hard cap: stop after 10s regardless
   WARNING: the timer starts before the first notification — if the server takes >200ms
   to emit its first publishDiagnostics, the call returns empty. This is intentional for
   fast servers; slow/cold servers may need a higher initial timeout.
7. client.Notify("textDocument/didClose", didCloseParams(uri))
8. return merged diagnostics
```

### Output shapes

```go
// hover → plain string (type + doc, as returned by LSP)

// definition
type Location struct {
    FilePath string `json:"filePath"`
    Line     int    `json:"line"`    // 1-based
    Column   int    `json:"column"`  // 1-based
}

// references → []Location

// diagnostics
type Diagnostic struct {
    Severity string `json:"severity"`  // "error" | "warning" | "info" | "hint"
    Message  string `json:"message"`
    Line     int    `json:"line"`      // 1-based
    Column   int    `json:"column"`    // 1-based
}
```

### `main.go`

```go
func main() {
    // 1. parse flags: --workspace, --port, --env-lsp, --env-custom
    // 2. cfg = config.Load(envLsp, envCustom, flags)
    // 3. mgr = NewManager(cfg)
    // 4. s = server.NewMCPServer("lsp-mcp-bridge", "0.1.0")
    // 5. register tools: hover, definition, references, diagnostics
    // 6. httpServer = server.NewStreamableHTTPServer(s)
    // 7. go httpServer.Start(":" + cfg.Port)   // non-blocking — Start blocks internally
    // 8. ctx, stop = signal.NotifyContext(ctx, SIGINT, SIGTERM)
    // 9. <-ctx.Done()
    // 10. httpServer.Shutdown(shutCtx)         // closes listener; unblocks Start goroutine
    // 11. mgr.Shutdown(shutCtx)
}
// WARNING: do NOT use log.Fatal(httpServer.Start(...)) — Start blocks and signal handling
// never fires. Run Start in a goroutine; block main on the signal context instead.
```

**Tool registration example:**

```go
s.AddTool(mcp.NewTool("hover",
    mcp.WithDescription("Get type and documentation for a symbol at a position"),
    mcp.WithString("filePath", mcp.Required(), mcp.Description("Absolute path to the file")),
    mcp.WithNumber("line",     mcp.Required(), mcp.Description("Line number (1-based)")),
    mcp.WithNumber("column",   mcp.Required(), mcp.Description("Column number (1-based)")),
), hoverHandler(mgr))
```

**Argument access — use the accessor methods, not direct map assertion:**

```go
// WRONG — will not compile: Arguments is typed `any`, not `map[string]any`
line := int(req.Params.Arguments["line"].(float64))

// CORRECT
filePath, err := req.RequireString("filePath")
line,     err := req.RequireInt("line")
column,   err := req.RequireInt("column")
```

`RequireString` / `RequireInt` / `RequireFloat` / `RequireBool` return `(T, error)`. Use these in all tool handlers — they handle the type assertion internally and return a descriptive error if the argument is missing or wrong type.

**Done when:**
- `TestHoverReturnType` (integration) — hover on typed Python symbol returns correct type string
- `TestDiagnosticsTypeError` (integration) — deliberate type error returns `{severity:"error",...}`
- `TestDiagnosticsCleanFile` (integration) — clean file returns empty list
- `TestDiagnosticsListenerBeforeDidOpen` (integration) — listener registered before `didOpen`; no missed notification
- `TestE2EHoverViaCurl` (integration) — full HTTP round-trip returns correct result
- `start-lsp.sh` and `.mcp.json` are updated in **Step 8 of IMPL-3B.md** (after 3b tools are wired). Do not update them here — the 3B version includes a `go build` auto-build guard that the 3A-only version lacks.

---

## Phase 3a success criteria

| Check | How |
|---|---|
| `hover` returns correct type | Integration test on `sample.py` known symbol |
| `diagnostics` returns error | Integration test on `sample_error.py` (deliberate type mismatch) |
| `diagnostics` returns empty on clean file | Integration test |
| No notification lost on fast server | `TestDiagnosticsListenerBeforeDidOpen` |
| Two concurrent `.py` requests → one process | `TestGetClientConcurrentSameSlot` |
| Crash recovery | `TestCrashTriggerRestart` |
| Claude Code resolves hover via bridge | Manual: hover on a symbol in a `.py` file |

When all pass → Phase 3b (`IMPL-3B.md`).
