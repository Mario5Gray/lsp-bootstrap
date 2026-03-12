# Implementation Guide — Phase 3b

Coding-focused guide for Steps 6–8. Starts where `IMPL-3A.md` stops. Reference `MCP-BRIDGE.md` for rationale, protocol notes, and edge case detail.

**Prerequisite:** Phase 3a success criteria in `IMPL-3A.md` must all pass before starting here.

**Deliverable:** `rename`, `call_hierarchy_in`, `call_hierarchy_out`, and `signature_help` tools running in the same HTTP bridge; acceptance tests B1–B6 and F4 unskipped.

---

## Repo layout additions

```
lsp-mcp-bridge/
  tools_3b.go               # new — rename, call_hierarchy_in/out, signature_help
  util.go                   # update — complete WorkspaceEditToDiff stub
  testutil/fixtures/
    sample_multifile/       # new — multi-file fixture for rename/call hierarchy
      worker.py
      dispatcher.py
```

---

## Step 6 — `util.go` · `WorkspaceEditToDiff` (complete)

Replace the stub added in Step 2 with a real implementation.

### Dependency

`go mod tidy` drops unused imports, so `go-diff` may have been removed if nothing imports it yet. Before implementing this step, confirm it is present and re-add if needed:

```bash
grep sergi/go-diff go.mod || go get github.com/sergi/go-diff@v1.4.0
```

Then import:

```go
import "github.com/sergi/go-diff/diffmatchpatch"
```

### Function signature (unchanged)

```go
func WorkspaceEditToDiff(edit map[string]any, workspace string) string
```

- `edit` — raw `json.RawMessage` unmarshalled into `map[string]any` (the full LSP `WorkspaceEdit` object)
- `workspace` — used to compute relative paths for diff headers
- Returns a unified diff string; empty string if the edit is nil or has no changes
- **Never writes to disk**

### Algorithm

```
1. Collect file edits — two forms, check documentChanges first:

   documentChanges (preferred, modern servers):
     []{ textDocument.uri, edits: []{ range, newText } }

   changes (legacy):
     { "file:///path": []{ range, newText } }

2. For each URI:
   a. Read file into string (original).
   b. Split into []string lines (preserve \n on each line).
   c. Sort edits for this file by start.line DESCENDING, then start.character DESCENDING.
      (Reverse order prevents earlier edits from shifting line/column offsets of later ones.)
   d. Apply each edit:
      - Single-line edit (start.line == end.line):
          replace characters [start.character, end.character) on that line with newText
      - Multi-line edit (start.line < end.line):
          collect lines[start.line..end.line] (inclusive)
          replace the span from start.character on the first line through
          end.character on the last line with newText
          collapse back to a single slice entry (newText may contain \n — re-split if needed)
   e. Join lines → modified string.
   f. Produce diff (go-diff three-step — no DiffLines function):
      dmp := diffmatchpatch.New()
      a, b, lineArr := dmp.DiffLinesToChars(original, modified)
      diffs := dmp.DiffMain(a, b, false)
      diffs = dmp.DiffCharsToLines(diffs, lineArr)
      patches := dmp.PatchMake(original, diffs)
      patchText := dmp.PatchToText(patches)
   g. Prepend git-style file header:
      rel, _ := filepath.Rel(workspace, URIToPath(uri))
      header := "--- a/" + rel + "\n+++ b/" + rel + "\n"
      append header + patchText to output

3. Return concatenated diff blocks for all files.
```

### Edge cases to handle explicitly

| Case | Handling |
|---|---|
| `edit` is nil or neither field is present | return `""` |
| `newText` contains `\n` (multi-line replacement) | re-split on `\n` before inserting into line buffer |
| URI is outside `workspace` | use absolute path in header instead of relative |
| File does not exist on disk | return error text in diff header comment |
| Empty edit list for a URI | skip that file |

### Done when

| Test | Scenario |
|---|---|
| `TestWorkspaceEditToDiffSingleLine` | single character-range edit → correct `@@` hunk |
| `TestWorkspaceEditToDiffMultiLine` | edit spanning lines 10–12 → collapsed correctly |
| `TestWorkspaceEditToDiffMultiFile` | two-file edit → two `---/+++` blocks |
| `TestWorkspaceEditToDiffDocumentChanges` | `documentChanges` form produces same output as `changes` |
| `TestWorkspaceEditToDiffNoDiskWrite` | files unchanged after call |
| `TestWorkspaceEditToDiffNewlineInReplacement` | `newText` with embedded `\n` handled correctly |

---

## Step 7 — `tools_3b.go`

