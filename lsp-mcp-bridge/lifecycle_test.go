package main

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/http"
	"sync"
	"testing"
	"time"
)

type fakeBridgeServer struct {
	startErr   error
	runtimeErr error

	started  chan struct{}
	release  chan struct{}
	shutdown int
	mu       sync.Mutex
}

func newFakeBridgeServer() *fakeBridgeServer {
	return &fakeBridgeServer{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (f *fakeBridgeServer) Start(string) error {
	close(f.started)
	if f.startErr != nil {
		return f.startErr
	}
	<-f.release
	if f.runtimeErr != nil {
		return f.runtimeErr
	}
	return http.ErrServerClosed
}

func (f *fakeBridgeServer) Shutdown(context.Context) error {
	f.mu.Lock()
	f.shutdown++
	f.mu.Unlock()

	select {
	case <-f.release:
	default:
		close(f.release)
	}
	return nil
}

func (f *fakeBridgeServer) shutdownCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.shutdown
}

type fakeManagerShutdowner struct {
	count int
	mu    sync.Mutex
}

func (f *fakeManagerShutdowner) Shutdown(context.Context) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.count++
}

func (f *fakeManagerShutdowner) shutdownCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.count
}

func TestRunBridgeReturnsStartupErrorWithoutShutdown(t *testing.T) {
	srv := newFakeBridgeServer()
	srv.startErr = errors.New("bind failed")
	mgr := &fakeManagerShutdowner{}
	logger := log.New(&bytes.Buffer{}, "", 0)

	err := runBridge(context.Background(), srv, mgr, ":7890", logger)
	if err == nil || err.Error() != "bind failed" {
		t.Fatalf("runBridge error = %v, want bind failed", err)
	}
	if got := srv.shutdownCount(); got != 0 {
		t.Fatalf("Shutdown count = %d, want 0", got)
	}
	if got := mgr.shutdownCount(); got != 0 {
		t.Fatalf("manager Shutdown count = %d, want 0", got)
	}
}

func TestRunBridgeShutsDownServerAndManagerOnCancel(t *testing.T) {
	srv := newFakeBridgeServer()
	mgr := &fakeManagerShutdowner{}
	var logs bytes.Buffer
	logger := log.New(&logs, "", 0)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runBridge(ctx, srv, mgr, ":7890", logger)
	}()

	select {
	case <-srv.started:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not start")
	}

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runBridge returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runBridge did not return after cancel")
	}

	if got := srv.shutdownCount(); got != 1 {
		t.Fatalf("Shutdown count = %d, want 1", got)
	}
	if got := mgr.shutdownCount(); got != 1 {
		t.Fatalf("manager Shutdown count = %d, want 1", got)
	}
	if !bytes.Contains(logs.Bytes(), []byte("shutting down")) {
		t.Fatalf("expected shutdown log, got %q", logs.String())
	}
}

func TestRunBridgeReturnsRuntimeErrorBeforeCancel(t *testing.T) {
	srv := newFakeBridgeServer()
	srv.runtimeErr = errors.New("listener crashed")
	mgr := &fakeManagerShutdowner{}
	logger := log.New(&bytes.Buffer{}, "", 0)

	done := make(chan error, 1)
	go func() {
		done <- runBridge(context.Background(), srv, mgr, ":7890", logger)
	}()

	select {
	case <-srv.started:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not start")
	}

	close(srv.release)

	select {
	case err := <-done:
		if err == nil || err.Error() != "listener crashed" {
			t.Fatalf("runBridge error = %v, want listener crashed", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runBridge did not return runtime error")
	}

	if got := srv.shutdownCount(); got != 0 {
		t.Fatalf("Shutdown count = %d, want 0", got)
	}
	if got := mgr.shutdownCount(); got != 0 {
		t.Fatalf("manager Shutdown count = %d, want 0", got)
	}
}
