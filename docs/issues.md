# Issues. OnGoing

## First run



  ---
  1. Tool output formats are prose, not schemas

  hover → "type + doc string", call_hierarchy_in → "callers with locations", signature_help → "parameter names + types" — none define the actual shape returned to the agent. Is hover a plain string or a struct? Are locations absolute or relative paths?
  This needs concrete output schemas before implementation.

  2. -- in routing table args is wrong

  The table shows LSP_PYRIGHT_BIN + "-- --stdio" etc. — that -- separator was inherited from the mcp-language-server invocation pattern. In the bridge we call the LSP binary directly, so -- has no meaning and must be dropped. The args should just be
  ["--stdio"] or nothing.

  3. bridge.go vs main.go split is undefined

  The file structure lists both but assigns overlapping responsibilities (both do server setup). The build order only mentions main.go. It's unclear what lives in bridge.go that doesn't belong in main.go. Either collapse them or define the split
  explicitly.

  4. Restart policy is unspecified

  "Restarts dead clients" — how many times? Immediate or with backoff? What if it crashes on every initialize? Without a defined policy this becomes an infinite restart loop in pathological cases.

  5. "Clean shutdown" is undefined

  No timeout, no force-kill fallback. What if a language server ignores shutdown + exit? Needs: send LSP shutdown request, wait N seconds, send exit notification, SIGKILL after timeout.

  6. Multiple publishDiagnostics window is a magic number

  Edge case #3 says "500ms follow-up window" with no justification. This is a guess that will be wrong for slow indexers and wasteful for fast ones. Should either be configurable or replaced with a defined heuristic (e.g. wait until no notification
  arrives for 200ms, cap at 5s).

  7. gopls rootUri fallback is undefined

  Edge case #15 says "walk up to find nearest go.mod" but doesn't say what to do if there's no go.mod (non-module Go code, or a file outside the workspace). Needs an explicit fallback — probably LSP_WORKSPACE.

  8. .sc languageId is unspecified

  .sc (Scala script) is in the routing table but missing from the languageId values list. Metals expects scala for .sc — should be stated explicitly.

  9. Success criteria only exist for 3a and 3b

  Steps 1–4 have no validation gate. A broken lsp_client.go won't be caught until step 5. Each build order step needs a one-line "done when" condition.

  --

  --

## Phase 3a / 3b alignment issues
 
Errors (will cause bugs or build failures)

1. go-diff dropped from go.mod
go mod tidy removed github.com/sergi/go-diff because the stub WorkspaceEditToDiff doesn't import it. IMPL-3B.md Step 6 says "already in go.mod" — that's false now. Step 6 must add it back with go get github.com/sergi/go-diff@v1.4.0 before importing.

2. Acceptance test line numbers are all off by one from the actual fixture

The fixture (sample.py) has:
- def submit(...) — line 16 (DEFINITION)
- return w.submit(b"ping") — line 23 (REFERENCE)
- result = fut.result() — line 27 (hover on fut)

The acceptance tests assert:
- A1: line: 28 for hover on fut → should be 27
- A2: line: 22 for the REFERENCE → should be 23
- A3: line: 15 for DEFINITION → should be 16

B1, B2, B3, B6 in acceptance_test.go also hardcode line: 15 for Worker.submit. All wrong by the same delta — the docstring in the fixture is two lines longer than what the tests were written against.

3. Manager struct in IMPL-3A.md has a routing field that doesn't exist

The doc shows:
type Manager struct {
    cfg     *Config
    slots   map[string]*slotState
    routing map[string]string   // ← doesn't exist in the actual code
    mu      sync.Mutex
}
The actual Manager has defs map[string]slotDef instead. Routing is done via the package-level extToSlot, not a struct field.

4. sample_multifile/dispatcher.py import won't resolve in pyright

from worker import Worker requires either a pyrightconfig.json pointing at sample_multifile/ as the project root, or an __init__.py. Without that pyright won't find cross-file references and the B2 (rename covers all files) and B3 (call hierarchy)
acceptance tests will fail.

---
Inconsistencies between docs and code

5. didOpen params use ext in IMPL-3A Step 5, should be langID

The pseudocode says:
client.Notify("textDocument/didOpen", didOpenParams(uri, ext, content))
But the actual function signature and call is didOpenParams(uri, langID, content) where langID is the result of LanguageID(ext). The ext is never passed directly.

