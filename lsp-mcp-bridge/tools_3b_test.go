package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/lsp-bootstrap/lsp-mcp-bridge/testutil"
)

// ── test harness ──────────────────────────────────────────────────────────────

// newMockManager starts a MockLSP and returns a Manager wired to it via the
// subprocess mock pattern, plus the mock for assertion.
func newMockManager(t *testing.T) (*Manager, *testutil.MockLSP) {
	t.Helper()
	mock := testutil.NewMockLSP()

	origArgs := slotArgs["python"]
	slotArgs["python"] = []string{"-mock-lsp"}
	t.Cleanup(func() { slotArgs["python"] = origArgs })

	cfg := &Config{
		Workspace:  t.TempDir(),
		PyrightBin: os.Args[0],
	}
	mgr := NewManager(cfg)
	t.Cleanup(func() { mgr.Shutdown(context.Background()) })
	return mgr, mock
}

// callHandler calls a ToolHandlerFunc with the given arguments map.
func callHandler(t *testing.T, h func(*Manager) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error), mgr *Manager, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	req := mcp.CallToolRequest{}
	req.Params.Arguments = args
	result, err := h(mgr)(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned Go error: %v", err)
	}
	return result
}

func resultIsError(r *mcp.CallToolResult) bool {
	return r != nil && r.IsError
}

func resultLabel(r *mcp.CallToolResult) string {
	if r == nil || len(r.Content) == 0 {
		return ""
	}
	if tc, ok := r.Content[0].(mcp.TextContent); ok {
		return tc.Text
	}
	return ""
}

// ── rename ────────────────────────────────────────────────────────────────────

