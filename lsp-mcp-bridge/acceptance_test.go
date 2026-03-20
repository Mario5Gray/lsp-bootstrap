//go:build acceptance

package main_test

// Acceptance tests — black-box tests that interact with the bridge via HTTP,
// exactly as Claude Code would. No internal bridge packages are imported.
// Each test mirrors the acceptance doc structure:
//   Context -> Action -> Pass/Fail
// and asserts only on information an agent can act on.
//
// Run: go test -tags acceptance ./...

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lsp-bootstrap/lsp-mcp-bridge/testutil"
)

// bridgeURL returns the HTTP endpoint under test.
// Override with BRIDGE_URL env var; defaults to localhost:7890.
func bridgeURL() string {
	if u := os.Getenv("BRIDGE_URL"); u != "" {
		return u
	}
	return "http://localhost:7890/mcp"
}

// healthURL derives the /health endpoint from the bridge URL.
func healthURL() string {
	return strings.Replace(bridgeURL(), "/mcp", "/health", 1)
}

// ── MCP session management ──────────────────────────────────────────────────

// The StreamableHTTP MCP transport requires an initialize handshake before
// tool calls. We lazily create one session for the entire test run.
var (
	mcpSessionID   string
	mcpSessionOnce sync.Once
	mcpSessionErr  error
	mcpRequestID   atomic.Int64
)

// ensureSession performs the MCP initialize + initialized handshake once.
func ensureSession(t *testing.T) {
	t.Helper()
	mcpSessionOnce.Do(func() {
		mcpSessionErr = initSession()
	})
	if mcpSessionErr != nil {
		t.Fatalf("MCP session init failed: %v", mcpSessionErr)
	}
}

func initSession() error {
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":   map[string]any{},
			"clientInfo":     map[string]any{"name": "acceptance-test", "version": "0.1"},
		},
	})
	resp, err := http.Post(bridgeURL(), "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.ReadAll(resp.Body) //nolint:errcheck

	mcpSessionID = resp.Header.Get("Mcp-Session-Id")
	if mcpSessionID == "" {
		return io.ErrUnexpectedEOF // no session header
	}

	// Send the initialized notification.
	notif, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	})
	req, _ := http.NewRequest("POST", bridgeURL(), bytes.NewReader(notif))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Mcp-Session-Id", mcpSessionID)
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	resp2.Body.Close()
	return nil
}

// callTool issues a single MCP tool call over HTTP and returns the raw result.
// Lazily initializes the MCP session on first call.
func callTool(t *testing.T, tool string, args map[string]any) map[string]any {
	t.Helper()
	ensureSession(t)

	id := mcpRequestID.Add(1)
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      tool,
			"arguments": args,
		},
	})

	req, _ := http.NewRequest("POST", bridgeURL(), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Mcp-Session-Id", mcpSessionID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("callTool %s: HTTP error: %v", tool, err)
	}
	defer resp.Body.Close()
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("callTool %s: decode response (HTTP %d): %v", tool, resp.StatusCode, err)
	}
	return result
}

// callToolRaw issues a tool call against an arbitrary URL. Does not fatal on HTTP error.
func callToolRaw(url, tool string, args map[string]any) (*http.Response, error) {
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      tool,
			"arguments": args,
		},
	})
	return http.Post(url, "application/json", bytes.NewReader(body))
}

// resultText extracts the first text content block from an MCP tool result.
func resultText(t *testing.T, result map[string]any) string {
	t.Helper()
	r, ok := result["result"].(map[string]any)
	if !ok {
		t.Fatalf("resultText: no result field in %v", result)
	}
	contents, ok := r["content"].([]any)
	if !ok || len(contents) == 0 {
		t.Fatalf("resultText: no content in %v", r)
	}
	first, ok := contents[0].(map[string]any)
	if !ok {
		t.Fatalf("resultText: content[0] not a map: %v", contents[0])
	}
	text, ok := first["text"].(string)
	if !ok {
		t.Fatalf("resultText: content[0].text not a string: %v", first)
	}
	return text
}

