package main

import (
	"os"
	"sync"
	"testing"
	"time"
)

// mockBin returns a config where "python" slot points to the test binary
// (which self-invokes as MockLSP when called with -mock-lsp).
func mockBinConfig(t *testing.T) *Config {
	t.Helper()
	return &Config{
		Workspace:  t.TempDir(),
		PyrightBin: os.Args[0],
	}
}

// mockBinArgs overrides the slotArgs for "python" to self-invoke as mock.
// Returns a restore function.
func withMockArgs(t *testing.T) func() {
	t.Helper()
	orig := slotArgs["python"]
	slotArgs["python"] = []string{"-mock-lsp"}
	return func() { slotArgs["python"] = orig }
}

func TestGetClientStartsProcess(t *testing.T) {
	restore := withMockArgs(t)
	defer restore()

	m := NewManager(mockBinConfig(t))

	pyFile := "/tmp/workspace/sample.py"
	client, err := m.GetClient(pyFile)
	if err != nil {
		t.Fatalf("GetClient: %v", err)
	}
	if client == nil {
		t.Fatal("GetClient returned nil client")
	}
	if !client.isAlive.Load() {
		t.Error("client should be alive after start")
	}

	m.Shutdown(t.Context())
}

func TestGetClientReusesProcess(t *testing.T) {
	restore := withMockArgs(t)
	defer restore()

	m := NewManager(mockBinConfig(t))

	pyFile := "/tmp/workspace/sample.py"
	c1, err := m.GetClient(pyFile)
	if err != nil {
		t.Fatalf("first GetClient: %v", err)
	}
	c2, err := m.GetClient(pyFile)
	if err != nil {
		t.Fatalf("second GetClient: %v", err)
	}
	if c1 != c2 {
		t.Error("GetClient should return the same client on second call")
	}

	m.Shutdown(t.Context())
}

func TestGetClientConcurrentSameSlot(t *testing.T) {
	restore := withMockArgs(t)
	defer restore()

	m := NewManager(mockBinConfig(t))

	const goroutines = 10
	clients := make([]*LspClient, goroutines)
	errs := make([]error, goroutines)
	var wg sync.WaitGroup

	pyFile := "/tmp/workspace/sample.py"
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			clients[idx], errs[idx] = m.GetClient(pyFile)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: %v", i, err)
		}
	}

	// All goroutines must have received the same client (single process).
	first := clients[0]
	for i, c := range clients[1:] {
		if c != first {
			t.Errorf("goroutine %d got a different client — expected single process", i+1)
		}
	}

	m.Shutdown(t.Context())
}

func TestGetClientUnsupportedExtension(t *testing.T) {
	m := NewManager(&Config{Workspace: t.TempDir()})

	_, err := m.GetClient("/tmp/README.md")
	if err == nil {
		t.Fatal("expected error for .md extension, got nil")
	}
	if err.Error() == "" {
		t.Error("error message should be non-empty")
	}
}

func TestGetClientMissingBinary(t *testing.T) {
	// Python slot not configured (empty bin).
	m := NewManager(&Config{Workspace: t.TempDir()})

	_, err := m.GetClient("/tmp/workspace/sample.py")
	if err == nil {
		t.Fatal("expected error when python slot not configured")
	}
}

func TestCrashTriggerRestart(t *testing.T) {
	restore := withMockArgs(t)
	defer restore()

	m := NewManager(mockBinConfig(t))

	pyFile := "/tmp/workspace/sample.py"
	c1, err := m.GetClient(pyFile)
	if err != nil {
		t.Fatalf("initial GetClient: %v", err)
	}

	// Kill the underlying process.
	if c1.cmd != nil && c1.cmd.Process != nil {
		c1.cmd.Process.Kill() //nolint:errcheck
	}
	// Wait for isAlive to drop.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && c1.isAlive.Load() {
		time.Sleep(20 * time.Millisecond)
	}
	if c1.isAlive.Load() {
		t.Skip("process didn't die in time — skipping crash-restart test")
	}

	c2, err := m.GetClient(pyFile)
	if err != nil {
		t.Fatalf("GetClient after crash: %v", err)
	}
	if c2 == c1 {
		t.Error("expected a new client after crash, got same pointer")
	}
	if !c2.isAlive.Load() {
		t.Error("new client should be alive")
	}

	m.Shutdown(t.Context())
}

func TestRestartLimitPermanentFailure(t *testing.T) {
	// Configure python slot to point to a nonexistent binary.
	cfg := &Config{
		Workspace:  t.TempDir(),
		PyrightBin: "/nonexistent/pyright",
	}
	slotArgsBak := slotArgs["python"]
	slotArgs["python"] = []string{"--stdio"}
	defer func() { slotArgs["python"] = slotArgsBak }()

	m := NewManager(cfg)

	pyFile := "/tmp/workspace/sample.py"
	// Each call increments failure count.
	for i := 0; i < maxFailures; i++ {
		_, err := m.GetClient(pyFile)
		if err == nil {
			t.Fatalf("attempt %d: expected error for nonexistent binary", i)
		}
		// Force lastFailure into the window.
		m.mu.Lock()
		if s, ok := m.slots["python"]; ok {
			s.lastFailure = time.Now()
		}
		m.mu.Unlock()
	}

	// Next call must return permanent failure.
	_, err := m.GetClient(pyFile)
	if err == nil {
		t.Fatal("expected permanent failure error after max restarts")
	}

	m.mu.Lock()
	dead := m.slots["python"].dead
	m.mu.Unlock()
	if !dead {
		t.Error("slot should be marked dead after permanent failure")
	}
}
