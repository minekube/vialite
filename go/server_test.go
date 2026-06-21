package vialite

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNewSelectsEmbeddedRunner(t *testing.T) {
	oldEmbedded := newEmbeddedRunner
	defer func() { newEmbeddedRunner = oldEmbedded }()
	called := false
	newEmbeddedRunner = func(Options) runner {
		called = true
		return &fakeRunner{started: make(chan struct{}), backends: map[string]string{"lobby": "127.0.0.1:40000"}}
	}

	srv, err := New(Options{Backends: []Backend{{Name: "lobby", Address: "127.0.0.1:25566"}}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if srv == nil || !called {
		t.Fatal("embedded runner was not selected")
	}
}

func TestServerLifecycle(t *testing.T) {
	fr := &fakeRunner{
		started:  make(chan struct{}),
		ready:    make(chan struct{}),
		backends: map[string]string{"lobby": "127.0.0.1:40000"},
	}
	srv := &Server{runner: fr, opts: Options{Backends: []Backend{{Name: "lobby", Address: "127.0.0.1:25566"}}}}

	if err := srv.Stop(context.Background()); !errors.Is(err, ErrNotStarted) {
		t.Fatalf("Stop before start = %v, want ErrNotStarted", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Start(ctx) }()

	select {
	case <-fr.started:
	case <-time.After(time.Second):
		t.Fatal("runner did not start")
	}
	if _, err := srv.BackendDialAddress("lobby"); !errors.Is(err, ErrNotReady) {
		t.Fatalf("BackendDialAddress before ready = %v, want ErrNotReady", err)
	}
	close(fr.ready)
	if err := srv.WaitReady(context.Background()); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
	if !srv.Healthy() {
		t.Fatal("Healthy = false after start")
	}
	if err := srv.Start(context.Background()); !errors.Is(err, ErrAlreadyStarted) {
		t.Fatalf("second Start = %v, want ErrAlreadyStarted", err)
	}

	got, err := srv.BackendDialAddress("lobby")
	if err != nil {
		t.Fatalf("BackendDialAddress: %v", err)
	}
	if got != "127.0.0.1:40000" {
		t.Fatalf("BackendDialAddress = %q", got)
	}

	if err := srv.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Start returned %v, want nil", err)
	}
	if srv.Healthy() {
		t.Fatal("Healthy = true after stop")
	}
	cancel()
}

func TestBackendDialAddressErrors(t *testing.T) {
	srv := &Server{runner: &fakeRunner{started: make(chan struct{}), backends: map[string]string{}}, opts: Options{}}
	if _, err := srv.BackendDialAddress("missing"); !errors.Is(err, ErrNotStarted) {
		t.Fatalf("BackendDialAddress before start = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	fr := srv.runner.(*fakeRunner)
	go func() { _ = srv.Start(ctx) }()
	<-fr.started
	if err := srv.WaitReady(context.Background()); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
	if _, err := srv.BackendDialAddress("missing"); !errors.Is(err, ErrBackendNotFound) {
		t.Fatalf("BackendDialAddress missing = %v", err)
	}
	cancel()
}

func TestWaitReadyCanBeCalledBeforeStartGoroutineRuns(t *testing.T) {
	fr := &fakeRunner{started: make(chan struct{}), backends: map[string]string{"lobby": "127.0.0.1:40000"}}
	srv := &Server{runner: fr, opts: Options{}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ready := make(chan error, 1)
	go func() { ready <- srv.WaitReady(ctx) }()
	go func() {
		time.Sleep(25 * time.Millisecond)
		_ = srv.Start(ctx)
	}()

	select {
	case err := <-ready:
		if err != nil {
			t.Fatalf("WaitReady: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("WaitReady did not observe start readiness")
	}
}

func TestServerWaitReadyReturnsStartupError(t *testing.T) {
	startErr := errors.New("boom")
	srv := &Server{runner: &fakeRunner{started: make(chan struct{}), err: startErr}, opts: Options{}}
	done := make(chan error, 1)
	go func() { done <- srv.Start(context.Background()) }()
	<-srv.runner.(*fakeRunner).started

	if err := srv.WaitReady(context.Background()); !errors.Is(err, startErr) {
		t.Fatalf("WaitReady = %v, want %v", err, startErr)
	}
	if err := <-done; !errors.Is(err, startErr) {
		t.Fatalf("Start = %v, want %v", err, startErr)
	}
}

type fakeRunner struct {
	started  chan struct{}
	ready    chan struct{}
	healthy  bool
	backends map[string]string
	err      error
}

func (f *fakeRunner) run(ctx context.Context, s *Server) error {
	if f.started == nil {
		f.started = make(chan struct{})
	}
	if f.err != nil {
		close(f.started)
		return f.err
	}
	f.healthy = true
	close(f.started)
	if f.ready != nil {
		<-f.ready
	}
	s.markReady(nil)
	<-ctx.Done()
	f.healthy = false
	return nil
}

func (f *fakeRunner) isHealthy() bool { return f.healthy }

func (f *fakeRunner) backendAddress(name string) (string, error) {
	addr, ok := f.backends[name]
	if !ok {
		return "", ErrBackendNotFound
	}
	return addr, nil
}
