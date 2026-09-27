package vialite

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
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

func TestSubprocessRunnerDynamicAddHonorsCallerDeadline(t *testing.T) {
	bin := buildSubprocessHelper(t)
	t.Setenv("VIALITE_HELPER_READY_DELAY_MS", "11000")

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
	addCtx, cancelAdd := context.WithTimeout(context.Background(), 15*time.Second)
	addr, err := srv.AddBackend(addCtx, Backend{Name: "session-1", Address: "127.0.0.1:25566"})
	cancelAdd()
	if err != nil {
		cancel()
		t.Fatalf("AddBackend: %v", err)
	}
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		cancel()
		t.Fatalf("dynamic backend not dialable: %v", err)
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

// TestSubprocessRunnerStaticRestartKeepsDynamicBackend pins that a static
// runtime crash-restart does not drop a dynamic backend that was registered in
// between.
//
// The test owns the ordering instead of racing a deadline: the static runtime
// holds its one-time exit until the test creates
// VIALITE_HELPER_EXIT_RELEASE_FILE, and "which process owns the static backend"
// is read from VIALITE_HELPER_BACKEND_PIDS. Nothing here depends on how fast
// the pod schedules a fork/exec.
//
// The previous shape of this test let the runtime exit 50ms after it published
// its listener (a fixed time.Sleep inside the helper) and then raced AddBackend
// against that deadline. On a CPU-starved pod (2 CPUs) the helper's sleep
// stretched to 88ms while AddBackend plus the dynamic child's fork/exec plus a
// 10ms-granularity readiness poll took 79-89ms: AddBackend lost the race and
// failed with "vialite: server not started" (measured 2/10 and 7/20 focused
// runs, and 5/5 once the ordering was pinned by hand). A fixed sleep the test
// does not own is a flake, not a regression.
func TestSubprocessRunnerStaticRestartKeepsDynamicBackend(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "static-crashed")
	release := filepath.Join(dir, "release-static-runtime")
	pids := filepath.Join(dir, "backend-pids")
	bin := buildSubprocessHelper(t)
	t.Setenv("VIALITE_HELPER_BACKEND_PIDS", pids)
	t.Setenv("VIALITE_HELPER_EXIT_BACKEND_ONCE", "static")
	t.Setenv("VIALITE_HELPER_EXIT_BACKEND_MARKER", marker)
	t.Setenv("VIALITE_HELPER_EXIT_RELEASE_FILE", release)
	// A non-zero exit is a crash, so the restart policy owns it. A clean exit is
	// a deliberate stop; TestSubprocessRunnerStaticCleanExitStopsDynamicBackends
	// pins that half of the bookkeeping.
	t.Setenv("VIALITE_HELPER_EXIT_CODE", "7")

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
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- srv.Start(ctx) }()

	if err := srv.WaitReady(context.Background()); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
	addr, err := srv.AddBackend(context.Background(), Backend{Name: "session-1", Address: "127.0.0.1:25567"})
	if err != nil {
		t.Fatalf("AddBackend: %v", err)
	}
	staticPid := waitForRecordedPid(t, pids, "static", 0)

	// Let the static runtime crash. The runner must restart it and keep the
	// dynamic backend registered above.
	if err := os.WriteFile(release, []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if restarted := waitForRecordedPid(t, pids, "static", staticPid); restarted == staticPid {
		t.Fatalf("static backend still owned by pid %d: the runner did not restart the runtime", staticPid)
	}

	dialAddr, err := srv.BackendDialAddress("session-1")
	if err != nil {
		t.Fatalf("dynamic backend dropped by the static restart: %v", err)
	}
	if dialAddr != addr {
		t.Fatalf("dynamic backend address changed across the static restart: got %s, want %s", dialAddr, addr)
	}
	conn, err := net.DialTimeout("tcp", dialAddr, time.Second)
	if err != nil {
		t.Fatalf("dynamic backend not dialable after static restart: %v", err)
	}
	_ = conn.Close()
	waitForHealthy(t, srv)
	if err := srv.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Start returned %v, want nil", err)
	}
}

