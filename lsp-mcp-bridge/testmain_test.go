package main

import (
	"os"
	"testing"

	"github.com/lsp-bootstrap/lsp-mcp-bridge/testutil"
)

func TestMain(m *testing.M) {
	// If this process was launched as a mock LSP subprocess, run the mock and exit.
	if testutil.RunIfMockLSP() {
		os.Exit(0)
	}
	os.Exit(m.Run())
}
