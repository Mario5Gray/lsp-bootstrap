# Acceptance Tests

Scenario-based tests written from the agent's perspective. Each scenario describes a real situation the agent encounters, what it asks the bridge, and what a correct outcome looks like. These are black-box — implementation details are invisible.

Pass/fail is determined by whether the agent gets information it can correctly act on, not whether the underlying code ran without error.

---

## Phase 3a

### A1 — Type resolution on a known symbol

**Context:** Agent is reviewing `worker_pool.py` and needs to know the type of `fut` before refactoring the return path.

**Action:** Agent calls `hover` at the line and column of `fut`.

**Pass:**
- Response contains the fully-resolved generic type (e.g. `Future[tuple[bytes, int]]`)
- Response is returned within 10 seconds on a cold start, within 500ms on a warm server
- No error is returned

**Fail:**
- Response is empty
- Response contains `Unknown` or an unresolved generic
- Tool errors with a timeout or subprocess failure

---

### A2 — Cross-file definition lookup

**Context:** Agent sees a call to `job.execute(self._worker)` and needs to find where `execute` is implemented.

**Action:** Agent calls `definition` at the position of `execute`.

**Pass:**
- Response contains an absolute file path and a line number pointing to the concrete method definition
- File path is within the workspace
- Line number is 1-based and points to the `def execute` line, not an import or stub

**Fail:**
- Response is empty
- Response points to a `.pyi` stub file when a concrete implementation exists
- File path is a URI (`file://...`) rather than an absolute path

---

### A3 — Type-resolved reference list

**Context:** Agent needs all call sites of `switch_mode` on `WorkerPool` specifically — not every method named `switch_mode` across the codebase.

**Action:** Agent calls `references` at the position of `switch_mode` in `worker_pool.py`.

**Pass:**
- Response lists only type-resolved references to this specific method
- Each result has an absolute file path, line, and column
- Results do not include unrelated methods that share the name

**Fail:**
- Response is empty when references exist
- Response includes references to a different `switch_mode` on a different class
- Response includes the definition site when `includeDeclaration` was not requested

---

### A4 — Diagnostics surface a real type error

**Context:** Agent wants to verify that a recent change hasn't introduced type errors in the demand-reload path.

**Action:** Agent calls `diagnostics` on a file known to have a type mismatch.

**Pass:**
- Response contains at least one diagnostic with `severity: "error"`
- `message` is human-readable and identifies the specific mismatch
- `line` and `column` point to the offending expression, not the top of the file

**Fail:**
- Response is empty on a file with a known error
- `severity` is `"warning"` for an actual type error
- Response arrives after a 10-second timeout

---

### A5 — Diagnostics returns empty on a clean file

**Context:** Agent checks a file after fixing all reported errors.

**Action:** Agent calls `diagnostics` on a file with no type errors.

**Pass:**
- Response is an empty list `[]`
- Response arrives within 10 seconds

**Fail:**
- Response contains false positives
- Tool errors instead of returning empty

---

### A6 — Unsupported file type returns a clear error

**Context:** Agent calls a tool on a file type with no configured LSP server (e.g. `.md`).

**Action:** Agent calls `hover` on a Markdown file.

**Pass:**
- Tool returns a descriptive error: unsupported file type, no LSP server configured
- Error message names the extension
- Bridge does not crash or hang

**Fail:**
- Tool returns empty result with no explanation
- Bridge crashes
- Error message is a raw Go panic trace

---

### A7 — Missing binary returns an actionable error

**Context:** Agent calls a tool on a Go file but `gopls` is not installed on the machine.

**Action:** Agent calls `hover` on a `.go` file.

**Pass:**
- Tool returns an error naming the missing binary (`gopls`) and its install command
- Bridge does not crash

**Fail:**
- Tool hangs indefinitely
- Error message does not identify what is missing or how to fix it

---

### A8 — Bridge recovers from language server crash

**Context:** `pyright-langserver` crashes mid-session (OOM, signal, etc.).

**Action:** Agent calls `hover` on a Python file immediately after the crash.

**Pass:**
- Bridge restarts the language server transparently
- The call that triggered the restart may return an error (acceptable)
- The next call succeeds and returns a correct result
- Agent does not need to restart Claude Code or the bridge

**Fail:**
- Bridge enters a broken state requiring a manual restart
- Subsequent calls all return errors after a single crash
- Bridge crashes alongside the language server

---

### A9 — Concurrent requests to different languages resolve independently

**Context:** Agent issues hover requests on a `.py` file and a `.go` file close together (e.g. during a cross-language analysis task).

**Action:** Two concurrent `hover` calls — one Python, one Go.