6. main.go pseudocode shows the broken blocking pattern

IMPL-3A.md Step 5 says:
// 8. log.Fatal(httpServer.Start(":" + cfg.Port))
This is the pattern that caused the Ctrl+C hang. The actual fix runs Start in a goroutine and blocks on signal. The doc still shows the old broken approach.

7. env.go in Step 0 describes a NewMockClient() helper that doesn't exist

Step 0 says:
env.go   # NewMockClient() helper; TempEnvLsp() fixture loader
No NewMockClient exists anywhere. The in-process client setup is NewLspClientFromPipes() in lsp_client.go. The description is stale.

8. lsp_client.go mutex note contradicts MCP-BRIDGE.md

IMPL-3A Step 3: "pending and listeners share one mutex"
MCP-BRIDGE.md Concurrency Reference: "Notification listeners map: separate sync.Mutex from pending — must not share lock with pending"

The actual code shares one mutex. These two documents disagree on the correct approach.

9. Diagnostics timer description is misleading

IMPL-3A Step 5 says: "on first receive: reset 200ms idle timer" — implying the timer starts only after the first notification. The actual code starts the timer immediately (time.NewTimer(diagIdleTimeout) before the select loop). If a slow server takes
>200ms to emit its first diagnostic, the current code returns empty. The description hides this behavior.

10. Success criteria references wrong fixture file

IMPL-3A success criteria says:
"Integration test on sample.py with deliberate type mismatch"

Should be sample_error.py. sample.py is the clean fixture.

---
Duplication

11. start-lsp.sh update appears in both 3A and 3B with different content

3A (Step 5 "Done when") has a plain nohup block.
3B (Step 8) has the same block plus a go build auto-build guard.

The 3B version is strictly better. The 3A mention should be removed or replaced with "see Step 8 in IMPL-3B.md".

12. .mcp.json update section is identical in both docs

Both docs tell you to replace the per-language entries with the single HTTP entry. It should only be in 3B Step 8.

---
Minor / Low priority

13. Start(ctx) accepts ctx but ignores it

IMPL-3A shows func (c *LspClient) Start(ctx context.Context) error. The actual code accepts ctx but doesn't wire it to exec.CommandContext, so cancelling ctx doesn't kill the subprocess. Not a bug today but sets a false expectation.

14. IMPL-3B outgoingCalls uses fromRanges field name but the doc doesn't explain it

The JSON example for outgoing calls includes "fromRanges" — which is counterintuitive (these are ranges IN the caller where the callee is invoked). The doc should clarify we use to.range.start (definition of the callee) not fromRanges (the call sites),
  or explicitly state which is returned and why.

---
Summary table:

