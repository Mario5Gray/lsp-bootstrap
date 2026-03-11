package main

import (
	"strings"
	"testing"
)

func TestPathToURISpaces(t *testing.T) {
	uri := PathToURI("/home/user/my project/main.go")
	if !strings.HasPrefix(uri, "file://") {
		t.Errorf("expected file:// prefix, got %q", uri)
	}
	if !strings.Contains(uri, "%20") {
		t.Errorf("expected spaces percent-encoded as %%20, got %q", uri)
	}
	if strings.Contains(uri, " ") {
		t.Errorf("URI must not contain raw spaces: %q", uri)
	}
}

func TestURIToPathRoundtrip(t *testing.T) {
	cases := []string{
		"/home/user/main.go",
		"/Users/alice/my project/file.py",
		"/tmp/lsp-test/sample.go",
	}
	for _, p := range cases {
		uri := PathToURI(p)
		got := URIToPath(uri)
		if got != p {
			t.Errorf("URIToPath(PathToURI(%q)) = %q, want original path", p, got)
		}
	}
}

func TestMakePosition(t *testing.T) {
	pos := MakePosition(1, 1)
	if pos["line"] != 0 {
		t.Errorf("line = %v, want 0", pos["line"])
	}
	if pos["character"] != 0 {
		t.Errorf("character = %v, want 0", pos["character"])
	}

	pos2 := MakePosition(10, 5)
	if pos2["line"] != 9 {
		t.Errorf("line = %v, want 9", pos2["line"])
	}
	if pos2["character"] != 4 {
		t.Errorf("character = %v, want 4", pos2["character"])
	}
}

func TestLanguageIDEdgeCases(t *testing.T) {
	cases := map[string]string{
		".jsx":  "javascriptreact",
		".tsx":  "typescriptreact",
		".scss": "scss",
		".sc":   "scala",
		".mjs":  "javascript",
		".kts":  "kotlin",
		".bash": "shellscript",
		".htm":  "html",
		".jsonc": "json",
	}
	for ext, want := range cases {
		got, ok := LanguageID(ext)
		if !ok {
			t.Errorf("LanguageID(%q): not found", ext)
			continue
		}
		if got != want {
			t.Errorf("LanguageID(%q) = %q, want %q", ext, got, want)
		}
	}
}

func TestLanguageIDUnknownExtension(t *testing.T) {
	_, ok := LanguageID(".md")
	if ok {
		t.Error("LanguageID(.md) should return false for unsupported extension")
	}
}