**Pass:**
- Both return correct results
- Results are not cross-contaminated (Python result for Python file, Go result for Go file)
- Two separate LSP processes are running (one per language)

**Fail:**
- One or both calls error
- Results are swapped or contain data from the wrong language server

---

### A10 — Cold start latency is within bounds

**Context:** First tool call of a session on a language whose LSP server has not yet started.

**Action:** Agent calls any tool on a Python file with no prior session activity.

**Pass:**
- Response arrives within 10 seconds
- Subsequent calls on the same language arrive within 500ms

**Fail:**
- First call times out
- Subsequent calls are still slow (server failed to stay alive)

---

## Phase 3b

### B1 — Rename returns a reviewable diff, nothing applied

**Context:** Agent wants to rename `_free_worker` across the project but needs to review the impact first.

**Action:** Agent calls `rename` at the position of `_free_worker` with `newName: "release_worker"`.

**Pass:**
- Response is a unified diff string with one block per affected file
- Diff shows all lines where `_free_worker` appears as changed
- Files on disk are byte-for-byte identical before and after the call
- Agent can read the diff and decide whether to apply it

**Fail:**
- Files on disk are modified
- Diff is empty when references exist
- Diff contains changes to files that don't reference the symbol
- Response is applied directly with no diff returned

---

### B2 — Rename diff covers all affected files

**Context:** `_free_worker` is referenced in three files.

**Action:** Agent calls `rename` on the symbol.

**Pass:**
- Diff contains three blocks, one per file
- Each block correctly shows the old and new name
- No affected file is missing from the diff

**Fail:**
- Diff covers only the file where the symbol is defined
- Diff covers more files than actually reference the symbol

---

### B3 — Incoming call hierarchy identifies all callers

**Context:** Agent needs to understand what calls `submit_job` before changing its signature.

**Action:** Agent calls `call_hierarchy_in` at the position of `submit_job`.

**Pass:**
- Response lists every function that directly calls `submit_job`
- Each entry has a name, absolute file path, and 1-based line/column pointing to the call site
- Results are type-resolved (not just name-matched)

**Fail:**
- Response is empty when callers exist
- Response includes functions that don't call `submit_job`
- Locations point to the definition rather than the call site

---

### B4 — Outgoing call hierarchy maps what a function calls

**Context:** Agent wants to understand the dependency footprint of `dispatch_task` before extracting it.

**Action:** Agent calls `call_hierarchy_out` at the position of `dispatch_task`.

**Pass:**
- Response lists every function directly called by `dispatch_task`
- Each entry has a name, file path, and location

**Fail:**
- Response is empty when callees exist
- Response includes callees of callees (should be one level only)

---

### B5 — Call hierarchy on a non-callable returns empty, not an error

**Context:** Agent mistakenly calls `call_hierarchy_in` on a variable rather than a function.

**Action:** Agent calls `call_hierarchy_in` at the position of a module-level variable.

**Pass:**
- Response is an empty list
- No error is returned
- Bridge does not crash

**Fail:**
- Tool returns an error
- Bridge crashes or hangs

---

### B6 — Signature help returns parameter names and types at a call site

**Context:** Agent is constructing a call to `WorkerPool.__init__` and needs the parameter list.

**Action:** Agent calls `signature_help` inside the argument list of a `WorkerPool(...)` call.

**Pass:**
- Response identifies the active parameter (if detectable)
- Response includes parameter names and types in human-readable form
- Response is a plain text string the agent can read directly

**Fail:**
- Response is empty when called inside a valid argument list
- Response contains raw LSP JSON rather than a formatted string

---

## Failure mode scenarios

### F1 — Bridge not running

**Context:** Agent tries to use an LSP tool but the bridge process has not been started.

**Expected:** Claude Code reports the MCP server is unreachable. Agent is told to run `./start-lsp.sh`.

---

### F2 — Language server permanently failed (restart limit exceeded)

**Context:** `gopls` binary is present but always exits immediately (corrupt install).

**Expected:** After 3 restart attempts, tool returns a permanent error: "go language server failed to start after 3 attempts — check gopls installation". Bridge remains alive for other language slots.

---

### F3 — File outside workspace

**Context:** Agent calls `hover` on an absolute path outside `LSP_WORKSPACE`.

**Expected:** Tool returns a clear error identifying the path and the workspace boundary. Bridge does not attempt to open the file.

---

### F4 — Large rename across many files

**Context:** A commonly-used symbol appears in 50+ files.

**Expected:** Rename completes and returns a diff. May be slow (acceptable). Response is not truncated. Files on disk unchanged.
