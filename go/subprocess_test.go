package vialite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestSubprocessRunnerStartsAndStops(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script subprocess test is unix-only")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "vialite")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntrap 'exit 0' TERM INT\nwhile true; do sleep 1; done\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	opts, err := Options{
		Mode:       ModeSubprocess,
		BinaryPath: script,
		Backends:   []Backend{{Name: "lobby", Address: "127.0.0.1:25566"}},
	}.validate()
	if err != nil {
		t.Fatalf("validate: %v", err)
	}

	srv := &Server{opts: opts, runner: &subprocessRunner{}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Start(ctx) }()

	deadline := time.After(time.Second)
	for !srv.Healthy() {
		select {
		case <-deadline:
			cancel()
			t.Fatal("subprocess never became healthy")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	cancel()
	err = <-done
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Start returned %v, want context.Canceled", err)
	}
}