┌─────┬─────────────────┬───────────────────────────┬────────────────────────────────────────────────┐
│  #  │    Severity     │         Location          │                   Fix needed                   │
├─────┼─────────────────┼───────────────────────────┼────────────────────────────────────────────────┤
│ 1   │ Build break     │ go.mod / IMPL-3B Step 6   │ Re-add go-diff in Step 6                       │
├─────┼─────────────────┼───────────────────────────┼────────────────────────────────────────────────┤
│ 2   │ Test bug        │ acceptance_test.go        │ Fix A1→27, A2→23, A3→16, B-series→16           │
├─────┼─────────────────┼───────────────────────────┼────────────────────────────────────────────────┤
│ 3   │ Doc error       │ IMPL-3A Step 4            │ Remove routing field from struct               │
├─────┼─────────────────┼───────────────────────────┼────────────────────────────────────────────────┤
│ 4   │ Integration bug │ fixtures/sample_multifile │ Add pyrightconfig.json                         │
├─────┼─────────────────┼───────────────────────────┼────────────────────────────────────────────────┤
│ 5   │ Doc error       │ IMPL-3A Step 5            │ ext → langID                                   │
├─────┼─────────────────┼───────────────────────────┼────────────────────────────────────────────────┤
│ 6   │ Doc error       │ IMPL-3A Step 5            │ Fix main.go pseudocode                         │
├─────┼─────────────────┼───────────────────────────┼────────────────────────────────────────────────┤
│ 7   │ Doc error       │ IMPL-3A Step 0            │ Remove NewMockClient reference                 │
├─────┼─────────────────┼───────────────────────────┼────────────────────────────────────────────────┤
│ 8   │ Contradiction   │ IMPL-3A vs MCP-BRIDGE.md  │ Pick one — shared or separate mutex            │
├─────┼─────────────────┼───────────────────────────┼────────────────────────────────────────────────┤
│ 9   │ Misleading      │ IMPL-3A Step 5            │ Timer starts immediately, not on first receive │
├─────┼─────────────────┼───────────────────────────┼────────────────────────────────────────────────┤
│ 10  │ Wrong file      │ IMPL-3A success criteria  │ sample.py → sample_error.py                    │
├─────┼─────────────────┼───────────────────────────┼────────────────────────────────────────────────┤
│ 11  │ Duplication     │ Both docs                 │ Remove start-lsp.sh from 3A                    │
├─────┼─────────────────┼───────────────────────────┼────────────────────────────────────────────────┤
│ 12  │ Duplication     │ Both docs                 │ Remove .mcp.json from 3A                       │
├─────┼─────────────────┼───────────────────────────┼────────────────────────────────────────────────┤
│ 13  │ Misleading      │ IMPL-3A Step 3            │ Note ctx is not wired to process lifecycle     │
├─────┼─────────────────┼───────────────────────────┼────────────────────────────────────────────────┤
│ 14  │ Unclear         │ IMPL-3B call hierarchy    │ Clarify fromRanges vs to.range                 │
└─────┴─────────────────┴───────────────────────────┴────────────────────────────────────────────────┘

  --FIXED--
 Pre-existing, not new:

  1. B2 acceptance test uses sample.py (single file) but is named "covers all files" — B2 won't actually exercise cross-file rename. It will only verify the diff contains @@ hunks within the same file. The sample_multifile/ fixture exists now but isn't
  wired into B2. To properly test cross-file rename, B2 would need to point at sample_multifile/dispatcher.py. Low priority since B2 is still skipped.
  2. Cold-start diagnostics window is 200ms — if pyright takes longer on first open (cold cache, large file), the call returns empty. Documented in the updated IMPL-3A Step 5, but the code isn't changed. The right fix (make initial timeout configurable
  or use a longer first-receive deadline) is deferred.
  3. sample_multifile rootUri mismatch — integration tests for tools_3b will need to initialize pyright with sample_multifile/ as workspace root, not the overall LSP_WORKSPACE. The fixture directory has pyrightconfig.json for this, but the integration
  test harness will need to pass the right workspace path when calling mgr.GetClient. Not an issue until Step 7 integration tests are written.

  Nothing to fix now — issues.md is clean.

  --

Immediate (unblock the bridge for daily use)

  1. Smoke test start-lsp.sh — run it, confirm the binary builds, server starts, `curl http://localhost:7890/health` returns `{"status":"ok",...}`, then stop-lsp.sh cleans up.
     Note: GET /mcp hangs — that endpoint only handles MCP JSON-RPC POST requests. Use /health for liveness checks.
  2. Restart Claude Code — .mcp.json now has the lsp HTTP entry; Claude Code won't see the 8 new tools until it reloads.
  3. Manual smoke test via Claude Code — call hover, rename, call_hierarchy_in on a real file in the workspace, confirm non-error results.

  ---
  Test coverage (still incomplete)

  4. Integration tests for 3b — 8 tests listed in IMPL-3B Step 7, //go:build integration, against real pyright using sample_multifile/. These are the gate before unskipping acceptance tests.
  5. Unskip B1–B6 and F4 acceptance tests — only after integration tests pass consistently.

  ---
  TODO.md items remaining

  6. Update CLAUDE.md tool selection table — treesitter vs. LSP decision guide (which tool to reach for in which situation). This is listed in Phase 3b TODO.
  7. TypeScript support — Manager currently only routes .py files to pyright. slotArgs has a typescript slot but it's not wired into GetClient routing for .ts/.tsx/.js files.

  ---
  Future / lower priority

  8. Integration test with a pinned real-world repo — as discussed, a submodule fixture for realistic cross-file complexity.
  9. Additional language servers — the table in TODO.md lists Go (gopls), Rust (rust-analyzer), and others if you want to expand beyond Python/TypeScript.

  ---

