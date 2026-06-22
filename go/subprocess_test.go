package vialite

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestSubprocessRunnerStartsAndStops(t *testing.T) {
	bin := buildSubprocessHelper(t)

	opts, err := Options{
		Mode:       ModeSubprocess,
		BinaryPath: bin,
		Backends:   []Backend{{Name: "lobby", Address: "127.0.0.1:25566"}},
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
	addr, err := srv.BackendDialAddress("lobby")
	if err != nil {
		cancel()
		t.Fatalf("BackendDialAddress: %v", err)
	}
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		cancel()
		t.Fatalf("dial backend address %s: %v", addr, err)
	}
	_ = conn.Close()
	if err := srv.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	err = <-done
	if err != nil {
		t.Fatalf("Start returned %v, want nil", err)
	}
	cancel()
}

func TestSubprocessBackendAddressIsDialableWhenBindUsesPortZero(t *testing.T) {
	addr, err := loopbackBackendAddress("127.0.0.1:0", 0)
	if err != nil {
		t.Fatalf("loopbackBackendAddress: %v", err)
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("SplitHostPort: %v", err)
	}
	if host != "127.0.0.1" {
		t.Fatalf("host = %q, want 127.0.0.1", host)
	}
	if port == "0" {
		t.Fatalf("port = %q, want allocated dialable port", port)
	}
}

func TestSubprocessRunnerRestartsFailedProcess(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "attempt")
	restarted := filepath.Join(dir, "restarted")
	bin := buildSubprocessHelper(t)
	t.Setenv("VIALITE_HELPER_FAIL_ONCE", marker)
	t.Setenv("VIALITE_HELPER_RESTARTED", restarted)

	opts, err := Options{
		Mode:       ModeSubprocess,
		BinaryPath: bin,
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

func TestSubprocessRunnerStartsOneProcessPerBackend(t *testing.T) {
	dir := t.TempDir()
	bin := buildSubprocessHelper(t)
	t.Setenv("VIALITE_HELPER_RECORD_DIR", dir)

	opts, err := Options{
		Mode:            ModeSubprocess,
		BinaryPath:      bin,
		ShutdownTimeout: time.Second,
		Backends: []Backend{
			{Name: "old", Address: "127.0.0.1:25566"},
			{Name: "new", Address: "127.0.0.1:25567"},
		},
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
	oldAddr, err := srv.BackendDialAddress("old")
	if err != nil {
		cancel()
		t.Fatalf("old BackendDialAddress: %v", err)
	}
	newAddr, err := srv.BackendDialAddress("new")
	if err != nil {
		cancel()
		t.Fatalf("new BackendDialAddress: %v", err)
	}
	if oldAddr == newAddr {
		cancel()
		t.Fatalf("backend addresses are equal: %s", oldAddr)
	}
	for _, addr := range []string{oldAddr, newAddr} {
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err != nil {
			cancel()
			t.Fatalf("dial backend listener %s: %v", addr, err)
		}
		_ = conn.Close()
	}
	if got := countFiles(t, dir, "config-*.json"); got != 2 {
		cancel()
		t.Fatalf("started configs = %d, want 2", got)
	}
	if err := srv.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Start returned %v, want nil", err)
	}
	cancel()
}

func countFiles(t *testing.T, dir, pattern string) int {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil {
		t.Fatal(err)
	}
	return len(matches)
}

func buildSubprocessHelper(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	bin := filepath.Join(dir, "vialite-helper")
	if err := os.WriteFile(src, []byte(subprocessHelperSource), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "build", "-o", bin, src)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build subprocess helper: %v\n%s", err, out)
	}
	return bin
}

const subprocessHelperSource = `package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
)

type nativeConfig struct {
	Bind string ` + "`json:\"bind\"`" + `
}

func main() {
	if marker := os.Getenv("VIALITE_HELPER_FAIL_ONCE"); marker != "" {
		if _, err := os.Stat(marker); os.IsNotExist(err) {
			_ = os.WriteFile(marker, []byte("1"), 0o644)
			os.Exit(7)
		}
	}
	cfgPath := ""
	for i, arg := range os.Args {
		if arg == "--config" && i+1 < len(os.Args) {
			cfgPath = os.Args[i+1]
			break
		}
	}
	if cfgPath == "" {
		os.Exit(2)
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		os.Exit(3)
	}
	if dir := os.Getenv("VIALITE_HELPER_RECORD_DIR"); dir != "" {
		name := filepath.Join(dir, fmt.Sprintf("config-%d.json", os.Getpid()))
		_ = os.WriteFile(name, data, 0o644)
	}
var cfg nativeConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		os.Exit(4)
	}
	ln, err := net.Listen("tcp", cfg.Bind)
	if err != nil {
		os.Exit(5)
	}
	if path := os.Getenv("VIALITE_HELPER_RESTARTED"); path != "" {
		_ = os.WriteFile(path, []byte("1"), 0o644)
	}
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		_ = conn.Close()
	}
}
`