### New fixture files

**`testutil/fixtures/sample_multifile/worker.py`**

```python
"""Multi-file fixture — worker definition."""
from __future__ import annotations


class Worker:
    def __init__(self, worker_id: int) -> None:
        self.worker_id = worker_id

    def run(self) -> str:  # DEFINITION — line 9
        return f"worker-{self.worker_id}"
```

**`testutil/fixtures/sample_multifile/dispatcher.py`**

```python
"""Multi-file fixture — calls Worker.run."""
from __future__ import annotations
from worker import Worker


def dispatch(worker_id: int) -> str:
    w = Worker(worker_id)
    return w.run()  # REFERENCE — line 8
```

These are used by the rename and call hierarchy tests.

### Tool handlers

```go
func renameHandler(mgr *Manager) server.ToolHandlerFunc
func callHierarchyInHandler(mgr *Manager) server.ToolHandlerFunc
func callHierarchyOutHandler(mgr *Manager) server.ToolHandlerFunc
func signatureHelpHandler(mgr *Manager) server.ToolHandlerFunc
```

Register all four in `main.go` after the 3a tools.

---

### `rename`

**MCP inputs:** `filePath string`, `line int`, `column int`, `newName string`

**Output:** unified diff string (one `---/+++` block per affected file)

**Protocol:**

```
1. client = mgr.GetClient(filePath)
2. uri, content, langID = openFile(filePath)
3. client.Notify("textDocument/didOpen", didOpenParams(uri, langID, content))
4. raw, err = client.Request("textDocument/rename", {
       textDocument: {uri},
       position: MakePosition(line, col),
       newName: newName,
   })
5. client.Notify("textDocument/didClose", didCloseParams(uri))
6. diff = WorkspaceEditToDiff(unmarshal(raw), cfg.Workspace)
7. return diff
```

**Output type:** plain string (the diff text). If the server returns null (no rename possible at that position), return an empty string — not an error.

**Never apply the edit.** Pass the raw `WorkspaceEdit` directly to `WorkspaceEditToDiff`. Do not touch `os.WriteFile`.

**WorkspaceEditToDiff needs `workspace`:** pass `mgr.cfg.Workspace`.

---

### `call_hierarchy_in` and `call_hierarchy_out`

**MCP inputs:** `filePath string`, `line int`, `column int`

**Output:** `[]{name string, filePath string, line int, column int}` — 1-based, absolute paths

**Protocol — two sequential requests:**

```
Step 1 — prepare:
  raw = client.Request("textDocument/prepareCallHierarchy", {
      textDocument: {uri},
      position: MakePosition(line, col),
  })
  if raw == null || raw == [] → return [], nil  (not callable — empty, not error)
  item = raw[0]   // CallHierarchyItem

Step 2 — query:
  // for call_hierarchy_in:
  raw = client.Request("callHierarchy/incomingCalls", { item: item })
  // for call_hierarchy_out:
  raw = client.Request("callHierarchy/outgoingCalls", { item: item })
```

**Parsing incomingCalls result:**

```json
[
  {
    "from": {
      "name": "dispatch",
      "uri": "file:///path/dispatcher.py",
      "range": { "start": { "line": 5, "character": 4 } }
    },
    "fromRanges": [...]
  }
]
```

Extract `from.name`, `from.uri` → `URIToPath`, `from.range.start` → 1-based.

**Parsing outgoingCalls result:**

```json
[
  {
    "to": {
      "name": "run",
      "uri": "file:///path/worker.py",
      "range": { "start": { "line": 8, "character": 4 } }
    },
    "fromRanges": [{ "start": { "line": 13, "character": 11 } }]
  }
]
```

- `to` — `CallHierarchyItem` for the callee (the function being called). `to.range.start` is where the callee is **defined**.
- `fromRanges` — ranges within the **calling** function's body where the callee is invoked (the actual call sites).

Extract `to.name`, `to.uri` → `URIToPath`, `to.range.start` → 1-based. This navigates to the callee's definition, which is the most useful location for an agent reviewing outgoing calls. If you instead want to navigate to the call site within the caller, use `fromRanges[0].start`.

**Output type:**

```go
type CallSite struct {
    Name     string `json:"name"`
    FilePath string `json:"filePath"`
    Line     int    `json:"line"`
    Column   int    `json:"column"`
}
```

---

### `signature_help`

**MCP inputs:** `filePath string`, `line int`, `column int`

**Output:** plain text string — the active signature with parameter names and types

**Protocol:**

