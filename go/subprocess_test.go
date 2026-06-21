package vialite

import (
	"context"
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
	if err := srv.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	err = <-done
	if err != nil {
		t.Fatalf("Start returned %v, want nil", err)
	}
	cancel()
}

func TestSubprocessRunnerRestartsFailedProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script subprocess test is unix-only")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "attempt")
	restarted := filepath.Join(dir, "restarted")
	script := filepath.Join(dir, "vialite")
	body := "#!/bin/sh\n" +
		"if [ ! -f " + marker + " ]; then touch " + marker + "; exit 7; fi\n" +
		"touch " + restarted + "\n" +
		"trap 'exit 0' TERM INT\n" +
		"while true; do sleep 1; done\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	opts, err := Options{
		Mode:       ModeSubprocess,
		BinaryPath: script,
		RestartPolicy: &RestartPolicy{
			MinBackoff: time.Millisecond,
			MaxBackoff: time.Millisecond,
			MaxRetries: 1,
		},
		ShutdownTimeout: time.Second,
		Backends:        []Backend{{Name: "lobby", Address: "127.0.0.1:25566"}},
	}.validate()
	if err != nil {
		t.Fatalf("validate: %v", err)
	}

	srv := &Server{opts: opts, runner: &subprocessRunner{}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Start(ctx) }()

	if err := srv.WaitReady(context.Background()); err != nil {
		cancel()
		t.Fatalf("WaitReady: %v", err)
	}
	deadline := time.After(time.Second)
	for {
		if _, err := os.Stat(restarted); err == nil {
			break
		}
		select {
		case <-deadline:
			cancel()
			t.Fatal("subprocess did not restart after first failure")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	if err := srv.Stop(context.Background()); err != nil {
		cancel()
		t.Fatalf("Stop: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Start returned %v, want nil", err)
	}
	cancel()
}
