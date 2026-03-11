package main

import (
	"net/url"
	"path/filepath"
	"strings"
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
// Stub — completed in Step 6 (Phase 3b).
func WorkspaceEditToDiff(edit map[string]any, workspace string) string {
	return ""
}