```
1–3. open file (same as hover)
4. raw = client.Request("textDocument/signatureHelp", {
       textDocument: {uri},
       position: MakePosition(line, col),
   })
5. close file
6. parse + return text
```

**Parsing:**

```json
{
  "signatures": [
    {
      "label": "run(self) -> str",
      "documentation": "...",
      "parameters": [
        { "label": [5, 9] }
      ]
    }
  ],
  "activeSignature": 0,
  "activeParameter": 0
}
```

Return `signatures[activeSignature].label`. If `signatures` is empty or null, return `""`.

---

### Registration in `main.go`

```go
s.AddTool(mcp.NewTool("rename",
    mcp.WithDescription("Rename a symbol and return a unified diff (nothing applied to disk)"),
    mcp.WithString("filePath", mcp.Required(), mcp.Description("Absolute path to the source file")),
    mcp.WithNumber("line",     mcp.Required(), mcp.Description("Line number (1-based)")),
    mcp.WithNumber("column",   mcp.Required(), mcp.Description("Column number (1-based)")),
    mcp.WithString("newName",  mcp.Required(), mcp.Description("New symbol name")),
), renameHandler(mgr))

s.AddTool(mcp.NewTool("call_hierarchy_in",
    mcp.WithDescription("Return all callers of the symbol at a position"),
    mcp.WithString("filePath", mcp.Required(), mcp.Description("Absolute path to the source file")),
    mcp.WithNumber("line",     mcp.Required(), mcp.Description("Line number (1-based)")),
    mcp.WithNumber("column",   mcp.Required(), mcp.Description("Column number (1-based)")),
), callHierarchyInHandler(mgr))

s.AddTool(mcp.NewTool("call_hierarchy_out",
    mcp.WithDescription("Return all callees of the symbol at a position"),
    mcp.WithString("filePath", mcp.Required(), mcp.Description("Absolute path to the source file")),
    mcp.WithNumber("line",     mcp.Required(), mcp.Description("Line number (1-based)")),
    mcp.WithNumber("column",   mcp.Required(), mcp.Description("Column number (1-based)")),
), callHierarchyOutHandler(mgr))

s.AddTool(mcp.NewTool("signature_help",
    mcp.WithDescription("Return parameter names and types for the call at a position"),
    mcp.WithString("filePath", mcp.Required(), mcp.Description("Absolute path to the source file")),
    mcp.WithNumber("line",     mcp.Required(), mcp.Description("Line number (1-based)")),
    mcp.WithNumber("column",   mcp.Required(), mcp.Description("Column number (1-based)")),
), signatureHelpHandler(mgr))
```

### Done when

**Unit tests (mock LSP, no real binary):**

| Test | Scenario |
|---|---|
| `TestRenameHandlerCallsRenameRequest` | mock returns a `WorkspaceEdit`; handler calls `textDocument/rename` with correct params |
| `TestRenameHandlerNullResult` | mock returns null; handler returns empty string, not error |
| `TestRenameHandlerNoDiskWrite` | fixture files unchanged after handler call |
| `TestCallHierarchyInTwoStepProtocol` | mock records `prepareCallHierarchy` then `incomingCalls` in order |
| `TestCallHierarchyInNonCallableReturnsEmpty` | prepare returns null; handler returns `[]`, not error |
| `TestCallHierarchyOutTwoStepProtocol` | mock records `prepareCallHierarchy` then `outgoingCalls` in order |
| `TestSignatureHelpReturnsLabel` | mock returns signature; handler extracts `label` |
| `TestSignatureHelpEmptyReturnsEmpty` | mock returns `{signatures: []}` or null; handler returns `""` |

**Integration tests (`//go:build integration`, real LSP binary):**

| Test | Scenario |
|---|---|
| `TestRenameDiffCorrect` | rename `run` → `execute` in `sample_multifile`; diff covers both files |
| `TestRenameSingleFile` | symbol only in `worker.py`; diff has one block |
| `TestRenameNoDiskWrite` | files byte-for-byte identical before and after |
| `TestRenameDocumentChangesForm` | server returns `documentChanges` form; diff still correct |
| `TestCallHierarchyInReturnsCallers` | `dispatch` returned as caller of `Worker.run` |
| `TestCallHierarchyOutReturnsCallees` | `Worker` and `run` returned as callees of `dispatch` |
| `TestCallHierarchyNonCallable` | position on `worker_id` field → empty result, no error |
| `TestSignatureHelpReturnsParams` | inside `Worker(worker_id)` call; `worker_id: int` in result |

**Acceptance tests to unskip (remove `t.Skip`):**

