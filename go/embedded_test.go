package vialite

import (
	"context"
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
