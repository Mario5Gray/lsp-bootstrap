package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// rpcResponse is an inbound LSP message (response or notification).
type rpcResponse struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method,omitempty"`
	Params  json.RawMessage  `json:"params,omitempty"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   *rpcError        `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("LSP error %d: %s", e.Code, e.Message) }

// LspClient manages a single LSP server subprocess.
type LspClient struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader

	nextID atomic.Int64

	mu        sync.Mutex
	pending   map[int64]chan rpcResponse
	listeners map[string]chan json.RawMessage // "method:uri" → channel

	isAlive atomic.Bool
}

// ErrClientDead is returned by Request/Notify when the server is no longer running.
var ErrClientDead = errors.New("lsp client: server not running")

// NewLspClient creates a client that will launch the given binary with args.
// Call Start to actually launch the process.
func NewLspClient(binary string, args []string) *LspClient {
	c := &LspClient{
		pending:   make(map[int64]chan rpcResponse),
		listeners: make(map[string]chan json.RawMessage),
	}
	c.cmd = exec.Command(binary, args...)
	return c
}

// NewLspClientFromPipes creates a client connected to existing io pipes.
// Used in tests to wire a MockLSP without launching a real subprocess.
func NewLspClientFromPipes(w io.WriteCloser, r io.ReadCloser) *LspClient {
	c := &LspClient{
		pending:   make(map[int64]chan rpcResponse),
		listeners: make(map[string]chan json.RawMessage),
		stdin:     w,
		stdout:    bufio.NewReader(r),
	}
	c.isAlive.Store(true)
	return c
}

// Start launches the subprocess and starts the read loop.
func (c *LspClient) Start(ctx context.Context) error {
	stdin, err := c.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("lsp start: stdin pipe: %w", err)
	}
	stdout, err := c.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("lsp start: stdout pipe: %w", err)
	}
	c.stdin = stdin
	c.stdout = bufio.NewReader(stdout)

	if err := c.cmd.Start(); err != nil {
		return fmt.Errorf("lsp start: exec: %w", err)
	}
	c.isAlive.Store(true)
	go c.readLoop()
	return nil
}

// Initialize performs the LSP initialize + initialized handshake.
func (c *LspClient) Initialize(workspace string) error {
	params := map[string]any{
		"processId":    os.Getpid(),
		"rootUri":      PathToURI(workspace),
		"rootPath":     workspace,
		"capabilities": map[string]any{
			"textDocument": map[string]any{
				"hover":          map[string]any{"contentFormat": []string{"plaintext", "markdown"}},
				"definition":     map[string]any{},
				"references":     map[string]any{},
				"publishDiagnostics": map[string]any{},
				"signatureHelp":  map[string]any{},
				"callHierarchy":  map[string]any{},
				"rename":         map[string]any{},
			},
		},
	}
	_, err := c.Request("initialize", params)
	if err != nil {
		return fmt.Errorf("lsp initialize: %w", err)
	}
	// initialized is a notification — no response expected.
	return c.Notify("initialized", map[string]any{})
}

// Request sends an LSP request and waits for the response.
func (c *LspClient) Request(method string, params any) (json.RawMessage, error) {
	if !c.isAlive.Load() {
		return nil, ErrClientDead
	}

	id := c.nextID.Add(1)
	ch := make(chan rpcResponse, 1)

	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()

	msg := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}
	if err := c.writeMessage(msg); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("lsp request %s: write: %w", method, err)
	}

	resp := <-ch
	if resp.Error != nil {
		return nil, resp.Error
	}
	return resp.Result, nil
}

// Notify sends an LSP notification (no response expected).
func (c *LspClient) Notify(method string, params any) error {
	if !c.isAlive.Load() {
		return ErrClientDead
	}
	msg := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	}
	return c.writeMessage(msg)
}

// RegisterNotification registers a buffered channel to receive notifications
// matching the given key (format: "method:uri"). The caller must register
// before sending the trigger notification to avoid a race.
func (c *LspClient) RegisterNotification(key string) chan json.RawMessage {
	ch := make(chan json.RawMessage, 16)
	c.mu.Lock()
	c.listeners[key] = ch
	c.mu.Unlock()
	return ch
}

// Close sends shutdown + exit and kills the process.
func (c *LspClient) Close() {
	c.isAlive.Store(false)
	// Best-effort graceful shutdown.
	_ = c.Notify("exit", nil)
	if c.stdin != nil {
		c.stdin.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		c.cmd.Process.Kill() //nolint:errcheck
	}
}

// ── internal ─────────────────────────────────────────────────────────────────

func (c *LspClient) readLoop() {
	defer c.drainPending()
	for {
		msg, err := c.readMessage()
		if err != nil {
			c.isAlive.Store(false)
			return
		}
		if msg.ID != nil && msg.Method == "" {
			c.resolveRequest(msg)
		}
		if msg.ID == nil && msg.Method != "" {
			c.dispatchNotification(msg)
		}
	}
}

func (c *LspClient) resolveRequest(msg rpcResponse) {
	var id int64
	if err := json.Unmarshal(*msg.ID, &id); err != nil {
		return
	}
	c.mu.Lock()
	ch, ok := c.pending[id]
	if ok {
		delete(c.pending, id)
	}
	c.mu.Unlock()
	if ok {
		ch <- msg
	}
}

func (c *LspClient) dispatchNotification(msg rpcResponse) {
	// Build listener key: "method:uri"
	// For publishDiagnostics the URI is in params.uri.
	key := msg.Method
	if msg.Method == "textDocument/publishDiagnostics" {
		var p struct {
			URI string `json:"uri"`
		}
		if err := json.Unmarshal(msg.Params, &p); err == nil && p.URI != "" {
			key = msg.Method + ":" + p.URI
		}
	}
	c.mu.Lock()
	ch, ok := c.listeners[key]
	c.mu.Unlock()
	if ok {
		select {
		case ch <- msg.Params:
		default:
			// Drop if buffer full — caller must consume promptly.
		}
	}
}

// drainPending unblocks all callers waiting on responses when the server exits.
func (c *LspClient) drainPending() {
	dead := rpcResponse{Error: &rpcError{Code: -32099, Message: "server exited"}}
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, ch := range c.pending {
		ch <- dead
		delete(c.pending, id)
	}
}

func (c *LspClient) writeMessage(v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))
	if _, err := io.WriteString(c.stdin, header); err != nil {
		return err
	}
	_, err = c.stdin.Write(body)
	return err
}

func (c *LspClient) readMessage() (rpcResponse, error) {
	var contentLength int
	for {
		line, err := c.stdout.ReadString('\n')
		if err != nil {
			return rpcResponse{}, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(line, "Content-Length:") {
			val := strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:"))
			contentLength, err = strconv.Atoi(val)
			if err != nil {
				return rpcResponse{}, fmt.Errorf("invalid Content-Length: %w", err)
			}
		}
	}
	if contentLength == 0 {
		return rpcResponse{}, fmt.Errorf("missing Content-Length")
	}
	buf := make([]byte, contentLength)
	if _, err := io.ReadFull(c.stdout, buf); err != nil {
		return rpcResponse{}, err
	}
	var msg rpcResponse
	if err := json.Unmarshal(buf, &msg); err != nil {
		return rpcResponse{}, fmt.Errorf("json unmarshal: %w", err)
	}
	return msg, nil
}