| Test | File |
|---|---|
| `TestB1_RenameReturnsDiffNothingApplied` | acceptance_test.go |
| `TestB2_RenameDiffCoversAllFiles` | acceptance_test.go |
| `TestB3_CallHierarchyInReturnsCallers` | acceptance_test.go |
| `TestB4_CallHierarchyOutReturnsCallees` | acceptance_test.go |
| `TestB5_CallHierarchyOnNonCallableReturnsEmpty` | acceptance_test.go |
| `TestB6_SignatureHelpReturnsParams` | acceptance_test.go |
| `TestF4_LargeRenameCompletesWithoutTruncation` | acceptance_test.go — also needs multi-file fixture |

---

## Step 8 — Integration

### `start-lsp.sh`

Replace the Phase 3 TODO block with:

```bash
BRIDGE="$SCRIPT_DIR/lsp-mcp-bridge/lsp-mcp-bridge"
if [ ! -f "$BRIDGE" ]; then
    echo "Building lsp-mcp-bridge..."
    (cd "$SCRIPT_DIR/lsp-mcp-bridge" && go build -o lsp-mcp-bridge .)
fi
mkdir -p "$(dirname "$LSP_LOG")"
nohup "$BRIDGE" \
    --workspace "$LSP_WORKSPACE" \
    --port      "$LSP_PORT"      \
    --env-lsp   "$SCRIPT_DIR/env.lsp" \
    >> "$LSP_LOG" 2>&1 &
echo $! > "$LSP_PID"
echo "Started (pid $(cat "$LSP_PID")) → http://localhost:$LSP_PORT/mcp"
```

### `.mcp.json`

Replace all per-language `mcp-language-server` entries with a single HTTP entry:

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

Restart Claude Code after updating.

### Done when

| Check | How |
|---|---|
| All B-series acceptance tests pass | `go test -tags acceptance ./...` |
| `rename` on a Python symbol returns correct diff | Manual: rename `run` in `worker.py` |
| Renamed files on disk unchanged | `git diff` clean after rename tool call |
| `call_hierarchy_in` on `Worker.run` lists `dispatch` | Manual via curl or Claude Code |
| `.mcp.json` has only the HTTP entry | `cat .mcp.json` |
| Claude Code resolves all 8 tools | Manual smoke test via Claude Code |

---

## Edge cases specific to Phase 3b

### `prepareCallHierarchy` may return a single item or an array

Some servers return a `CallHierarchyItem` object directly; others return `[CallHierarchyItem]`. Handle both:

```go
// try array first
var items []json.RawMessage
if err := json.Unmarshal(raw, &items); err != nil || len(items) == 0 {
    // try single object
    var item json.RawMessage
    if err2 := json.Unmarshal(raw, &item); err2 != nil {
        return nil  // not callable
    }
    items = []json.RawMessage{item}
}
```

### `rename` may return null for unsupported symbols

If `textDocument/rename` returns JSON `null`, the symbol is not renameable at that position (e.g. a builtin, or the cursor is on whitespace). Return empty string — not an error.

### `PatchToText` produces GNU patch format, not git format

`go-diff` hunks look like:

```
@@ -10,3 +10,3 @@
```

Prepend `--- a/<rel>\n+++ b/<rel>\n` manually. Without the headers, tools that consume the diff (e.g. `git apply`) will reject it.

### `WorkspaceEditToDiff` with empty `newText`

Empty `newText` is a valid deletion edit — the range is replaced with nothing. The algorithm handles this correctly if `newText == ""` is passed through without special-casing.

### `signatureHelp` `activeSignature` may be null

Some servers omit `activeSignature` when there is only one candidate. Default to index 0.

---

## Concurrency notes for 3b

No new concurrency primitives are needed. All 3b handlers follow the same open/request/close pattern as 3a handlers and call `mgr.GetClient` which already holds the slot mutex. The only new state is transient (the `prepareCallHierarchy` result lives on the stack).

`WorkspaceEditToDiff` is stateless and reads files only — no mutex needed.

---

## Phase 3b success criteria

| Check | How |
|---|---|
| `rename` returns correct unified diff | Integration test + acceptance test B1 |
| Renamed files on disk unchanged | `TestRenameNoDiskWrite` + acceptance test B1 |
| Diff covers all affected files | Acceptance test B2 |
| `call_hierarchy_in` returns correct callers | Acceptance test B3 |
| `call_hierarchy_out` returns correct callees | Acceptance test B4 |
| Non-callable returns empty (not error) | Acceptance test B5 |
| `signature_help` returns parameter list | Acceptance test B6 |
| All B-series + F4 acceptance tests pass | `go test -tags acceptance ./...` |
