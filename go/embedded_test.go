package vialite

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"
)

func TestEmbeddedRunNativeWaitsForRunAfterShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runCanExit := make(chan struct{})
	runExited := atomic.Bool{}
	shutdownCalled := atomic.Bool{}
	symbols := &nativeSymbols{
		run: func(unsafe.Pointer) int {
			<-runCanExit
			runExited.Store(true)
			return 0
		},
		shutdown: func(unsafe.Pointer) int {
			shutdownCalled.Store(true)
			return 0
		},
	}

	done := make(chan error, 1)
	go func() { done <- (&embeddedRunner{}).runNative(ctx, symbols, nil) }()
	cancel()

	select {
	case err := <-done:
		t.Fatalf("runNative returned before native run exited: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	if !shutdownCalled.Load() {
		t.Fatal("shutdown was not called")
	}
	close(runCanExit)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runNative returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("runNative did not return")
	}
	if !runExited.Load() {
		t.Fatal("runNative returned before native run exited")
	}
}

func TestEmbeddedRunnerAddsAndRemovesDynamicBackend(t *testing.T) {
	addedJSON := ""
	removedName := ""
	r := &embeddedRunner{
		backends: map[string]string{},
		thread:   unsafe.Pointer(uintptr(1)),
		symbols: &nativeSymbols{
			addBackend: func(_ unsafe.Pointer, backendJSON string) string {
				addedJSON = backendJSON
				return "127.0.0.1:41000"
			},
			removeBackend: func(_ unsafe.Pointer, name string) int {
				removedName = name
				return 0
			},
		},
	}
	r.healthy.Store(true)

	backend, err := normalizeBackend(Backend{Name: "Session-1", Address: "127.0.0.1:25566"})
	if err != nil {
		t.Fatalf("normalizeBackend: %v", err)
	}
	addr, err := r.addBackend(context.Background(), backend)
	if err != nil {
		t.Fatalf("addBackend: %v", err)
	}
	if addr != "127.0.0.1:41000" {
		t.Fatalf("addBackend address = %q", addr)
	}
	if addedJSON == "" {
		t.Fatal("addBackend did not call native addBackend")
	}
	got, err := r.backendAddress("session-1")
	if err != nil {
		t.Fatalf("backendAddress: %v", err)
	}
	if got != addr {
		t.Fatalf("backendAddress = %q, want %q", got, addr)
	}
	if err := r.removeBackend(context.Background(), "SESSION-1"); err != nil {
		t.Fatalf("removeBackend: %v", err)
	}
	if removedName != "SESSION-1" {
		t.Fatalf("removeBackend name = %q", removedName)
	}
	if _, err := r.backendAddress("session-1"); !errors.Is(err, ErrBackendNotFound) {
		t.Fatalf("backendAddress after remove = %v, want ErrBackendNotFound", err)
	}
}

func TestEmbeddedRunnerRejectsDynamicBackendWhenStopped(t *testing.T) {
	r := &embeddedRunner{backends: map[string]string{}}
	backend, err := normalizeBackend(Backend{Name: "session-1", Address: "127.0.0.1:25566"})
	if err != nil {
		t.Fatalf("normalizeBackend: %v", err)
	}
	if _, err := r.addBackend(context.Background(), backend); !errors.Is(err, ErrNotStarted) {
		t.Fatalf("addBackend stopped = %v, want ErrNotStarted", err)
	}
	if err := r.removeBackend(context.Background(), "session-1"); !errors.Is(err, ErrNotStarted) {
		t.Fatalf("removeBackend stopped = %v, want ErrNotStarted", err)
	}
}
