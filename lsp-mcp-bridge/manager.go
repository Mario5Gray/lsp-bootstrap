package main

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// extToSlot maps file extension → language slot name.
var extToSlot = map[string]string{
	".py":    "python",
	".js":    "typescript",  ".jsx": "typescript",
	".ts":    "typescript",  ".tsx": "typescript",   ".mjs": "typescript",
	".rs":    "rust",
	".go":    "go",
	".kt":    "kotlin",      ".kts": "kotlin",
	".scala": "scala",       ".sc":  "scala",
	".sh":    "shell",       ".bash": "shell",       ".zsh": "shell",
	".yml":   "yaml",        ".yaml": "yaml",
	".toml":  "toml",
	".json":  "json",        ".jsonc": "json",
	".html":  "html",        ".htm": "html",
	".css":   "css",         ".scss": "css",         ".less": "css",
}

// slotArgs maps slot name → extra args passed to the language server binary.
var slotArgs = map[string][]string{
	"python":     {"--stdio"},
	"typescript": {"--stdio"},
	"rust":       {},
	"go":         {},
	"kotlin":     {},
	"scala":      {},
	"shell":      {"--stdio"},
	"yaml":       {"--stdio"},
	"toml":       {"lsp", "stdio"},
	"json":       {"--stdio"},
	"html":       {"--stdio"},
	"css":        {"--stdio"},
}

// slotDef describes a language server binary + args.
type slotDef struct {
	binary string
	args   []string
}

// slotState tracks a live (or dead) language server for one slot.
type slotState struct {
	client      *LspClient
	failures    int
	lastFailure time.Time
	dead        bool
}

const (
	maxFailures      = 3
	failureWindowSec = 30
)

// Manager owns all language server slots and routes file paths to clients.
type Manager struct {
	cfg   *Config
	slots map[string]*slotState // slot name → state
	defs  map[string]slotDef    // slot name → binary + args
	mu    sync.Mutex
}

// NewManager builds a Manager from the given config.
// Slots whose binary field is empty are left unconfigured (calls will error).
func NewManager(cfg *Config) *Manager {
	m := &Manager{
		cfg:   cfg,
		slots: make(map[string]*slotState),
		defs:  make(map[string]slotDef),
	}

	binaries := map[string]string{
		"python":     cfg.PyrightBin,
		"typescript": cfg.TSSBin,
		"rust":       cfg.RustBin,
		"go":         cfg.GoplsBin,
		"kotlin":     cfg.KotlinBin,
		"scala":      cfg.MetalsBin,
		"shell":      cfg.BashBin,
		"yaml":       cfg.YamlBin,
		"toml":       cfg.TaploBin,
		"json":       cfg.JSONBin,
		"html":       cfg.HTMLBin,
		"css":        cfg.CSSBin,
	}

	for slot, bin := range binaries {
		if bin == "" {
			continue
		}
		m.defs[slot] = slotDef{binary: bin, args: slotArgs[slot]}
		m.slots[slot] = &slotState{}
	}
	return m
}

// GetClient returns a live LspClient for the given file path.
// If the client is not running it is started; crashed clients are restarted
// up to maxFailures times within failureWindowSec seconds before the slot
// is marked permanently dead.
func (m *Manager) GetClient(filePath string) (*LspClient, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	slotName, ok := extToSlot[ext]
	if !ok {
		return nil, fmt.Errorf("no LSP slot for extension %q", ext)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	state, ok := m.slots[slotName]
	if !ok {
		return nil, fmt.Errorf("LSP slot %q not configured (binary path empty)", slotName)
	}

	if state.dead {
		return nil, fmt.Errorf("LSP slot %q permanently failed after %d attempts", slotName, maxFailures)
	}

	if state.client != nil && state.client.isAlive.Load() {
		return state.client, nil
	}

	// Check restart limit.
	if state.failures >= maxFailures &&
		time.Since(state.lastFailure) < failureWindowSec*time.Second {
		state.dead = true
		return nil, fmt.Errorf("LSP slot %q permanently failed after %d restart attempts in %ds",
			slotName, maxFailures, failureWindowSec)
	}

	// Start a new client.
	def := m.defs[slotName]
	client, err := m.startClient(slotName, def, filePath)
	if err != nil {
		state.failures++
		state.lastFailure = time.Now()
		return nil, fmt.Errorf("LSP slot %q start failed: %w", slotName, err)
	}

	state.client = client
	return client, nil
}

// startClient creates, starts, and initialises a new LspClient for the slot.
// Must be called with m.mu held.
func (m *Manager) startClient(slotName string, def slotDef, filePath string) (*LspClient, error) {
	workspace := m.cfg.Workspace

	// gopls needs rootUri at the go.mod level, not the repo root.
	if slotName == "go" {
		if modDir := findGoMod(filePath); modDir != "" {
			workspace = modDir
		}
	}

	client := NewLspClient(def.binary, def.args)
	ctx := context.Background()
	if err := client.Start(ctx); err != nil {
		return nil, err
	}
	if err := client.Initialize(workspace); err != nil {
		client.Close()
		return nil, err
	}
	return client, nil
}

// Shutdown gracefully stops all running language servers.
func (m *Manager) Shutdown(ctx context.Context) {
	m.mu.Lock()
	clients := make([]*LspClient, 0, len(m.slots))
	for _, state := range m.slots {
		if state.client != nil && state.client.isAlive.Load() {
			clients = append(clients, state.client)
		}
	}
	m.mu.Unlock()

	for _, c := range clients {
		shutdownCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		_, _ = c.Request("shutdown", nil)
		_ = c.Notify("exit", nil)
		cancel()
		if c.cmd != nil && c.cmd.Process != nil {
			done := make(chan struct{})
			go func(cmd *exec.Cmd) {
				cmd.Wait() //nolint:errcheck
				close(done)
			}(c.cmd)
			select {
			case <-done:
			case <-shutdownCtx.Done():
				c.cmd.Process.Kill() //nolint:errcheck
			}
		}
	}
}

// findGoMod walks up from filePath looking for a go.mod file.
// Returns the directory containing go.mod, or "" if not found.
func findGoMod(filePath string) string {
	dir := filepath.Dir(filePath)
	for {
		if _, err := filepath.Abs(filepath.Join(dir, "go.mod")); err == nil {
			// Check existence.
			if _, err2 := filepath.EvalSymlinks(filepath.Join(dir, "go.mod")); err2 == nil {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