// TestSubprocessRunnerStaticCleanExitStopsDynamicBackends pins the other half of
// the same bookkeeping: a static runtime that exits 0 is a deliberate stop, not
// a crash. The runner must not restart it, must return nil, and must tear the
// whole group down - including the dynamic backends it started - so callers see
// a dead hop instead of a half-alive one.
//
// This is the behaviour the old shape of
// TestSubprocessRunnerStaticRestartKeepsDynamicBackend was accidentally racing
// against: its helper exited cleanly 50ms after readiness, so a slow AddBackend
// observed the teardown instead of the address.
func TestSubprocessRunnerStaticCleanExitStopsDynamicBackends(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "static-exited")
	release := filepath.Join(dir, "release-static-runtime")
	pids := filepath.Join(dir, "backend-pids")
	bin := buildSubprocessHelper(t)
	t.Setenv("VIALITE_HELPER_BACKEND_PIDS", pids)
	t.Setenv("VIALITE_HELPER_EXIT_BACKEND_ONCE", "static")
	t.Setenv("VIALITE_HELPER_EXIT_BACKEND_MARKER", marker)
	t.Setenv("VIALITE_HELPER_EXIT_RELEASE_FILE", release)
	// VIALITE_HELPER_EXIT_CODE stays unset: exit 0 is a deliberate stop.

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
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- srv.Start(ctx) }()

	if err := srv.WaitReady(context.Background()); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
	addr, err := srv.AddBackend(context.Background(), Backend{Name: "session-1", Address: "127.0.0.1:25567"})
	if err != nil {
		t.Fatalf("AddBackend: %v", err)
	}
	staticPid := waitForRecordedPid(t, pids, "static", 0)

	// Let the static runtime stop deliberately (exit 0).
	if err := os.WriteFile(release, []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start returned %v, want nil after a clean runtime exit", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runner did not stop after the static runtime exited cleanly")
	}
	if srv.Healthy() {
		t.Error("server reports healthy after the runtime exited cleanly")
	}
	if got := recordedPids(t, pids, "static"); len(got) != 1 || got[0] != staticPid {
		t.Errorf("static runtime was restarted after a clean exit: pids=%v, want [%d]", got, staticPid)
	}
	eventuallyNotDialable(t, addr)
	if err := srv.Stop(context.Background()); err != nil && !errors.Is(err, ErrNotStarted) {
		t.Fatalf("Stop after the runner already returned: %v", err)
	}
}

// recordedPids returns the pids the helper recorded under label in path,
// oldest first. The helper appends "<label> <pid>" lines: the backend name for
// VIALITE_HELPER_BACKEND_PIDS (after it published that backend's listener) and
// "forked" for VIALITE_HELPER_FORKED_PIDS (before it published anything).
func recordedPids(t *testing.T, path, label string) []int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var pids []int
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		owner, rawPid, ok := strings.Cut(line, " ")
		if !ok || owner != label {
			continue
		}
		// A concurrently appended line can be read half-written; skip it and let
		// the caller's next poll see the complete line.
		if pid, err := strconv.Atoi(strings.TrimSpace(rawPid)); err == nil {
			pids = append(pids, pid)
		}
	}
	return pids
}

// waitForRecordedPid waits (bounded) for the helper to record a pid under label
// that differs from exclude (0 = any) and returns it.
func waitForRecordedPid(t *testing.T, path, label string, exclude int) int {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		for _, pid := range recordedPids(t, path, label) {
			if pid != exclude {
				return pid
			}
		}
		select {
		case <-deadline:
			data, _ := os.ReadFile(path)
			t.Fatalf("no pid other than %d recorded as %s in %s (content: %q)",
				exclude, label, path, string(data))
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// TestSubprocessRunnerStopDuringRestartStopsTheRuntime is the regression guard
// for a runtime that outlived the runner.
//
// A crash sends the runner into a restart, and the restarted runtime sleeps
// before it publishes its listener (VIALITE_HELPER_READY_DELAY_MS), so the Stop
// below lands while the runner is still waiting for that listener. Before the
// fix, that path returned nil from the readiness wait without terminating the
// child it had just started: Start reported a clean stop while the runtime kept
// running and holding its loopback bind - exactly the "listener nobody owns"
// shape fail-closed startup guards against, and it left the test binary's
// stdout pipe open (go test: "Test I/O incomplete").
//
// The window is entered deterministically: the helper records every fork
// (VIALITE_HELPER_FORKED_PIDS) before it publishes anything, so the test waits
// for the restarted runtime's pid instead of sleeping.
func TestSubprocessRunnerStopDuringRestartStopsTheRuntime(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "crashed-once")
	forked := filepath.Join(dir, "forked-pids")
	bin := buildSubprocessHelper(t)
	t.Setenv("VIALITE_HELPER_FORKED_PIDS", forked)
	t.Setenv("VIALITE_HELPER_FAIL_ONCE", marker)
	t.Setenv("VIALITE_HELPER_READY_DELAY_MS", "800")

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
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- srv.Start(ctx) }()

	crashed := waitForRecordedPid(t, forked, "forked", 0)
	restarted := waitForRecordedPid(t, forked, "forked", crashed)

	if err := srv.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Start returned %v, want nil", err)
	}
	waitForProcessExit(t, restarted)
}

