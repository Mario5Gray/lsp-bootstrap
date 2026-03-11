package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// ── output types ─────────────────────────────────────────────────────────────

// Location is a resolved source position (1-based lines and columns).
type Location struct {
	FilePath string `json:"filePath"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
}

// Diagnostic is a single LSP diagnostic normalised for MCP output.
type Diagnostic struct {
	Severity string `json:"severity"` // "error" | "warning" | "info" | "hint"
	Message  string `json:"message"`
	Line     int    `json:"line"`   // 1-based
	Column   int    `json:"column"` // 1-based
}

// ── handlers ─────────────────────────────────────────────────────────────────

func hoverHandler(mgr *Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filePath, err := req.RequireString("filePath")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		line, err := req.RequireInt("line")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		col, err := req.RequireInt("column")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		client, err := mgr.GetClient(filePath)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		uri, content, langID, err := openFile(filePath)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		if err := client.Notify("textDocument/didOpen", didOpenParams(uri, langID, content)); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("didOpen: %v", err)), nil
		}
		defer client.Notify("textDocument/didClose", didCloseParams(uri)) //nolint:errcheck

		params := map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     MakePosition(line, col),
		}
		raw, err := client.Request("textDocument/hover", params)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("hover: %v", err)), nil
		}

		text := parseHoverResult(raw)
		return mcp.NewToolResultText(text), nil
	}
}

func definitionHandler(mgr *Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filePath, err := req.RequireString("filePath")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		line, err := req.RequireInt("line")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		col, err := req.RequireInt("column")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		client, err := mgr.GetClient(filePath)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		uri, content, langID, err := openFile(filePath)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		if err := client.Notify("textDocument/didOpen", didOpenParams(uri, langID, content)); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("didOpen: %v", err)), nil
		}
		defer client.Notify("textDocument/didClose", didCloseParams(uri)) //nolint:errcheck

		params := map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     MakePosition(line, col),
		}
		raw, err := client.Request("textDocument/definition", params)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("definition: %v", err)), nil
		}

		locs, err := parseLocations(raw)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("parse definition: %v", err)), nil
		}
		out, _ := json.Marshal(locs)
		return mcp.NewToolResultText(string(out)), nil
	}
}

func referencesHandler(mgr *Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filePath, err := req.RequireString("filePath")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		line, err := req.RequireInt("line")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		col, err := req.RequireInt("column")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		client, err := mgr.GetClient(filePath)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		uri, content, langID, err := openFile(filePath)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		if err := client.Notify("textDocument/didOpen", didOpenParams(uri, langID, content)); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("didOpen: %v", err)), nil
		}
		defer client.Notify("textDocument/didClose", didCloseParams(uri)) //nolint:errcheck

		params := map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     MakePosition(line, col),
			"context":      map[string]any{"includeDeclaration": true},
		}
		raw, err := client.Request("textDocument/references", params)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("references: %v", err)), nil
		}

		locs, err := parseLocations(raw)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("parse references: %v", err)), nil
		}
		out, _ := json.Marshal(locs)
		return mcp.NewToolResultText(string(out)), nil
	}
}

func diagnosticsHandler(mgr *Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filePath, err := req.RequireString("filePath")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		client, err := mgr.GetClient(filePath)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		uri, content, langID, err := openFile(filePath)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		// Register BEFORE didOpen to avoid missing the first notification.
		key := "textDocument/publishDiagnostics:" + uri
		ch := client.RegisterNotification(key)

		if err := client.Notify("textDocument/didOpen", didOpenParams(uri, langID, content)); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("didOpen: %v", err)), nil
		}
		defer client.Notify("textDocument/didClose", didCloseParams(uri)) //nolint:errcheck

		diags := collectDiagnostics(ch)
		out, _ := json.Marshal(diags)
		return mcp.NewToolResultText(string(out)), nil
	}
}

// ── diagnostics collection ────────────────────────────────────────────────────

const (
	diagIdleTimeout = 200 * time.Millisecond
	diagHardCap     = 10 * time.Second
)

// collectDiagnostics merges publishDiagnostics notifications using an
// idle timer (200ms) with a hard cap (10s).
func collectDiagnostics(ch chan json.RawMessage) []Diagnostic {
	var merged []Diagnostic
	deadline := time.Now().Add(diagHardCap)
	idle := time.NewTimer(diagIdleTimeout)
	defer idle.Stop()

	for {
		select {
		case raw, ok := <-ch:
			if !ok {
				return merged
			}
			// Reset idle timer on each new notification.
			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			idle.Reset(diagIdleTimeout)

			var payload struct {
				Diagnostics []lspDiagnostic `json:"diagnostics"`
			}
			if err := json.Unmarshal(raw, &payload); err == nil {
				for _, d := range payload.Diagnostics {
					merged = append(merged, normalizeDiag(d))
				}
			}
			if time.Now().After(deadline) {
				return merged
			}
		case <-idle.C:
			return merged
		case <-time.After(time.Until(deadline)):
			return merged
		}
	}
}

type lspDiagnostic struct {
	Severity int    `json:"severity"`
	Message  string `json:"message"`
	Range    struct {
		Start struct {
			Line      int `json:"line"`
			Character int `json:"character"`
		} `json:"start"`
	} `json:"range"`
}

func normalizeDiag(d lspDiagnostic) Diagnostic {
	sev := map[int]string{1: "error", 2: "warning", 3: "info", 4: "hint"}
	severity, ok := sev[d.Severity]
	if !ok {
		severity = "info"
	}
	return Diagnostic{
		Severity: severity,
		Message:  d.Message,
		Line:     d.Range.Start.Line + 1,
		Column:   d.Range.Start.Character + 1,
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func openFile(filePath string) (uri, content, langID string, err error) {
	raw, err := os.ReadFile(filePath)
	if err != nil {
		return "", "", "", fmt.Errorf("read %s: %w", filePath, err)
	}
	ext := strings.ToLower(filepath.Ext(filePath))
	id, ok := LanguageID(ext)
	if !ok {
		id = "plaintext"
	}
	return PathToURI(filePath), string(raw), id, nil
}

func didOpenParams(uri, langID, content string) map[string]any {
	return map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": langID,
			"version":    1,
			"text":       content,
		},
	}
}

func didCloseParams(uri string) map[string]any {
	return map[string]any{
		"textDocument": map[string]any{"uri": uri},
	}
}

func parseHoverResult(raw json.RawMessage) string {
	if raw == nil || string(raw) == "null" {
		return ""
	}
	var result struct {
		Contents any `json:"contents"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return string(raw)
	}
	switch v := result.Contents.(type) {
	case string:
		return v
	case map[string]any:
		if val, ok := v["value"].(string); ok {
			return val
		}
	case []any:
		var parts []string
		for _, item := range v {
			switch s := item.(type) {
			case string:
				parts = append(parts, s)
			case map[string]any:
				if val, ok := s["value"].(string); ok {
					parts = append(parts, val)
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return string(raw)
}

// parseLocations converts an LSP Location or []Location result to []Location.
func parseLocations(raw json.RawMessage) ([]Location, error) {
	if raw == nil || string(raw) == "null" {
		return []Location{}, nil
	}

	// Try array first.
	var lspLocs []struct {
		URI   string `json:"uri"`
		Range struct {
			Start struct {
				Line      int `json:"line"`
				Character int `json:"character"`
			} `json:"start"`
		} `json:"range"`
	}
	if err := json.Unmarshal(raw, &lspLocs); err == nil {
		out := make([]Location, 0, len(lspLocs))
		for _, l := range lspLocs {
			out = append(out, Location{
				FilePath: URIToPath(l.URI),
				Line:     l.Range.Start.Line + 1,
				Column:   l.Range.Start.Character + 1,
			})
		}
		return out, nil
	}

	// Try single location.
	var single struct {
		URI   string `json:"uri"`
		Range struct {
			Start struct {
				Line      int `json:"line"`
				Character int `json:"character"`
			} `json:"start"`
		} `json:"range"`
	}
	if err := json.Unmarshal(raw, &single); err != nil {
		return nil, fmt.Errorf("unrecognised location shape: %w", err)
	}
	return []Location{{
		FilePath: URIToPath(single.URI),
		Line:     single.Range.Start.Line + 1,
		Column:   single.Range.Start.Character + 1,
	}}, nil
}
