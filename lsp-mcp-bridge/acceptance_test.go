//go:build acceptance

package main_test

// Acceptance tests — written before implementation.
// All tests carry t.Skip until the build step that satisfies them is complete.
//
// Remove skip schedule (see IMPL-3A.md § Step 0):
//   After Step 4 (manager.go) : A6, A7, F1, F2, F3
//   After Step 5 (tools_3a.go): A1, A2, A3, A4, A5, A8, A9, A10
//   After Step 7 (tools_3b.go): B1, B2, B3, B4, B5, B6, F4
//
// Run: go test -tags acceptance ./...
//
// These tests are black-box — they interact with the bridge via HTTP,
// exactly as Claude Code would. No internal bridge packages are imported.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// callTool issues a single MCP tool call over HTTP and returns the raw result.
func callTool(t *testing.T, tool string, args map[string]any) map[string]any {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      tool,
			"arguments": args,
		},
	})
	resp, err := http.Post(bridgeURL(), "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("callTool %s: HTTP error: %v", tool, err)
	}
	defer resp.Body.Close()
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("callTool %s: decode response: %v", tool, err)
	}
	return result
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

// ── Phase 3a ────────────────────────────────────────────────────────────────

// A1 — Type resolution on a known symbol.
func TestA1_HoverResolvesType(t *testing.T) {
	t.Skip("not yet implemented — remove after Step 5")

	result := callTool(t, "hover", map[string]any{
		"filePath": fixture("sample.py"),
		"line":     27, // `result = fut.result()` — hover on `fut`
		"column":   12,
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
	t.Skip("not yet implemented — remove after Step 5")

	result := callTool(t, "definition", map[string]any{
		"filePath": fixture("sample.py"),
		"line":     23, // w.submit(b"ping") — REFERENCE line
		"column":   7,  // position of `submit`
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
	t.Skip("not yet implemented — remove after Step 5")

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
	t.Skip("not yet implemented — remove after Step 5")

	result := callTool(t, "diagnostics", map[string]any{
		"filePath": fixture("sample_error.py"),
	})

	if isError(result) {
		t.Fatalf("A4: unexpected error: %v", result)
	}
	text := resultText(t, result)

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
	t.Skip("not yet implemented — remove after Step 5")

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
	t.Skip("not yet implemented — remove after Step 4")

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
	t.Skip("not yet implemented — remove after Step 4")
	// This test requires the bridge to be started with an empty LSP_GOPLS_BIN.
	// Controlled via BRIDGE_URL pointing to a specially-configured bridge instance.
	// Skip if gopls is actually installed (would pass for the wrong reason).
	// RealBinPath skips the test if the binary is NOT found; we need the inverse:
	// skip if it IS found.
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
	t.Skip("not yet implemented — remove after Step 5")
	// Crash injection is out-of-band — this test relies on a bridge instance
	// whose pyright process has been killed externally before the call.
	// See test harness documentation for setup.
	t.Log("A8: crash recovery test — requires external process kill setup")
}

// A9 — Concurrent requests to different languages resolve independently.
func TestA9_ConcurrentLanguagesResolveIndependently(t *testing.T) {
	t.Skip("not yet implemented — remove after Step 5")

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

	go call("python", "sample.py", 28, 12)
	go call("go", "sample.go", 17, 14) // hover on Run()

	for i := 0; i < 2; i++ {
		r := <-ch
		if isError(r.result) {
			t.Errorf("A9: %s returned error: %v", r.lang, r.result)
		}
	}
}

// A10 — Cold start latency is within bounds.
func TestA10_ColdStartLatency(t *testing.T) {
	t.Skip("not yet implemented — remove after Step 5")

	start := time.Now()
	result := callTool(t, "hover", map[string]any{
		"filePath": fixture("sample.py"),
		"line":     28,
		"column":   12,
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
	t.Skip("not yet implemented — remove after Step 7")

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
	t.Skip("not yet implemented — remove after Step 7")

	result := callTool(t, "rename", map[string]any{
		"filePath": fixture("sample.py"),
		"line":     16,
		"column":   9,
		"newName":  "dispatch",
	})

	if isError(result) {
		t.Fatalf("B2: unexpected error: %v", result)
	}
	text := resultText(t, result)

	// sample.py has both the DEFINITION (line 16) and the REFERENCE (line 23).
	// The diff must contain at least one hunk covering both.
	if strings.Count(text, "@@") < 1 {
		t.Errorf("B2: expected at least one diff hunk, got: %q", text)
	}
}

// B3 — Incoming call hierarchy identifies all callers.
func TestB3_CallHierarchyInReturnsCallers(t *testing.T) {
	t.Skip("not yet implemented — remove after Step 7")

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
	t.Skip("not yet implemented — remove after Step 7")

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

// B5 — Call hierarchy on a non-callable returns empty, not an error.
func TestB5_CallHierarchyOnNonCallableReturnsEmpty(t *testing.T) {
	t.Skip("not yet implemented — remove after Step 7")

	result := callTool(t, "call_hierarchy_in", map[string]any{
		"filePath": fixture("sample.go"),
		"line":     13, // `ID int` field — not callable
		"column":   2,
	})

	if isError(result) {
		t.Fatalf("B5: non-callable should return empty, not error: %v", result)
	}
	text := resultText(t, result)
	if strings.TrimSpace(text) != "" && strings.TrimSpace(text) != "[]" {
		t.Errorf("B5: expected empty result for non-callable, got: %q", text)
	}
}

// B6 — Signature help returns parameter names and types at a call site.
func TestB6_SignatureHelpReturnsParams(t *testing.T) {
	t.Skip("not yet implemented — remove after Step 7")

	result := callTool(t, "signature_help", map[string]any{
		"filePath": fixture("sample.py"),
		"line":     23,  // w.submit(b"ping") — inside argument list
		"column":   18,
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
	t.Skip("not yet implemented — remove after Step 4")
	// This test intentionally targets a port where no bridge is running.
	// It validates Claude Code's error path, not the bridge itself.
	// Use a separate BRIDGE_URL pointing to a known-dead port.
	t.Log("F1: requires bridge to be stopped before running")
}

// F2 — Permanent restart failure: slot marked dead, other slots unaffected.
func TestF2_PermanentRestartFailure(t *testing.T) {
	t.Skip("not yet implemented — remove after Step 4")
	// Requires bridge configured with a broken binary path for one slot.
	// After 3 crashes, that slot returns a permanent error.
	// Other slots must remain functional.
	t.Log("F2: requires bridge with broken binary for one language slot")
}

// F3 — File outside workspace returns a clear error.
func TestF3_FileOutsideWorkspaceReturnsError(t *testing.T) {
	t.Skip("not yet implemented — remove after Step 4")

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
	t.Skip("not yet implemented — remove after Step 7")
	// Requires a fixture with a symbol referenced in many files.
	// Placeholder: validate that diff is non-empty and files are unchanged.
	t.Log("F4: requires a multi-file fixture — to be added before Step 7")
}

