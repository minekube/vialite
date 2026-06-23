package vialite

import (
	"context"
	"errors"
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

func TestSubprocessRunnerPublishesDistinctAddressesForMultipleBackends(t *testing.T) {
	bin := buildSubprocessHelper(t)

	opts, err := Options{
		Mode:       ModeSubprocess,
		BinaryPath: bin,
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
		t.Fatalf("BackendDialAddress old: %v", err)
	}
	newAddr, err := srv.BackendDialAddress("new")
	if err != nil {
		cancel()
		t.Fatalf("BackendDialAddress new: %v", err)
	}
	if oldAddr == newAddr {
		cancel()
		t.Fatalf("multiple backends share one subprocess listener: old=%s new=%s", oldAddr, newAddr)
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

func TestSubprocessBackendAddressIsDialableWhenBindUsesPortZero(t *testing.T) {
	addrs, err := loopbackBackendAddresses("127.0.0.1:0", []Backend{
		{Name: "old", Address: "127.0.0.1:25566"},
		{Name: "new", Address: "127.0.0.1:25567"},
	})
	if err != nil {
		t.Fatalf("loopbackBackendAddresses: %v", err)
	}
	if addrs["old"] == "" || addrs["new"] == "" {
		t.Fatalf("missing backend addresses: %#v", addrs)
	}
	if addrs["old"] == addrs["new"] {
		t.Fatalf("backend addresses reused one listener: %#v", addrs)
	}
	for name, addr := range addrs {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			t.Fatalf("%s SplitHostPort: %v", name, err)
		}
		if host != "127.0.0.1" {
			t.Fatalf("%s host = %q, want 127.0.0.1", name, host)
		}
		if port == "0" {
			t.Fatalf("%s port = %q, want allocated dialable port", name, port)
		}
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

func TestSubprocessRunnerAddsAndRemovesDynamicBackend(t *testing.T) {
	bin := buildSubprocessHelper(t)

	opts, err := Options{
		Mode:                 ModeSubprocess,
		BinaryPath:           bin,
		AllowDynamicBackends: true,
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
	addr, err := srv.AddBackend(context.Background(), Backend{Name: "session-1", Address: "127.0.0.1:25566"})
	if err != nil {
		cancel()
		t.Fatalf("AddBackend: %v", err)
	}
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		cancel()
		t.Fatalf("dial dynamic backend address %s: %v", addr, err)
	}
	_ = conn.Close()
	if err := srv.RemoveBackend(context.Background(), "SESSION-1"); err != nil {
		cancel()
		t.Fatalf("RemoveBackend: %v", err)
	}
	eventuallyNotDialable(t, addr)
	if err := srv.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Start returned %v, want nil", err)
	}
	cancel()
}

func TestSubprocessRunnerDynamicBackendsUseDistinctPortsWithFixedBind(t *testing.T) {
	bin := buildSubprocessHelper(t)

	opts, err := Options{
		Mode:                 ModeSubprocess,
		BinaryPath:           bin,
		Bind:                 "127.0.0.1:0",
		AllowDynamicBackends: true,
	}.validate()
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	fixedBind, err := concreteLoopbackBind("127.0.0.1:0")
	if err != nil {
		t.Fatalf("fixed bind: %v", err)
	}
	opts.Bind = fixedBind

	srv := &Server{opts: opts, runner: &subprocessRunner{}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Start(ctx) }()

	if err := srv.WaitReady(context.Background()); err != nil {
		cancel()
		t.Fatalf("WaitReady: %v", err)
	}
	first, err := srv.AddBackend(context.Background(), Backend{Name: "session-1", Address: "127.0.0.1:25566"})
	if err != nil {
		cancel()
		t.Fatalf("AddBackend first: %v", err)
	}
	second, err := srv.AddBackend(context.Background(), Backend{Name: "session-2", Address: "127.0.0.1:25567"})
	if err != nil {
		cancel()
		t.Fatalf("AddBackend second: %v", err)
	}
	if first == fixedBind || second == fixedBind || first == second {
		cancel()
		t.Fatalf("dynamic backends reused fixed/shared bind: fixed=%s first=%s second=%s", fixedBind, first, second)
	}
	if err := srv.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Start returned %v, want nil", err)
	}
	cancel()
}

func TestSubprocessRunnerDynamicBackendDoesNotReuseStaticFixedBind(t *testing.T) {
	bin := buildSubprocessHelper(t)
	fixedBind, err := concreteLoopbackBind("127.0.0.1:0")
	if err != nil {
		t.Fatalf("fixed bind: %v", err)
	}
	opts, err := Options{
		Mode:                 ModeSubprocess,
		BinaryPath:           bin,
		Bind:                 fixedBind,
		AllowDynamicBackends: true,
		Backends:             []Backend{{Name: "static", Address: "127.0.0.1:25566"}},
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
	staticAddr, err := srv.BackendDialAddress("static")
	if err != nil {
		cancel()
		t.Fatalf("BackendDialAddress static: %v", err)
	}
	dynamicAddr, err := srv.AddBackend(context.Background(), Backend{Name: "session-1", Address: "127.0.0.1:25567"})
	if err != nil {
		cancel()
		t.Fatalf("AddBackend: %v", err)
	}
	if dynamicAddr == staticAddr {
		cancel()
		t.Fatalf("dynamic backend reused static listener %s", dynamicAddr)
	}
	if err := srv.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Start returned %v, want nil", err)
	}
	cancel()
}

func TestSubprocessRunnerRemovesDynamicBackendWhenChildExits(t *testing.T) {
	bin := buildSubprocessHelper(t)
	t.Setenv("VIALITE_HELPER_EXIT_AFTER_READY", "1")

	opts, err := Options{
		Mode:                 ModeSubprocess,
		BinaryPath:           bin,
		AllowDynamicBackends: true,
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
	if _, err := srv.AddBackend(context.Background(), Backend{Name: "session-1", Address: "127.0.0.1:25566"}); err != nil {
		cancel()
		t.Fatalf("AddBackend: %v", err)
	}
	deadline := time.After(time.Second)
	for {
		if _, err := srv.BackendDialAddress("session-1"); errors.Is(err, ErrBackendNotFound) {
			break
		}
		select {
		case <-deadline:
			cancel()
			t.Fatal("dynamic backend remained registered after child exit")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	if err := srv.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Start returned %v, want nil", err)
	}
	cancel()
}

func TestSubprocessRunnerFailedDynamicAddCanBeRetried(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "failed-once")
	bin := buildSubprocessHelper(t)
	t.Setenv("VIALITE_HELPER_FAIL_ONCE", marker)

	opts, err := Options{
		Mode:                 ModeSubprocess,
		BinaryPath:           bin,
		AllowDynamicBackends: true,
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
	if _, err := srv.AddBackend(context.Background(), Backend{Name: "session-1", Address: "127.0.0.1:25566"}); err == nil {
		cancel()
		t.Fatal("first AddBackend succeeded, want helper failure")
	}
	addr, err := srv.AddBackend(context.Background(), Backend{Name: "session-1", Address: "127.0.0.1:25566"})
	if err != nil {
		cancel()
		t.Fatalf("retry AddBackend: %v", err)
	}
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		cancel()
		t.Fatalf("retry backend not dialable: %v", err)
	}
	_ = conn.Close()
	if err := srv.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Start returned %v, want nil", err)
	}
	cancel()
}

func TestSubprocessRunnerStopDuringDynamicAddDoesNotPanic(t *testing.T) {
	bin := buildSubprocessHelper(t)
	t.Setenv("VIALITE_HELPER_READY_DELAY_MS", "150")

	opts, err := Options{
		Mode:                 ModeSubprocess,
		BinaryPath:           bin,
		AllowDynamicBackends: true,
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
	addDone := make(chan error, 1)
	go func() {
		_, err := srv.AddBackend(context.Background(), Backend{Name: "session-1", Address: "127.0.0.1:25566"})
		addDone <- err
	}()
	time.Sleep(25 * time.Millisecond)
	if err := srv.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Start returned %v, want nil", err)
	}
	if err := <-addDone; !errors.Is(err, ErrNotStarted) {
		t.Fatalf("AddBackend after stop = %v, want ErrNotStarted", err)
	}
	cancel()
}

func TestSubprocessRunnerStaticRestartKeepsDynamicBackend(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "static-exited")
	bin := buildSubprocessHelper(t)
	t.Setenv("VIALITE_HELPER_EXIT_BACKEND_ONCE", "static")
	t.Setenv("VIALITE_HELPER_EXIT_BACKEND_MARKER", marker)

	opts, err := Options{
		Mode:       ModeSubprocess,
		BinaryPath: bin,
		RestartPolicy: &RestartPolicy{
			MinBackoff: time.Millisecond,
			MaxBackoff: time.Millisecond,
			MaxRetries: 1,
		},
		AllowDynamicBackends: true,
		Backends:             []Backend{{Name: "static", Address: "127.0.0.1:25566"}},
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
	addr, err := srv.AddBackend(context.Background(), Backend{Name: "session-1", Address: "127.0.0.1:25567"})
	if err != nil {
		cancel()
		t.Fatalf("AddBackend: %v", err)
	}
	waitForFile(t, marker)
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		cancel()
		t.Fatalf("dynamic backend not dialable after static restart: %v", err)
	}
	_ = conn.Close()
	if err := srv.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Start returned %v, want nil", err)
	}
	cancel()
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("file %s was not created", path)
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func eventuallyNotDialable(t *testing.T, addr string) {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err != nil {
			return
		}
		_ = conn.Close()
		select {
		case <-deadline:
			t.Fatalf("backend address %s is still dialable", addr)
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
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
	"net"
	"os"
	"time"
)

type nativeConfig struct {
	Bind string ` + "`json:\"bind\"`" + `
	Backends []nativeBackend ` + "`json:\"backends\"`" + `
}

type nativeBackend struct {
	Name string ` + "`json:\"name\"`" + `
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
var cfg nativeConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		os.Exit(4)
	}
	binds := []string{cfg.Bind}
	if len(cfg.Backends) > 0 {
		binds = binds[:0]
		for _, backend := range cfg.Backends {
			if backend.Bind != "" {
				binds = append(binds, backend.Bind)
			}
		}
	}
	if len(binds) == 0 {
		os.Exit(5)
	}
	if delay := os.Getenv("VIALITE_HELPER_READY_DELAY_MS"); delay != "" {
		d, err := time.ParseDuration(delay + "ms")
		if err == nil {
			time.Sleep(d)
		}
	}
	for _, bind := range binds {
		ln, err := net.Listen("tcp", bind)
		if err != nil {
			os.Exit(5)
		}
		go func() {
			for {
				conn, err := ln.Accept()
				if err != nil {
					return
				}
				_ = conn.Close()
			}
		}()
	}
	if path := os.Getenv("VIALITE_HELPER_RESTARTED"); path != "" {
		_ = os.WriteFile(path, []byte("1"), 0o644)
	}
	if target := os.Getenv("VIALITE_HELPER_EXIT_BACKEND_ONCE"); target != "" {
		marker := os.Getenv("VIALITE_HELPER_EXIT_BACKEND_MARKER")
		for _, backend := range cfg.Backends {
			if backend.Name == target {
				if marker != "" {
					if _, err := os.Stat(marker); os.IsNotExist(err) {
						_ = os.WriteFile(marker, []byte("1"), 0o644)
						time.Sleep(50 * time.Millisecond)
						return
					}
				}
			}
		}
	}
	if os.Getenv("VIALITE_HELPER_EXIT_AFTER_READY") != "" {
		time.Sleep(50 * time.Millisecond)
		return
	}
	select {}
}
`
