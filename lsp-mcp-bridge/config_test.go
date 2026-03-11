package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lsp-bootstrap/lsp-mcp-bridge/testutil"
)

func TestConfigParsesEnvLsp(t *testing.T) {
	envLsp := testutil.FixturePath("env.lsp")
	c, err := Load(envLsp, "", nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Port != "7890" {
		t.Errorf("Port = %q, want 7890", c.Port)
	}
	if c.Workspace != "/tmp/lsp-test-workspace" {
		t.Errorf("Workspace = %q, want /tmp/lsp-test-workspace", c.Workspace)
	}
	if c.PyrightBin != "/usr/local/bin/pyright-langserver" {
		t.Errorf("PyrightBin = %q, want /usr/local/bin/pyright-langserver", c.PyrightBin)
	}
	if c.LogPath != "/tmp/lsp-bridge-test.log" {
		t.Errorf("LogPath = %q, want /tmp/lsp-bridge-test.log", c.LogPath)
	}
}

func TestConfigEnvCustomOverrides(t *testing.T) {
	envLsp := testutil.FixturePath("env.lsp")

	// Write a custom override file that changes only Port and PyrightBin.
	dir := t.TempDir()
	customPath := filepath.Join(dir, "env.custom")
	if err := os.WriteFile(customPath, []byte(
		"LSP_PORT=9999\nLSP_PYRIGHT_BIN=/custom/pyright\n",
	), 0644); err != nil {
		t.Fatalf("write custom: %v", err)
	}

	c, err := Load(envLsp, customPath, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Port != "9999" {
		t.Errorf("Port = %q, want 9999 (from env.custom)", c.Port)
	}
	if c.PyrightBin != "/custom/pyright" {
		t.Errorf("PyrightBin = %q, want /custom/pyright (from env.custom)", c.PyrightBin)
	}
	// Field not in env.custom should keep env.lsp value.
	if c.Workspace != "/tmp/lsp-test-workspace" {
		t.Errorf("Workspace = %q, want unchanged /tmp/lsp-test-workspace", c.Workspace)
	}
}

func TestConfigCLIFlagOverride(t *testing.T) {
	envLsp := testutil.FixturePath("env.lsp")
	flags := map[string]string{
		"LSP_PORT":        "8080",
		"LSP_GOPLS_BIN":   "/flag/gopls",
	}

	c, err := Load(envLsp, "", flags)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Port != "8080" {
		t.Errorf("Port = %q, want 8080 (from flags)", c.Port)
	}
	if c.GoplsBin != "/flag/gopls" {
		t.Errorf("GoplsBin = %q, want /flag/gopls (from flags)", c.GoplsBin)
	}
}

func TestConfigPathWithSpaces(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "env.lsp")
	if err := os.WriteFile(envPath, []byte(
		"LSP_GOPLS_BIN=/path/with spaces/gopls\n",
	), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	c, err := Load(envPath, "", nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.GoplsBin != "/path/with spaces/gopls" {
		t.Errorf("GoplsBin = %q, want path with spaces preserved", c.GoplsBin)
	}
}

func TestConfigMissingEnvCustomIsNotError(t *testing.T) {
	envLsp := testutil.FixturePath("env.lsp")
	_, err := Load(envLsp, "/nonexistent/env.custom", nil)
	if err != nil {
		t.Errorf("missing env.custom should not error, got: %v", err)
	}
}

func TestConfigMissingEnvLspIsError(t *testing.T) {
	_, err := Load("/nonexistent/env.lsp", "", nil)
	if err == nil {
		t.Error("missing env.lsp should return error")
	}
}
