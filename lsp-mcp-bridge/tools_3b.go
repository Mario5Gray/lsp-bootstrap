package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// CallSite is one entry in a call hierarchy result.
type CallSite struct {
	Name     string `json:"name"`
	FilePath string `json:"filePath"`
	Line     int    `json:"line"`   // 1-based
	Column   int    `json:"column"` // 1-based
}

// ── handlers ─────────────────────────────────────────────────────────────────

func renameHandler(mgr *Manager) server.ToolHandlerFunc {
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
		newName, err := req.RequireString("newName")
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
			"newName":      newName,
		}
		raw, err := client.Request("textDocument/rename", params)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("rename: %v", err)), nil
		}

		// null result means the symbol is not renameable at this position.
		if raw == nil || string(raw) == "null" {
			return mcp.NewToolResultText(""), nil
		}

		var edit map[string]any
		if err := json.Unmarshal(raw, &edit); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("parse WorkspaceEdit: %v", err)), nil
		}

		diff := WorkspaceEditToDiff(edit, mgr.cfg.Workspace)
		return mcp.NewToolResultText(diff), nil
	}
}

func callHierarchyInHandler(mgr *Manager) server.ToolHandlerFunc {
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

		item, ok, err := prepareCallHierarchy(client, uri, line, col)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if !ok {
			out, _ := json.Marshal([]CallSite{})
			return mcp.NewToolResultText(string(out)), nil
		}

		raw, err := client.Request("callHierarchy/incomingCalls", map[string]any{"item": item})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("incomingCalls: %v", err)), nil
		}

		sites := parseIncomingCalls(raw)
		out, _ := json.Marshal(sites)
		return mcp.NewToolResultText(string(out)), nil
	}
}

func callHierarchyOutHandler(mgr *Manager) server.ToolHandlerFunc {
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

		item, ok, err := prepareCallHierarchy(client, uri, line, col)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if !ok {
			out, _ := json.Marshal([]CallSite{})
			return mcp.NewToolResultText(string(out)), nil
		}

		raw, err := client.Request("callHierarchy/outgoingCalls", map[string]any{"item": item})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("outgoingCalls: %v", err)), nil
		}

		sites := parseOutgoingCalls(raw)
		out, _ := json.Marshal(sites)
		return mcp.NewToolResultText(string(out)), nil
	}
}

func signatureHelpHandler(mgr *Manager) server.ToolHandlerFunc {
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
		raw, err := client.Request("textDocument/signatureHelp", params)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("signatureHelp: %v", err)), nil
		}

		return mcp.NewToolResultText(parseSignatureHelp(raw)), nil
	}
}

// ── protocol helpers ──────────────────────────────────────────────────────────

// prepareCallHierarchy runs textDocument/prepareCallHierarchy and returns the
// first CallHierarchyItem. Returns (nil, false, nil) when the position is not callable.
func prepareCallHierarchy(client *LspClient, uri string, line, col int) (json.RawMessage, bool, error) {
	params := map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     MakePosition(line, col),
	}
	raw, err := client.Request("textDocument/prepareCallHierarchy", params)
	if err != nil {
		return nil, false, fmt.Errorf("prepareCallHierarchy: %w", err)
	}
	if raw == nil || string(raw) == "null" {
		return nil, false, nil
	}

	// Result may be a single item or an array. Try array first.
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err == nil {
		if len(items) == 0 {
			return nil, false, nil
		}
		return items[0], true, nil
	}

	// Try single object.
	var item json.RawMessage
	if err := json.Unmarshal(raw, &item); err != nil {
		return nil, false, nil
	}
	return item, true, nil
}

func parseIncomingCalls(raw json.RawMessage) []CallSite {
	if raw == nil || string(raw) == "null" {
		return []CallSite{}
	}
	var calls []struct {
		From struct {
			Name  string `json:"name"`
			URI   string `json:"uri"`
			Range struct {
				Start struct {
					Line      int `json:"line"`
					Character int `json:"character"`
				} `json:"start"`
			} `json:"range"`
		} `json:"from"`
	}
	if err := json.Unmarshal(raw, &calls); err != nil {
		return []CallSite{}
	}
	out := make([]CallSite, 0, len(calls))
	for _, c := range calls {
		out = append(out, CallSite{
			Name:     c.From.Name,
			FilePath: URIToPath(c.From.URI),
			Line:     c.From.Range.Start.Line + 1,
			Column:   c.From.Range.Start.Character + 1,
		})
	}
	return out
}

func parseOutgoingCalls(raw json.RawMessage) []CallSite {
	if raw == nil || string(raw) == "null" {
		return []CallSite{}
	}
	var calls []struct {
		To struct {
			Name  string `json:"name"`
			URI   string `json:"uri"`
			Range struct {
				Start struct {
					Line      int `json:"line"`
					Character int `json:"character"`
				} `json:"start"`
			} `json:"range"`
		} `json:"to"`
	}
	if err := json.Unmarshal(raw, &calls); err != nil {
		return []CallSite{}
	}
	out := make([]CallSite, 0, len(calls))
	for _, c := range calls {
		out = append(out, CallSite{
			Name:     c.To.Name,
			FilePath: URIToPath(c.To.URI),
			Line:     c.To.Range.Start.Line + 1,
			Column:   c.To.Range.Start.Character + 1,
		})
	}
	return out
}

func parseSignatureHelp(raw json.RawMessage) string {
	if raw == nil || string(raw) == "null" {
		return ""
	}
	var result struct {
		Signatures      []struct{ Label string `json:"label"` } `json:"signatures"`
		ActiveSignature *int                                     `json:"activeSignature"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return ""
	}
	if len(result.Signatures) == 0 {
		return ""
	}
	idx := 0
	if result.ActiveSignature != nil {
		idx = *result.ActiveSignature
	}
	if idx >= len(result.Signatures) {
		idx = 0
	}
	return result.Signatures[idx].Label
}
