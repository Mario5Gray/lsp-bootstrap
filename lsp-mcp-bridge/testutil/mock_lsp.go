package testutil

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// MockLSP is a scriptable LSP server that communicates over stdin/stdout.
// It records all received requests and responds with pre-configured replies.
// Use NewMockLSP to construct; call Start to launch as a subprocess pair.
type MockLSP struct {
	mu       sync.Mutex
	handlers map[string]json.RawMessage // method → canned result
	received []rpcMessage               // all messages received, in order
	crashAt  string                     // if set, exit(1) when this method is received
	in       io.Writer
	out      io.Reader
}

type rpcMessage struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method,omitempty"`
	Params  json.RawMessage  `json:"params,omitempty"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   json.RawMessage  `json:"error,omitempty"`
}

// NewMockLSP returns a MockLSP with default handlers for initialize and initialized.
func NewMockLSP() *MockLSP {
	m := &MockLSP{
		handlers: make(map[string]json.RawMessage),
	}
	// Default: initialize responds with empty capabilities.
	m.RespondTo("initialize", json.RawMessage(`{"capabilities":{}}`))
	return m
}

// RespondTo registers a canned result for the given LSP method.
func (m *MockLSP) RespondTo(method string, result json.RawMessage) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handlers[method] = result
}

// CrashOn causes the mock to exit(1) when it receives the given method.
func (m *MockLSP) CrashOn(method string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.crashAt = method
}

// Received returns all messages the mock has received, in order.
func (m *MockLSP) Received() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	methods := make([]string, 0, len(m.received))
	for _, msg := range m.received {
		methods = append(methods, msg.Method)
	}
	return methods
}

// StartInProcess runs the mock LSP server in-process using a pipe pair.
// Returns (clientStdin, clientStdout) to pass to NewLspClient.
// The server runs until ctx is cancelled.
func (m *MockLSP) StartInProcess(ctx context.Context) (stdin io.WriteCloser, stdout io.ReadCloser) {
	serverIn, clientOut := io.Pipe()  // client writes here → server reads
	clientIn, serverOut := io.Pipe()  // server writes here → client reads

	go func() {
		m.serve(ctx, serverIn, serverOut)
		// Close the write end so the client's reader sees EOF.
		serverOut.Close()
		serverIn.Close()
	}()

	return clientOut, clientIn
}

func (m *MockLSP) serve(ctx context.Context, in io.Reader, out io.Writer) {
	reader := bufio.NewReader(in)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		msg, err := readMessage(reader)
		if err != nil {
			return
		}

		m.mu.Lock()
		m.received = append(m.received, msg)
		crashAt := m.crashAt
		m.mu.Unlock()

		if crashAt != "" && msg.Method == crashAt {
			return // simulate crash
		}

		// Notifications have no id — do not respond.
		if msg.ID == nil {
			continue
		}

		m.mu.Lock()
		result, ok := m.handlers[msg.Method]
		m.mu.Unlock()

		if !ok {
			result = json.RawMessage(`null`)
		}

		resp := rpcMessage{
			JSONRPC: "2.0",
			ID:      msg.ID,
			Result:  result,
		}
		if err := writeMessage(out, resp); err != nil {
			return
		}
	}
}

// StartSubprocess launches the mock as a real child process via a helper binary.
// Used for testing crash/restart behaviour where the process must truly exit.
// Requires TestMain to register the mock entrypoint (see RunIfMockLSP).
func StartMockSubprocess(t *testing.T, args ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], append([]string{"-mock-lsp"}, args...)...)
	cmd.Env = append(os.Environ(), "LSP_MOCK=1")
	return cmd
}

// RunIfMockLSP checks if the current process was launched as a mock LSP
// subprocess and, if so, runs the mock server and exits. Call at the top
// of TestMain before m.Run().
func RunIfMockLSP() bool {
	for _, arg := range os.Args {
		if arg == "-mock-lsp" {
			mock := NewMockLSP()
			ctx := context.Background()
			r, w := io.Pipe()
			go mock.serve(ctx, os.Stdin, w)
			io.Copy(os.Stdout, r) //nolint:errcheck
			os.Exit(0)
		}
	}
	return false
}

// readMessage reads one Content-Length-framed JSON-RPC message from r.
func readMessage(r *bufio.Reader) (rpcMessage, error) {
	var contentLength int
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return rpcMessage{}, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(line, "Content-Length:") {
			val := strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:"))
			contentLength, err = strconv.Atoi(val)
			if err != nil {
				return rpcMessage{}, fmt.Errorf("invalid Content-Length: %w", err)
			}
		}
	}
	if contentLength == 0 {
		return rpcMessage{}, fmt.Errorf("missing Content-Length")
	}
	buf := make([]byte, contentLength)
	if _, err := io.ReadFull(r, buf); err != nil {
		return rpcMessage{}, err
	}
	var msg rpcMessage
	if err := json.Unmarshal(buf, &msg); err != nil {
		return rpcMessage{}, err
	}
	return msg, nil
}

// writeMessage writes one Content-Length-framed JSON-RPC message to w.
func writeMessage(w io.Writer, msg rpcMessage) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(body))
	if err != nil {
		return err
	}
	_, err = w.Write(body)
	return err
}
