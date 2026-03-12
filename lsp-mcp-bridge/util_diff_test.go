package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTemp creates a temp file with given content and returns its path and URI.
func writeTemp(t *testing.T, dir, name, content string) (path, uri string) {
	t.Helper()
	path = filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writeTemp: %v", err)
	}
	return path, PathToURI(path)
}

// makeChangesEdit builds a WorkspaceEdit using the `changes` form.
func makeChangesEdit(uri string, edits []any) map[string]any {
	return map[string]any{
		"changes": map[string]any{
			uri: edits,
		},
	}
}

// makeDocumentChangesEdit builds a WorkspaceEdit using the `documentChanges` form.
func makeDocumentChangesEdit(uri string, edits []any) map[string]any {
	return map[string]any{
		"documentChanges": []any{
			map[string]any{
				"textDocument": map[string]any{"uri": uri, "version": 1},
				"edits":        edits,
			},
		},
	}
}

func rangeEdit(startLine, startChar, endLine, endChar int, newText string) map[string]any {
	return map[string]any{
		"range": map[string]any{
			"start": map[string]any{"line": startLine, "character": startChar},
			"end":   map[string]any{"line": endLine, "character": endChar},
		},
		"newText": newText,
	}
}

func TestWorkspaceEditToDiffSingleLine(t *testing.T) {
	dir := t.TempDir()
	content := "package main\n\nfunc foo() {}\n"
	path, uri := writeTemp(t, dir, "a.go", content)

	edit := makeChangesEdit(uri, []any{
		rangeEdit(2, 5, 2, 8, "bar"), // rename foo → bar on line 2 (0-based)
	})

	diff := WorkspaceEditToDiff(edit, dir)

	if diff == "" {
		t.Fatal("expected non-empty diff")
	}
	if !strings.Contains(diff, "@@") {
		t.Errorf("diff missing @@ hunk marker: %q", diff)
	}
	if !strings.Contains(diff, "-func foo()") {
		t.Errorf("diff missing removed line: %q", diff)
	}
	if !strings.Contains(diff, "+func bar()") {
		t.Errorf("diff missing added line: %q", diff)
	}
	// File on disk unchanged.
	got, _ := os.ReadFile(path)
	if string(got) != content {
		t.Error("file was modified on disk")
	}
}

func TestWorkspaceEditToDiffMultiLine(t *testing.T) {
	dir := t.TempDir()
	content := "line0\nline1\nline2\nline3\n"
	path, uri := writeTemp(t, dir, "b.go", content)

	// Replace "line1\nline2" (lines 1–2) with "replaced"
	edit := makeChangesEdit(uri, []any{
		rangeEdit(1, 0, 2, 5, "replaced"),
	})

	diff := WorkspaceEditToDiff(edit, dir)

	if diff == "" {
		t.Fatal("expected non-empty diff")
	}
	if !strings.Contains(diff, "replaced") {
		t.Errorf("diff missing replacement text: %q", diff)
	}
	got, _ := os.ReadFile(path)
	if string(got) != content {
		t.Error("file was modified on disk")
	}
}

func TestWorkspaceEditToDiffMultiFile(t *testing.T) {
	dir := t.TempDir()
	contentA := "func submit() {}\n"
	contentB := "// calls submit\nsubmit()\n"
	_, uriA := writeTemp(t, dir, "worker.go", contentA)
	_, uriB := writeTemp(t, dir, "caller.go", contentB)

	edit := map[string]any{
		"changes": map[string]any{
			uriA: []any{rangeEdit(0, 5, 0, 11, "dispatch")},
			uriB: []any{rangeEdit(1, 0, 1, 6, "dispatch")},
		},
	}

	diff := WorkspaceEditToDiff(edit, dir)

	// Two --- headers expected (one per file).
	if count := strings.Count(diff, "--- a/"); count != 2 {
		t.Errorf("expected 2 file headers, got %d: %q", count, diff)
	}
	if !strings.Contains(diff, "worker.go") {
		t.Errorf("diff missing worker.go: %q", diff)
	}
	if !strings.Contains(diff, "caller.go") {
		t.Errorf("diff missing caller.go: %q", diff)
	}
}

func TestWorkspaceEditToDiffDocumentChanges(t *testing.T) {
	dir := t.TempDir()
	content := "func foo() {}\n"
	_, uri := writeTemp(t, dir, "c.go", content)

	changesEdit := makeChangesEdit(uri, []any{
		rangeEdit(0, 5, 0, 8, "bar"),
	})
	docChangesEdit := makeDocumentChangesEdit(uri, []any{
		rangeEdit(0, 5, 0, 8, "bar"),
	})

	diffA := WorkspaceEditToDiff(changesEdit, dir)
	diffB := WorkspaceEditToDiff(docChangesEdit, dir)

	if diffA == "" || diffB == "" {
		t.Fatal("both forms should produce a non-empty diff")
	}
	// Both forms must produce equivalent content (same hunks).
	if !strings.Contains(diffA, "+func bar()") || !strings.Contains(diffB, "+func bar()") {
		t.Errorf("one form missing expected hunk\nchanges: %q\ndocumentChanges: %q", diffA, diffB)
	}
}

func TestWorkspaceEditToDiffNoDiskWrite(t *testing.T) {
	dir := t.TempDir()
	content := "original content\n"
	path, uri := writeTemp(t, dir, "d.go", content)

	edit := makeChangesEdit(uri, []any{
		rangeEdit(0, 0, 0, 8, "replaced"),
	})
	WorkspaceEditToDiff(edit, dir)

	got, _ := os.ReadFile(path)
	if string(got) != content {
		t.Errorf("file was modified: got %q, want %q", got, content)
	}
}

func TestWorkspaceEditToDiffNewlineInReplacement(t *testing.T) {
	dir := t.TempDir()
	content := "func foo() {}\n"
	_, uri := writeTemp(t, dir, "e.go", content)

	// Replace "foo" with "bar\nbaz" — newText contains \n
	edit := makeChangesEdit(uri, []any{
		rangeEdit(0, 5, 0, 8, "bar\nbaz"),
	})

	diff := WorkspaceEditToDiff(edit, dir)

	if diff == "" {
		t.Fatal("expected non-empty diff")
	}
	if !strings.Contains(diff, "bar") || !strings.Contains(diff, "baz") {
		t.Errorf("diff missing split replacement: %q", diff)
	}
}

func TestWorkspaceEditToDiffNilEdit(t *testing.T) {
	if got := WorkspaceEditToDiff(nil, "/tmp"); got != "" {
		t.Errorf("nil edit should return empty string, got %q", got)
	}
}

func TestWorkspaceEditToDiffEmptyEdit(t *testing.T) {
	edit := map[string]any{}
	if got := WorkspaceEditToDiff(edit, "/tmp"); got != "" {
		t.Errorf("empty edit should return empty string, got %q", got)
	}
}
