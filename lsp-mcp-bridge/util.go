package main

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sergi/go-diff/diffmatchpatch"
)

// PathToURI converts an absolute filesystem path to a file:// URI.
// Spaces and other special characters are percent-encoded.
func PathToURI(path string) string {
	u := &url.URL{
		Scheme: "file",
		Path:   path,
	}
	return u.String()
}

// URIToPath converts a file:// URI back to an absolute filesystem path.
// Non-file URIs are returned as-is.
func URIToPath(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return uri
	}
	return filepath.FromSlash(u.Path)
}

// MakePosition converts 1-based (line, col) to an LSP Position object.
// LSP uses 0-based line and character indices.
func MakePosition(line, col int) map[string]any {
	return map[string]any{
		"line":      line - 1,
		"character": col - 1,
	}
}

// langIDs maps file extensions (including dot) to LSP languageId values.
var langIDs = map[string]string{
	".py":    "python",
	".js":    "javascript",  ".mjs":  "javascript",
	".jsx":   "javascriptreact",
	".ts":    "typescript",
	".tsx":   "typescriptreact",
	".rs":    "rust",
	".go":    "go",
	".kt":    "kotlin",      ".kts":  "kotlin",
	".scala": "scala",       ".sc":   "scala",
	".sh":    "shellscript", ".bash": "shellscript", ".zsh": "shellscript",
	".yml":   "yaml",        ".yaml": "yaml",
	".toml":  "toml",
	".json":  "json",        ".jsonc": "json",
	".html":  "html",        ".htm":  "html",
	".css":   "css",
	".scss":  "scss",
	".less":  "less",
}

// LanguageID returns the LSP languageId for the given file extension (including dot).
// Returns ("", false) for unknown extensions.
func LanguageID(ext string) (string, bool) {
	id, ok := langIDs[strings.ToLower(ext)]
	return id, ok
}

// WorkspaceEditToDiff converts an LSP WorkspaceEdit to unified diff text.
// Handles both documentChanges and changes forms. Never writes to disk.
// Returns "" if the edit is nil or produces no changes.
func WorkspaceEditToDiff(edit map[string]any, workspace string) string {
	if edit == nil {
		return ""
	}

	// Collect edits per URI. documentChanges takes precedence (preferred by modern servers).
	fileEdits := make(map[string][]parsedTextEdit)

	if dc, ok := edit["documentChanges"].([]any); ok {
		for _, item := range dc {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			td, _ := m["textDocument"].(map[string]any)
			uri, _ := td["uri"].(string)
			raw, _ := m["edits"].([]any)
			fileEdits[uri] = append(fileEdits[uri], parseTextEdits(raw)...)
		}
	} else if changes, ok := edit["changes"].(map[string]any); ok {
		for uri, raw := range changes {
			edits, _ := raw.([]any)
			fileEdits[uri] = append(fileEdits[uri], parseTextEdits(edits)...)
		}
	}

	if len(fileEdits) == 0 {
		return ""
	}

	var out strings.Builder

	// Sort URIs for deterministic output.
	uris := make([]string, 0, len(fileEdits))
	for uri := range fileEdits {
		uris = append(uris, uri)
	}
	sort.Strings(uris)

	for _, uri := range uris {
		edits := fileEdits[uri]
		if len(edits) == 0 {
			continue
		}

		path := URIToPath(uri)
		originalBytes, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(&out, "# error reading %s: %v\n", path, err)
			continue
		}
		original := string(originalBytes)

		// Work on a line buffer (each element includes its trailing \n).
		lines := splitIntoLines(original)

		// Apply edits in reverse order to preserve offsets.
		sort.Slice(edits, func(i, j int) bool {
			if edits[i].startLine != edits[j].startLine {
				return edits[i].startLine > edits[j].startLine
			}
			return edits[i].startChar > edits[j].startChar
		})

		for _, e := range edits {
			lines = applyTextEdit(lines, e.startLine, e.startChar, e.endLine, e.endChar, e.newText)
		}


		modified := strings.Join(lines, "")
		if modified == original {
			continue
		}

		// Produce diff via go-diff three-step (no DiffLines function exists).
		dmp := diffmatchpatch.New()
		a, b, lineArr := dmp.DiffLinesToChars(original, modified)
		diffs := dmp.DiffMain(a, b, false)
		diffs = dmp.DiffCharsToLines(diffs, lineArr)
		patches := dmp.PatchMake(original, diffs)
		if len(patches) == 0 {
			continue
		}
		patchText := dmp.PatchToText(patches)

		// Git-style file header.
		rel, err := filepath.Rel(workspace, path)
		if err != nil || strings.HasPrefix(rel, "..") {
			rel = path // outside workspace — use absolute path
		}
		fmt.Fprintf(&out, "--- a/%s\n+++ b/%s\n%s", rel, rel, patchText)
	}

	return out.String()
}

// ── WorkspaceEditToDiff helpers ───────────────────────────────────────────────

type parsedTextEdit struct {
	startLine, startChar int
	endLine, endChar     int
	newText              string
}

func parseTextEdits(raw []any) []parsedTextEdit {
	out := make([]parsedTextEdit, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		r, _ := m["range"].(map[string]any)
		start, _ := r["start"].(map[string]any)
		end, _ := r["end"].(map[string]any)
		newText, _ := m["newText"].(string)
		out = append(out, parsedTextEdit{
			startLine: anyInt(start["line"]),
			startChar: anyInt(start["character"]),
			endLine:   anyInt(end["line"]),
			endChar:   anyInt(end["character"]),
			newText:   newText,
		})
	}
	return out
}

func anyInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	}
	return 0
}

// splitIntoLines splits s into a slice where each element includes its trailing \n.
// A final line without \n is kept as-is.
func splitIntoLines(s string) []string {
	var lines []string
	for len(s) > 0 {
		i := strings.Index(s, "\n")
		if i < 0 {
			lines = append(lines, s)
			break
		}
		lines = append(lines, s[:i+1])
		s = s[i+1:]
	}
	return lines
}

// applyTextEdit replaces [startLine:startChar, endLine:endChar) with newText.
// Lines include trailing \n. Returns the updated line buffer.
func applyTextEdit(lines []string, startLine, startChar, endLine, endChar int, newText string) []string {
	if startLine >= len(lines) {
		return lines
	}

	startStr := lines[startLine]
	prefix := safeByteSlice(startStr, 0, startChar)

	endStr := ""
	if endLine < len(lines) {
		endStr = lines[endLine]
	}
	suffix := safeByteSlice(endStr, endChar, len(endStr))

	combined := prefix + newText + suffix

	// Re-split combined in case newText contains \n.
	var newLines []string
	if combined != "" {
		newLines = splitIntoLines(combined)
	}

	endIdx := endLine + 1
	if endIdx > len(lines) {
		endIdx = len(lines)
	}

	result := make([]string, 0, len(lines)-(endIdx-startLine)+len(newLines))
	result = append(result, lines[:startLine]...)
	result = append(result, newLines...)
	result = append(result, lines[endIdx:]...)
	return result
}

// safeByteSlice slices s[start:end] clamping to valid bounds.
// NOTE: uses byte indices. LSP character offsets are UTF-16 code units by default;
// this is accurate for ASCII content. For non-ASCII, callers must convert offsets.
func safeByteSlice(s string, start, end int) string {
	if start > len(s) {
		start = len(s)
	}
	if end > len(s) {
		end = len(s)
	}
	if start > end {
		start = end
	}
	return s[start:end]
}
