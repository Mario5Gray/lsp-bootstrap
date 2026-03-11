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
| `github.com/sergi/go-diff` | Unified diff for `rename` dry-run |

Everything else is stdlib: `os/exec`, `bufio`, `net/http`, `encoding/json`, `sync`, `path/filepath`.

---

## File Structure

```
lsp-mcp-bridge/
  main.go        # flag parsing, config load, wire manager into MCP server, start HTTP
  config.go      # reads env.lsp then env.custom into a Config struct; CLI flags override
  lsp_client.go  # one subprocess per language; JSON-RPC multiplexed by id; reader goroutine
  manager.go     # extension → slot routing; lazy-start; crash restart; clean shutdown
  tools_3a.go    # hover, definition, references, diagnostics
  tools_3b.go    # rename (dry-run diff), call_hierarchy_in, call_hierarchy_out, signature_help
  util.go        # path_to_uri, uri_to_path, make_position, workspace_edit_to_diff
  go.mod
  go.sum
```

---

## Extension Routing Table

Defined as a constant map in `manager.go`. Each slot maps to a binary path sourced from `Config`.

| Extension(s) | Slot | Binary (from env.lsp) |
|---|---|---|
| `.py` | `python` | `LSP_PYRIGHT_BIN` + `-- --stdio` |
| `.js` `.jsx` `.ts` `.tsx` `.mjs` | `typescript` | `LSP_TSS_BIN` + `-- --stdio` |
| `.rs` | `rust` | `LSP_RUST_BIN` |
| `.go` | `go` | `LSP_GOPLS_BIN` |
| `.kt` `.kts` | `kotlin` | `LSP_KOTLIN_BIN` |
| `.scala` `.sc` | `scala` | `LSP_METALS_BIN` |
| `.sh` `.bash` `.zsh` | `shell` | `LSP_BASH_BIN` + `-- --stdio` |
| `.yml` `.yaml` | `yaml` | `LSP_YAML_BIN` + `-- --stdio` |
| `.toml` | `toml` | `LSP_TAPLO_BIN` + `lsp stdio` |
| `.json` `.jsonc` | `json` | `LSP_JSON_BIN` + `-- --stdio` |
| `.html` `.htm` | `html` | `LSP_HTML_BIN` + `-- --stdio` |
| `.css` `.scss` `.less` | `css` | `LSP_CSS_BIN` + `-- --stdio` |

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
| `hover` | `textDocument/hover` | `filePath`, `line`, `col` | type + doc string |
| `definition` | `textDocument/definition` | `filePath`, `line`, `col` | target file + line |
| `references` | `textDocument/references` | `filePath`, `line`, `col` | `[{file, line, col}]` |
| `diagnostics` | `textDocument/publishDiagnostics` | `filePath` | `[{severity, message, line}]` |

### Phase 3b — Extended

| MCP Tool | LSP Method | Input | Output |
|---|---|---|---|
| `rename` | `textDocument/rename` | `filePath`, `line`, `col`, `newName` | unified diff per file (dry-run, no apply) |
| `call_hierarchy_in` | `callHierarchy/incomingCalls` | `filePath`, `line`, `col` | callers with locations |
| `call_hierarchy_out` | `callHierarchy/outgoingCalls` | `filePath`, `line`, `col` | callees with locations |
| `signature_help` | `textDocument/signatureHelp` | `filePath`, `line`, `col` | parameter names + types |

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

### 2 — `util.go` · Util
Pure functions: `PathToURI`, `URIToPath`, `MakePosition` (0-based LSP coords from 1-based tool inputs), `WorkspaceEditToDiff` (stub returning empty string until Step 5).

### 3 — `lsp_client.go` · LspClient
Owns one `exec.Cmd` subprocess. Sends `Content-Length`-framed JSON-RPC over stdin; reads responses on a goroutine. Multiplexes requests by `id` via `map[int]chan`. Dispatches notifications to one-shot listeners. Exposes `Initialize`, `Request`, `Notify`.

### 4 — `manager.go` · Manager
Holds the extension → slot routing table and a `map[string]*LspClient`. `GetClient(filePath)` resolves the slot, lazy-starts the client if needed (runs LSP handshake), and returns it. Restarts dead clients. Shuts all down on `SIGTERM`.

### 5 — `tools_3a.go` + `main.go` · Phase 3a tools + server
Implements `hover`, `definition`, `references`, `diagnostics` as MCP tool handlers against `Manager`. Wires into `mcp-go` HTTP server in `main.go`. **First end-to-end runnable state** — validate with `curl`, then update `start-lsp.sh` stub.

### 6 — `util.go` · WorkspaceEditToDiff (complete)
Reads affected files, applies `WorkspaceEdit` edits in reverse line order, produces unified diff via `go-diff`. No disk writes.

### 7 — `tools_3b.go` · Phase 3b tools
Implements `rename` (returns diff from Step 6), `call_hierarchy_in`, `call_hierarchy_out`, `signature_help`. Register in `main.go`.

### 8 — Integration
Update `.mcp.json` to the single HTTP entry. Update `start-lsp.sh` to launch the binary. Smoke-test all tools against a real workspace.

---

**Gate:** Phase 3a (Steps 1–5) ships independently. Phase 3b (Steps 6–7) starts only after 3a is validated.

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

`languageId` values: `python`, `javascript`, `typescript`, `rust`, `go`, `kotlin`, `scala`, `shellscript`, `yaml`, `toml`, `json`, `html`, `css`.

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
4. Pass original lines and modified lines to `go-diff` `DiffLines` to produce the unified diff.

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
        filePath := req.Params.Arguments["filePath"].(string)
        line     := int(req.Params.Arguments["line"].(float64))
        column   := int(req.Params.Arguments["column"].(float64))
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

## Success Criteria

| Phase | Done when |
|---|---|
| 3a | `hover` on a Python symbol returns the correct type; `diagnostics` on a file with a deliberate type error returns the error |
| 3b | `rename` on a symbol returns a correct unified diff across all affected files without modifying disk |