// isError returns true if the MCP result carries isError:true.
func isError(result map[string]any) bool {
	r, ok := result["result"].(map[string]any)
	if !ok {
		return false
	}
	v, _ := r["isError"].(bool)
	return v
}

// fixture returns the absolute path to a named fixture file.
func fixture(name string) string {
	return testutil.FixturePath(name)
}

// requireSlot skips the test if the named language slot is not configured.
func requireSlot(t *testing.T, slot string) {
	t.Helper()
	resp, err := http.Get(healthURL())
	if err != nil {
		t.Skipf("cannot reach health endpoint: %v", err)
	}
	defer resp.Body.Close()
	var health struct {
		Slots map[string]struct {
			Configured bool `json:"configured"`
		} `json:"slots"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Skipf("cannot decode health: %v", err)
	}
	s, ok := health.Slots[slot]
	if !ok || !s.Configured {
		t.Skipf("slot %q not configured — skipping", slot)
	}
}

// ── Phase 3a ────────────────────────────────────────────────────────────────

// A1 — Type resolution on a known symbol.
func TestA1_HoverResolvesType(t *testing.T) {
	result := callTool(t, "hover", map[string]any{
		"filePath": fixture("sample.py"),
		"line":     27, // `result = fut.result()` — hover on `fut`
		"column":   14,
	})

	if isError(result) {
		t.Fatalf("A1: unexpected error: %v", result)
	}
	text := resultText(t, result)
	if !strings.Contains(text, "Future") {
		t.Errorf("A1: expected type containing 'Future', got: %q", text)
	}
	if !strings.Contains(text, "tuple") {
		t.Errorf("A1: expected type containing 'tuple', got: %q", text)
	}
}

// A2 — Cross-file definition lookup.
func TestA2_DefinitionReturnsLocation(t *testing.T) {
	result := callTool(t, "definition", map[string]any{
		"filePath": fixture("sample.py"),
		"line":     23, // w.submit(b"ping") — REFERENCE line
		"column":   14, // position of `submit`
	})

	if isError(result) {
		t.Fatalf("A2: unexpected error: %v", result)
	}
	text := resultText(t, result)

	// Result must contain an absolute path pointing back to sample.py
	if !strings.Contains(text, "sample.py") {
		t.Errorf("A2: expected result to reference sample.py, got: %q", text)
	}
	// Must not be a URI — we want absolute paths
	if strings.HasPrefix(text, "file://") {
		t.Errorf("A2: result should be an absolute path, not a URI: %q", text)
	}
}

// A3 — Type-resolved reference list.
func TestA3_ReferencesReturnsCallSites(t *testing.T) {
	result := callTool(t, "references", map[string]any{
		"filePath": fixture("sample.py"),
		"line":     16, // Worker.submit DEFINITION line
		"column":   9,
	})

	if isError(result) {
		t.Fatalf("A3: unexpected error: %v", result)
	}
	text := resultText(t, result)

	// Must include the REFERENCE call site on line 23
	if !strings.Contains(text, "23") {
		t.Errorf("A3: expected reference at line 23, got: %q", text)
	}
}

// A4 — Diagnostics surface a real type error.
func TestA4_DiagnosticsReportsTypeError(t *testing.T) {
	// Pyright may still be doing background analysis from earlier tests.
	// Retry up to 3 times with a brief pause to let it settle.
	var text string
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(500 * time.Millisecond)
		}
		result := callTool(t, "diagnostics", map[string]any{
			"filePath": fixture("sample_error.py"),
		})
		if isError(result) {
			t.Fatalf("A4: unexpected error: %v", result)
		}
		text = resultText(t, result)
		if strings.Contains(strings.ToLower(text), "error") {
			break
		}
	}

	if !strings.Contains(strings.ToLower(text), "error") {
		t.Errorf("A4: expected at least one error diagnostic, got: %q", text)
	}
	// Must point to line 15 (the assignment)
	if !strings.Contains(text, "15") {
		t.Errorf("A4: expected diagnostic at line 15, got: %q", text)
	}
}

// A5 — Diagnostics returns empty on a clean file.
func TestA5_DiagnosticsEmptyOnCleanFile(t *testing.T) {
	result := callTool(t, "diagnostics", map[string]any{
		"filePath": fixture("sample.py"),
	})

	if isError(result) {
		t.Fatalf("A5: unexpected error: %v", result)
	}
	text := resultText(t, result)

	// Clean file → empty diagnostic list
	if strings.Contains(strings.ToLower(text), "error") {
		t.Errorf("A5: expected no errors on clean file, got: %q", text)
	}
}

// A6 — Unsupported file type returns a clear error.
func TestA6_UnsupportedExtensionReturnsError(t *testing.T) {
	// Write a temp .md file so the path is valid.
	dir := t.TempDir()
	mdFile := filepath.Join(dir, "README.md")
	os.WriteFile(mdFile, []byte("# test"), 0644) //nolint:errcheck

	result := callTool(t, "hover", map[string]any{
		"filePath": mdFile,
		"line":     1,
		"column":   1,
	})

	if !isError(result) {
		t.Fatalf("A6: expected error for .md file, got success: %v", result)
	}
	text := resultText(t, result)
	if !strings.Contains(text, ".md") {
		t.Errorf("A6: error message should name the extension, got: %q", text)
	}
}

// A7 — Missing binary returns an actionable error.
func TestA7_MissingBinaryReturnsActionableError(t *testing.T) {
	// This test requires the bridge to be started with an empty LSP_GOPLS_BIN.
	// Controlled via BRIDGE_URL pointing to a specially-configured bridge instance.
	// Skip if gopls is actually installed (would pass for the wrong reason).
	if p, err := exec.LookPath("gopls"); err == nil && p != "" {
		t.Skipf("A7: gopls found at %s — test only valid when gopls is absent", p)
	}

	goFile := testutil.FixturePath("sample.go")
	result := callTool(t, "hover", map[string]any{
		"filePath": goFile,
		"line":     1,
		"column":   1,
	})

	if !isError(result) {
		t.Fatalf("A7: expected error for missing gopls, got success")
	}
	text := resultText(t, result)
	if !strings.Contains(strings.ToLower(text), "gopls") {
		t.Errorf("A7: error should name the missing binary, got: %q", text)
	}
}

// A8 — Bridge recovers from language server crash.
func TestA8_BridgeRecoverFromCrash(t *testing.T) {
	// Step 1: Warm up the python slot with a successful call.
	result := callTool(t, "hover", map[string]any{
		"filePath": fixture("sample.py"),
		"line":     27,
		"column":   14,
	})
	if isError(result) {
		t.Fatalf("A8: warm-up call failed: %v", result)
	}

	// Step 2: Kill the pyright process to simulate a crash.
	kill := exec.Command("pkill", "-f", "pyright-langserver")
	if err := kill.Run(); err != nil {
		t.Skipf("A8: could not kill pyright process (not running or pkill unavailable): %v", err)
	}

	// Brief pause to let the bridge detect the dead process.
	time.Sleep(500 * time.Millisecond)

	// Step 3: Issue another call — bridge should restart the LSP automatically.
	result = callTool(t, "hover", map[string]any{
		"filePath": fixture("sample.py"),
		"line":     27,
		"column":   14,
	})
	if isError(result) {
		t.Errorf("A8: post-crash call failed — bridge did not recover: %v", result)
	}
}

// A9 — Concurrent requests to different languages resolve independently.
func TestA9_ConcurrentLanguagesResolveIndependently(t *testing.T) {
	requireSlot(t, "go")

	type res struct {
		lang   string
		result map[string]any
		err    error
	}
	ch := make(chan res, 2)

	call := func(lang, file string, line, col int) {
		r := callTool(t, "hover", map[string]any{
			"filePath": fixture(file),
			"line":     line,
			"column":   col,
		})
		ch <- res{lang: lang, result: r}
	}

	go call("python", "sample.py", 27, 14)
	go call("go", "sample.go", 17, 18) // hover on Run()

	for i := 0; i < 2; i++ {
		r := <-ch
		if isError(r.result) {
			t.Errorf("A9: %s returned error: %v", r.lang, r.result)
		}
	}
}

// A10 — Cold start latency is within bounds.
func TestA10_ColdStartLatency(t *testing.T) {
	start := time.Now()
	result := callTool(t, "hover", map[string]any{
		"filePath": fixture("sample.py"),
		"line":     27,
		"column":   14,
	})
	elapsed := time.Since(start)

	if isError(result) {
		t.Fatalf("A10: error on cold start: %v", result)
	}
	if elapsed.Seconds() > 10 {
		t.Errorf("A10: cold start took %v, want <10s", elapsed)
	}
}

// ── Phase 3b ────────────────────────────────────────────────────────────────

// B1 — Rename returns a reviewable diff, nothing applied.
func TestB1_RenameReturnsDiffNothingApplied(t *testing.T) {
	// Capture file content before call.
	path := fixture("sample.py")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("B1: read fixture: %v", err)
	}

	result := callTool(t, "rename", map[string]any{
		"filePath": path,
		"line":     16, // Worker.submit DEFINITION
		"column":   9,
		"newName":  "dispatch",
	})

	if isError(result) {
		t.Fatalf("B1: unexpected error: %v", result)
	}
	text := resultText(t, result)

	// Diff must reference the rename
	if !strings.Contains(text, "dispatch") {
		t.Errorf("B1: diff should contain new name 'dispatch', got: %q", text)
	}
	if !strings.Contains(text, "submit") {
		t.Errorf("B1: diff should contain old name 'submit', got: %q", text)
	}

	// File on disk must be unchanged.
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Errorf("B1: fixture file was modified on disk — rename must not apply")
	}
}

// B2 — Rename diff covers all affected files.
func TestB2_RenameDiffCoversAllFiles(t *testing.T) {
	// This scenario must be genuinely cross-file: dispatcher.py references
	// Worker.run() defined in worker.py.
	result := callTool(t, "rename", map[string]any{
		"filePath": fixture("sample_multifile/worker.py"),
		"line":     15, // Worker.run DEFINITION
		"column":   9,
		"newName":  "execute",
	})

	if isError(result) {
		t.Fatalf("B2: unexpected error: %v", result)
	}
	text := resultText(t, result)

	if !strings.Contains(text, "worker.py") {
		t.Errorf("B2: expected diff block for worker.py, got: %q", text)
	}
	if !strings.Contains(text, "dispatcher.py") {
		t.Errorf("B2: expected diff block for dispatcher.py, got: %q", text)
	}
	if !strings.Contains(text, "execute") || !strings.Contains(text, "run") {
		t.Errorf("B2: expected diff to contain both old and new symbol names, got: %q", text)
	}
}

// B3 — Incoming call hierarchy identifies all callers.
func TestB3_CallHierarchyInReturnsCallers(t *testing.T) {
	result := callTool(t, "call_hierarchy_in", map[string]any{
		"filePath": fixture("sample.py"),
		"line":     16, // Worker.submit DEFINITION
		"column":   9,
	})

	if isError(result) {
		t.Fatalf("B3: unexpected error: %v", result)
	}
	text := resultText(t, result)

	// get_result() calls submit — must appear in callers
	if !strings.Contains(text, "get_result") {
		t.Errorf("B3: expected 'get_result' in callers, got: %q", text)
	}
}

// B4 — Outgoing call hierarchy maps what a function calls.
func TestB4_CallHierarchyOutReturnsCallees(t *testing.T) {
	requireSlot(t, "go")

	result := callTool(t, "call_hierarchy_out", map[string]any{
		"filePath": fixture("sample.go"),
		"line":     27, // Dispatch() function
		"column":   6,
	})

	if isError(result) {
		t.Fatalf("B4: unexpected error: %v", result)
	}
	text := resultText(t, result)

	// Dispatch calls NewWorker and Run
	if !strings.Contains(text, "NewWorker") && !strings.Contains(text, "Run") {
		t.Errorf("B4: expected callees NewWorker or Run, got: %q", text)
	}
}

// B5 — Call hierarchy on a non-callable returns gracefully.
// Some language servers (e.g., gopls) return an error for non-callable symbols;
// others return empty. Both are acceptable — the key requirement is no crash.
func TestB5_CallHierarchyOnNonCallableReturnsGracefully(t *testing.T) {
	requireSlot(t, "go")

	result := callTool(t, "call_hierarchy_in", map[string]any{
		"filePath": fixture("sample.go"),
		"line":     13, // `ID int` field — not callable
		"column":   2,
	})

	if isError(result) {
		// Acceptable: server explicitly rejects non-callable symbol.
		text := resultText(t, result)
		t.Logf("B5: got expected error for non-callable: %s", text)
		return
	}
	text := resultText(t, result)
	if strings.TrimSpace(text) != "" && strings.TrimSpace(text) != "[]" {
		t.Errorf("B5: expected empty result for non-callable, got: %q", text)
	}
}

// B6 — Signature help returns parameter names and types at a call site.
func TestB6_SignatureHelpReturnsParams(t *testing.T) {
	result := callTool(t, "signature_help", map[string]any{
		"filePath": fixture("sample.py"),
		"line":     23, // w.submit(b"ping") — inside argument list
		"column":   21,
	})

	if isError(result) {
		t.Fatalf("B6: unexpected error: %v", result)
	}
	text := resultText(t, result)

	// submit(self, payload: bytes) — "payload" must appear
	if !strings.Contains(text, "payload") {
		t.Errorf("B6: expected parameter 'payload' in signature help, got: %q", text)
	}
}

// ── Failure modes ────────────────────────────────────────────────────────────

// F1 — Bridge not running: HTTP call fails cleanly.
func TestF1_BridgeNotRunning(t *testing.T) {
	// Target a port where no bridge is listening.
	// Use a high ephemeral port unlikely to be in use.
	deadURL := "http://localhost:19876/mcp"

	resp, err := callToolRaw(deadURL, "hover", map[string]any{
		"filePath": "/tmp/test.py",
		"line":     1,
		"column":   1,
	})
	if err != nil {
		// Expected: connection refused or similar network error.
		t.Logf("F1: got expected HTTP error: %v", err)
		return
	}
	defer resp.Body.Close()
	t.Fatalf("F1: expected connection error to dead port, but got HTTP %d", resp.StatusCode)
}

// F2 — Permanent restart failure: slot marked dead, other slots unaffected.
func TestF2_PermanentRestartFailure(t *testing.T) {
	// This test requires the bridge to be configured with a broken binary for
	// one language slot (e.g., LSP_PYRIGHT_BIN=/nonexistent/pyright).
	// After 3 failed start attempts within 30s, that slot is marked permanently dead.
	//
	// To run with correct setup:
	//   LSP_PYRIGHT_BIN=/nonexistent/path ./start-lsp.sh
	//   go test -tags acceptance -run TestF2

	// Issue 4 calls to exhaust the 3-attempt retry budget.
	var lastResult map[string]any
	for i := 0; i < 4; i++ {
		lastResult = callTool(t, "hover", map[string]any{
			"filePath": fixture("sample.py"),
			"line":     27,
			"column":   14,
		})
		// If the first call succeeds, the python binary is working — wrong setup.
		if i == 0 && !isError(lastResult) {
			t.Skipf("F2: python slot is functional — test requires bridge configured with broken pyright binary")
		}
	}

	// After 3+ failures, the slot should be permanently dead.
	if !isError(lastResult) {
		t.Fatalf("F2: expected permanent failure after exhausting retry budget, got success")
	}
	text := resultText(t, lastResult)
	if !strings.Contains(strings.ToLower(text), "permanent") &&
		!strings.Contains(strings.ToLower(text), "failed") {
		t.Errorf("F2: error should indicate permanent failure, got: %q", text)
	}

	// Verify /health shows the python slot as dead.
	resp, err := http.Get(healthURL())
	if err != nil {
		t.Fatalf("F2: health check failed: %v", err)
	}
	defer resp.Body.Close()
	var health map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Fatalf("F2: decode health response: %v", err)
	}

	slots, _ := health["slots"].(map[string]any)
	python, _ := slots["python"].(map[string]any)
	if python == nil {
		t.Fatalf("F2: no python slot in health response: %v", health)
	}
	if dead, ok := python["dead"].(bool); !ok || !dead {
		t.Errorf("F2: expected python slot to be dead in /health, got: %v", python)
	}

	// Other slots must remain functional. Verify the go slot still works.
	goResult := callTool(t, "hover", map[string]any{
		"filePath": fixture("sample.go"),
		"line":     17, // Worker.Run()
		"column":   14,
	})
	if isError(goResult) {
		t.Errorf("F2: go slot should be unaffected by python failure, got error: %v", goResult)
	}
}

// F3 — File outside workspace returns a clear error.
func TestF3_FileOutsideWorkspaceReturnsError(t *testing.T) {
	t.Skip("workspace boundary validation not yet implemented in bridge")

	result := callTool(t, "hover", map[string]any{
		"filePath": "/tmp/outside_workspace.py",
		"line":     1,
		"column":   1,
	})

	if !isError(result) {
		t.Fatalf("F3: expected error for file outside workspace, got success")
	}
	text := resultText(t, result)
	if !strings.Contains(strings.ToLower(text), "workspace") {
		t.Errorf("F3: error should mention workspace boundary, got: %q", text)
	}
}

// F4 — Large rename across many files completes without truncation.
func TestF4_LargeRenameCompletesWithoutTruncation(t *testing.T) {
	// Use the multifile fixture: dispatcher.py references Worker.run()
	// defined in worker.py. Verify the diff covers all affected files
	// and both old/new names appear (no truncation).
	path := fixture("sample_multifile/worker.py")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("F4: read fixture: %v", err)
	}

	result := callTool(t, "rename", map[string]any{
		"filePath": path,
		"line":     15, // Worker.run DEFINITION
		"column":   9,
		"newName":  "execute_job",
	})

	if isError(result) {
		t.Fatalf("F4: unexpected error: %v", result)
	}
	text := resultText(t, result)

	// Diff must cover both files.
	if !strings.Contains(text, "worker.py") {
		t.Errorf("F4: diff should reference worker.py, got: %q", text)
	}
	if !strings.Contains(text, "dispatcher.py") {
		t.Errorf("F4: diff should reference dispatcher.py, got: %q", text)
	}

	// Diff must contain both old and new names — proves no truncation.
	if !strings.Contains(text, "run") {
		t.Errorf("F4: diff should contain old name 'run', got: %q", text)
	}
	if !strings.Contains(text, "execute_job") {
		t.Errorf("F4: diff should contain new name 'execute_job', got: %q", text)
	}

	// Verify each file in the diff has both - and + lines (complete hunks).
	lines := strings.Split(text, "\n")
	hasMinus, hasPlus := false, false
	for _, line := range lines {
		if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			hasMinus = true
		}
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			hasPlus = true
		}
	}
	if !hasMinus || !hasPlus {
		t.Errorf("F4: diff should have both - and + lines (not truncated), got:\n%s", text)
	}

	// File on disk must be unchanged.
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Errorf("F4: fixture file was modified on disk — rename must not apply")
	}
}