func TestRenameHandlerCallsRenameRequest(t *testing.T) {
	mgr, _ := newMockManager(t)

	// Write a real .py file so GetClient can start and openFile can read.
	pyFile := filepath.Join(mgr.cfg.Workspace, "sample.py")
	if err := os.WriteFile(pyFile, []byte("def foo(): pass\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Configure mock subprocess to respond to rename with a WorkspaceEdit.
	uri := PathToURI(pyFile)
	edit := map[string]any{
		"changes": map[string]any{
			uri: []any{
				map[string]any{
					"range": map[string]any{
						"start": map[string]any{"line": float64(0), "character": float64(4)},
						"end":   map[string]any{"line": float64(0), "character": float64(7)},
					},
					"newText": "bar",
				},
			},
		},
	}
	editJSON, _ := json.Marshal(edit)
	// The mock subprocess doesn't support dynamic handler config, so we test
	// the rename result parsing by calling the rename tool with a mock manager
	// that returns the edit directly. Use the in-process mock instead.

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	mock := testutil.NewMockLSP()
	mock.RespondTo("textDocument/rename", json.RawMessage(editJSON))
	w, r := mock.StartInProcess(ctx)
	client := NewLspClientFromPipes(w, r)
	go client.readLoop()

	if err := client.Initialize(mgr.cfg.Workspace); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	// Manually exercise the rename request + diff generation path.
	params := map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     MakePosition(1, 5),
		"newName":      "bar",
	}
	raw, err := client.Request("textDocument/rename", params)
	if err != nil {
		t.Fatalf("rename request: %v", err)
	}

	var editResult map[string]any
	if err := json.Unmarshal(raw, &editResult); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	diff := WorkspaceEditToDiff(editResult, mgr.cfg.Workspace)
	if !strings.Contains(diff, "bar") {
		t.Errorf("expected diff to contain 'bar', got: %q", diff)
	}
	if !strings.Contains(diff, "foo") {
		t.Errorf("expected diff to contain 'foo', got: %q", diff)
	}
}

func TestRenameHandlerNullResult(t *testing.T) {
	// null WorkspaceEdit → empty string, not error
	diff := WorkspaceEditToDiff(nil, "/tmp")
	if diff != "" {
		t.Errorf("nil edit: expected empty string, got %q", diff)
	}

	var m map[string]any
	json.Unmarshal([]byte("null"), &m) //nolint:errcheck
	diff = WorkspaceEditToDiff(m, "/tmp")
	if diff != "" {
		t.Errorf("null-unmarshalled edit: expected empty string, got %q", diff)
	}
}

func TestRenameHandlerNoDiskWrite(t *testing.T) {
	dir := t.TempDir()
	content := "def foo(): pass\n"
	path := filepath.Join(dir, "f.py")
	os.WriteFile(path, []byte(content), 0644) //nolint:errcheck
	uri := PathToURI(path)

	edit := map[string]any{
		"changes": map[string]any{
			uri: []any{
				map[string]any{
					"range": map[string]any{
						"start": map[string]any{"line": float64(0), "character": float64(4)},
						"end":   map[string]any{"line": float64(0), "character": float64(7)},
					},
					"newText": "bar",
				},
			},
		},
	}
	WorkspaceEditToDiff(edit, dir)

	got, _ := os.ReadFile(path)
	if string(got) != content {
		t.Errorf("file was modified: got %q want %q", got, content)
	}
}

// ── call hierarchy ────────────────────────────────────────────────────────────

func TestCallHierarchyInTwoStepProtocol(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	prepareResult := json.RawMessage(`[{"name":"foo","uri":"file:///f.py","range":{"start":{"line":0,"character":4},"end":{"line":0,"character":7}},"selectionRange":{"start":{"line":0,"character":4},"end":{"line":0,"character":7}},"kind":12}]`)
	incomingResult := json.RawMessage(`[{"from":{"name":"bar","uri":"file:///caller.py","range":{"start":{"line":5,"character":0},"end":{"line":5,"character":3}}},"fromRanges":[]}]`)

	mock := testutil.NewMockLSP()
	mock.RespondTo("textDocument/prepareCallHierarchy", prepareResult)
	mock.RespondTo("callHierarchy/incomingCalls", incomingResult)

	w, r := mock.StartInProcess(ctx)
	client := NewLspClientFromPipes(w, r)
	go client.readLoop()

	if err := client.Initialize("/tmp/ws"); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	item, ok, err := prepareCallHierarchy(client, "file:///f.py", 1, 5)
	if err != nil {
		t.Fatalf("prepareCallHierarchy: %v", err)
	}
	if !ok {
		t.Fatal("expected item to be returned")
	}

	raw, err := client.Request("callHierarchy/incomingCalls", map[string]any{"item": item})
	if err != nil {
		t.Fatalf("incomingCalls: %v", err)
	}

	sites := parseIncomingCalls(raw)
	if len(sites) != 1 {
		t.Fatalf("expected 1 call site, got %d", len(sites))
	}
	if sites[0].Name != "bar" {
		t.Errorf("Name = %q, want bar", sites[0].Name)
	}
	if sites[0].Line != 6 { // 0-based LSP → 1-based
		t.Errorf("Line = %d, want 6", sites[0].Line)
	}

	// Verify the two-step order.
	received := mock.Received()
	var prepare, incoming int
	for i, m := range received {
		if m == "textDocument/prepareCallHierarchy" {
			prepare = i
		}
		if m == "callHierarchy/incomingCalls" {
			incoming = i
		}
	}
	if prepare >= incoming {
		t.Errorf("prepareCallHierarchy (%d) must come before incomingCalls (%d)", prepare, incoming)
	}
}

func TestCallHierarchyInNonCallableReturnsEmpty(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	mock := testutil.NewMockLSP()
	mock.RespondTo("textDocument/prepareCallHierarchy", json.RawMessage(`null`))

	w, r := mock.StartInProcess(ctx)
	client := NewLspClientFromPipes(w, r)
	go client.readLoop()

	if err := client.Initialize("/tmp/ws"); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	_, ok, err := prepareCallHierarchy(client, "file:///f.py", 1, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected ok=false for null prepare result")
	}
}

func TestCallHierarchyOutTwoStepProtocol(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	prepareResult := json.RawMessage(`[{"name":"foo","uri":"file:///f.py","range":{"start":{"line":0,"character":4},"end":{"line":0,"character":7}},"selectionRange":{"start":{"line":0,"character":4},"end":{"line":0,"character":7}},"kind":12}]`)
	outgoingResult := json.RawMessage(`[{"to":{"name":"helper","uri":"file:///helper.py","range":{"start":{"line":10,"character":0},"end":{"line":10,"character":6}}},"fromRanges":[]}]`)

	mock := testutil.NewMockLSP()
	mock.RespondTo("textDocument/prepareCallHierarchy", prepareResult)
	mock.RespondTo("callHierarchy/outgoingCalls", outgoingResult)

	w, r := mock.StartInProcess(ctx)
	client := NewLspClientFromPipes(w, r)
	go client.readLoop()

	if err := client.Initialize("/tmp/ws"); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	item, ok, err := prepareCallHierarchy(client, "file:///f.py", 1, 5)
	if err != nil || !ok {
		t.Fatalf("prepare: err=%v ok=%v", err, ok)
	}

	raw, err := client.Request("callHierarchy/outgoingCalls", map[string]any{"item": item})
	if err != nil {
		t.Fatalf("outgoingCalls: %v", err)
	}

	sites := parseOutgoingCalls(raw)
	if len(sites) != 1 {
		t.Fatalf("expected 1 call site, got %d", len(sites))
	}
	if sites[0].Name != "helper" {
		t.Errorf("Name = %q, want helper", sites[0].Name)
	}
	if sites[0].Line != 11 { // 0-based → 1-based
		t.Errorf("Line = %d, want 11", sites[0].Line)
	}

	received := mock.Received()
	var prepare, outgoing int
	for i, m := range received {
		if m == "textDocument/prepareCallHierarchy" {
			prepare = i
		}
		if m == "callHierarchy/outgoingCalls" {
			outgoing = i
		}
	}
	if prepare >= outgoing {
		t.Errorf("prepareCallHierarchy (%d) must come before outgoingCalls (%d)", prepare, outgoing)
	}
}

// ── signature help ────────────────────────────────────────────────────────────

func TestSignatureHelpReturnsLabel(t *testing.T) {
	raw := json.RawMessage(`{
		"signatures": [{"label": "submit(self, payload: bytes) -> Future[tuple[bytes, int]]"}],
		"activeSignature": 0
	}`)
	got := parseSignatureHelp(raw)
	if got != "submit(self, payload: bytes) -> Future[tuple[bytes, int]]" {
		t.Errorf("got %q", got)
	}
}

func TestSignatureHelpNullActiveSignature(t *testing.T) {
	// activeSignature omitted — should default to index 0
	raw := json.RawMessage(`{
		"signatures": [{"label": "foo(x: int)"}]
	}`)
	got := parseSignatureHelp(raw)
	if got != "foo(x: int)" {
		t.Errorf("got %q", got)
	}
}

func TestSignatureHelpEmptyReturnsEmpty(t *testing.T) {
	cases := []json.RawMessage{
		json.RawMessage(`null`),
		json.RawMessage(`{"signatures":[]}`),
		json.RawMessage(`{}`),
	}
	for _, raw := range cases {
		if got := parseSignatureHelp(raw); got != "" {
			t.Errorf("parseSignatureHelp(%s) = %q, want empty", raw, got)
		}
	}
}