// waitForHealthy polls until the server reports healthy again.
//
// Poll - do not sample - when the trigger was an observation the *runtime* made:
// the helper publishes its listener and records its pid as soon as it is exec'd,
// which can happen while the runner is still inside the fork/exec of that
// attempt and has not run its own bookkeeping yet (`r.healthy.Store(true)` comes
// after `cmd.Start()` returns). Measured on the 2-CPU pod: sampling right after
// the pid marker failed 4/150 runs under load with
// `started=true ready=true healthy=false`.
func waitForHealthy(t *testing.T, srv *Server) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for !srv.Healthy() {
		select {
		case <-deadline:
			t.Fatal("server does not report healthy after the static runtime was restarted")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// waitForProcessExit asserts that pid is gone, bounded. A stopped server has
// already joined the children it started, so this returns immediately for
// correct code; a leaked runtime stays alive and trips it.
//
// The probe is unix-only (the Windows CI job cross-compiles this package but
// never runs it).
func waitForProcessExit(t *testing.T, pid int) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("process liveness probe is unix-only")
	}
	deadline := time.After(2 * time.Second)
	for processAlive(pid) {
		select {
		case <-deadline:
			t.Fatalf("runtime pid %d is still alive after the server was stopped", pid)
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// processAlive reports whether pid still exists (signal 0 probes without
// delivering anything).
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
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
	"fmt"
	"net"
	"os"
	"strconv"
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

// recordPid appends "<label> <pid>" to path so tests can observe WHICH process
// did what instead of assuming an ordering with a sleep.
func recordPid(path, label string) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	fmt.Fprintf(f, "%s %d\n", label, os.Getpid())
	_ = f.Close()
}

func main() {
	// VIALITE_HELPER_FORKED_PIDS records "<label> <pid>" ("forked <pid>") as the
	// first thing this instance does, so a test can tell that a runtime was
	// forked before it published anything - the only way to observe the window
	// where a runtime exists but is not ready yet.
	if path := os.Getenv("VIALITE_HELPER_FORKED_PIDS"); path != "" {
		recordPid(path, "forked")
	}
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
	// VIALITE_HELPER_SKIP_BIND models a runtime that stays alive but never
	// publishes its backend listener (the readiness wait must bound and name it).
	if os.Getenv("VIALITE_HELPER_SKIP_BIND") != "" {
		select {}
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
	// VIALITE_HELPER_BACKEND_PIDS records "<backend-name> <pid>" for every backend
	// this instance published a listener for, so a test can prove WHICH process
	// owns a backend (e.g. that a restart produced a new owner) instead of
	// assuming an ordering with a sleep.
	if path := os.Getenv("VIALITE_HELPER_BACKEND_PIDS"); path != "" {
		for _, backend := range cfg.Backends {
			recordPid(path, backend.Name)
		}
	}
	// VIALITE_HELPER_EXIT_BACKEND_ONCE=<name> makes the instance owning that
	// backend exit once. The exit is held until
	// VIALITE_HELPER_EXIT_RELEASE_FILE exists (bounded), so the test owns the
	// ordering; VIALITE_HELPER_EXIT_CODE selects 0 (deliberate stop) or a
	// non-zero crash the runner's restart policy owns. Default 0.
	if target := os.Getenv("VIALITE_HELPER_EXIT_BACKEND_ONCE"); target != "" {
		marker := os.Getenv("VIALITE_HELPER_EXIT_BACKEND_MARKER")
		if marker != "" {
			ownsTarget := false
			for _, backend := range cfg.Backends {
				if backend.Name == target {
					ownsTarget = true
				}
			}
			if ownsTarget {
				if _, err := os.Stat(marker); os.IsNotExist(err) {
					_ = os.WriteFile(marker, []byte("1"), 0o644)
					if release := os.Getenv("VIALITE_HELPER_EXIT_RELEASE_FILE"); release != "" {
						deadline := time.Now().Add(10 * time.Second)
						for {
							if _, err := os.Stat(release); err == nil {
								break
							}
							if time.Now().After(deadline) {
								break
							}
							time.Sleep(2 * time.Millisecond)
						}
					}
					code := 0
					if raw := os.Getenv("VIALITE_HELPER_EXIT_CODE"); raw != "" {
						if parsed, err := strconv.Atoi(raw); err == nil {
							code = parsed
						}
					}
					os.Exit(code)
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

// TestSubprocessRejectsBindOwnedByAnotherProcess is the regression guard for the
// shape measured on 2026-09-27 (kanban t_c79737e4, row f3).
//
// `via.bind` names a fixed loopback port, a foreign process already holds it, and
// the runtime therefore can never own its backend listener. Readiness used to be
// decided by dialing the configured address alone, which the foreign listener
// satisfies, so the Server reported ready and Gate started "healthy" with a
// runtime that was already gone: every join then failed with the misleading
// `vialite: not started` and the real backend was never contacted. A pinned bind
// the runtime cannot own must fail closed and name the address.
func TestSubprocessRejectsBindOwnedByAnotherProcess(t *testing.T) {
	holder, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("hold bind port: %v", err)
	}
	defer holder.Close()
	heldAddr := holder.Addr().String()
	go func() {
		for {
			conn, err := holder.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	bin := buildSubprocessHelper(t)
	opts, err := Options{
		Mode:       ModeSubprocess,
		BinaryPath: bin,
		Bind:       heldAddr, // operator pinned the port another process owns
		Backends:   []Backend{{Name: "lobby", Address: "127.0.0.1:25566"}},
	}.validate()
	if err != nil {
		t.Fatalf("validate: %v", err)
	}

	srv := &Server{opts: opts, runner: &subprocessRunner{}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- srv.Start(ctx) }()

	readyCtx, cancelReady := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancelReady()
	err = srv.WaitReady(readyCtx)
	if err == nil {
		cancel()
		<-done
		t.Fatalf("WaitReady = nil with bind %s owned by another process: a runtime that cannot "+
			"own its listener must not report ready", heldAddr)
	}
	if !strings.Contains(err.Error(), heldAddr) {
		t.Errorf("startup error %q does not name the bind address %s", err, heldAddr)
	}
	if !strings.Contains(err.Error(), "lobby") {
		t.Errorf("startup error %q does not name the backend whose listener could not be published", err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Error("Start did not return")
	}
}

// TestSubprocessReadinessNeverOutlivesTheSubprocess pins the ordering invariant of
// the readiness wait: a dialable address is not proof that *this* runtime owns the
// listener, so an already-exited subprocess must win over the dialable check.
func TestSubprocessReadinessNeverOutlivesTheSubprocess(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	done := make(chan error, 1)
	exitErr := errors.New("exit status 5")
	done <- exitErr
	backends := map[string]string{"lobby": ln.Addr().String()}

	processDone, err := waitBackendListeners(context.Background(), done, backends)
	if err == nil {
		t.Fatalf("waitBackendListeners reported ready (processDone=%t) although the subprocess had "+
			"already exited", processDone)
	}
	if !processDone {
		t.Errorf("processDone = false, want true: the exit was observed, not just dialability")
	}
	if !errors.Is(err, exitErr) {
		t.Errorf("error = %v, want the subprocess exit %v", err, exitErr)
	}
	if !errors.Is(err, errSubprocessExitedBeforeReady) && !errors.Is(err, exitErr) {
		t.Errorf("error = %v, want the subprocess-exit reason", err)
	}
}

// TestSubprocessNotReadyErrorNamesTheBind limits the "did not become ready" wait:
// when the runtime stays alive but never publishes its listener, the error must
// name the address it was supposed to publish and the stage that failed, not just
// say "not ready".
func TestSubprocessNotReadyErrorNamesTheBind(t *testing.T) {
	t.Setenv("VIALITE_HELPER_SKIP_BIND", "1")
	bin := buildSubprocessHelper(t)

	opts, err := Options{
		Mode:       ModeSubprocess,
		BinaryPath: bin,
		Bind:       "127.0.0.1:0",
		Backends:   []Backend{{Name: "lobby", Address: "127.0.0.1:25566"}},
	}.validate()
	if err != nil {
		t.Fatalf("validate: %v", err)
	}

	srv := &Server{opts: opts, runner: &subprocessRunner{}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- srv.Start(ctx) }()

	// The runner applies its own 10s readiness fallback when the Start context has
	// no deadline, so observe from outside that bound.
	readyCtx, cancelReady := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelReady()
	err = srv.WaitReady(readyCtx)
	if err == nil {
		cancel()
		t.Fatal("WaitReady = nil although the runtime never published its listener")
	}
	if !strings.Contains(err.Error(), "did not become ready") {
		t.Errorf("error = %v, want the bounded readiness failure", err)
	}
	if !strings.Contains(err.Error(), "127.0.0.1:") {
		t.Errorf("error %q does not name the address that never became ready", err)
	}
	if !strings.Contains(err.Error(), "lobby") {
		t.Errorf("error %q does not name the backend", err)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Error("Start did not return")
	}
}
