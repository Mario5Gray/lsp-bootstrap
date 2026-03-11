package main

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/lsp-bootstrap/lsp-mcp-bridge/testutil"
)

func TestInitializeCompletes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	mock := testutil.NewMockLSP()
	w, r := mock.StartInProcess(ctx)
	client := NewLspClientFromPipes(w, r)
	go client.readLoop()

	if err := client.Initialize("/tmp/workspace"); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	// initialized is a notification — it arrives asynchronously; poll briefly.
	var received []string
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		received = mock.Received()
		if len(received) >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if len(received) < 2 {
		t.Fatalf("expected at least 2 messages (initialize + initialized), got %v", received)
	}
	if received[0] != "initialize" {
		t.Errorf("first message = %q, want initialize", received[0])
	}
	if received[1] != "initialized" {
		t.Errorf("second message = %q, want initialized", received[1])
	}
}

func TestConcurrentRequests(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	mock := testutil.NewMockLSP()
	mock.RespondTo("textDocument/hover", json.RawMessage(`{"contents":"hover result"}`))
	w, r := mock.StartInProcess(ctx)
	client := NewLspClientFromPipes(w, r)
	go client.readLoop()

	// Initialise first.
	if err := client.Initialize("/tmp/workspace"); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	// Fire two concurrent requests.
	var wg sync.WaitGroup
	results := make([]json.RawMessage, 2)
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx], errs[idx] = client.Request("textDocument/hover", map[string]any{"idx": idx})
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("request %d error: %v", i, err)
		}
	}
	// Both should return the hover result.
	for i, raw := range results {
		if string(raw) != `{"contents":"hover result"}` {
			t.Errorf("request %d result = %s, want hover result", i, raw)
		}
	}
}

func TestNotificationRaceRegisterBeforeSend(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	mock := testutil.NewMockLSP()
	w, r := mock.StartInProcess(ctx)
	client := NewLspClientFromPipes(w, r)
	go client.readLoop()

	if err := client.Initialize("/tmp/workspace"); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	// Register before triggering the notification.
	uri := PathToURI("/tmp/workspace/sample.py")
	key := "textDocument/publishDiagnostics:" + uri
	ch := client.RegisterNotification(key)

	// Send didOpen — mock will send a publishDiagnostics via the mock's RespondTo
	// mechanism doesn't send notifications, so we trigger it manually via the mock.
	// For this test we verify that a notification pushed by the server
	// (simulated by calling dispatchNotification directly) is received on the channel.
	raw := json.RawMessage(`{"uri":"` + uri + `","diagnostics":[]}`)
	client.dispatchNotification(rpcResponse{
		JSONRPC: "2.0",
		Method:  "textDocument/publishDiagnostics",
		Params:  raw,
	})

	select {
	case params := <-ch:
		if string(params) != string(raw) {
			t.Errorf("notification params = %s, want %s", params, raw)
		}
	case <-time.After(time.Second):
		t.Error("timed out waiting for notification")
	}
}

func TestReaderLoopDrainsOnEOF(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	mock := testutil.NewMockLSP()
	mock.CrashOn("textDocument/hover")
	w, r := mock.StartInProcess(ctx)
	client := NewLspClientFromPipes(w, r)
	go client.readLoop()

	if err := client.Initialize("/tmp/workspace"); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	// This request should cause the mock to crash (stop serving), which
	// means the client will never get a response. The read loop detects EOF
	// and must drain all pending channels.
	done := make(chan error, 1)
	go func() {
		_, err := client.Request("textDocument/hover", nil)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Error("expected error after server crash, got nil")
		}
	case <-time.After(3 * time.Second):
		t.Error("timed out — read loop did not drain pending on EOF")
	}
}
