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
	fr := &fakeRunner{started: make(chan struct{}), backends: map[string]string{"lobby": "127.0.0.1:40000"}}
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

	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Start returned %v, want context.Canceled", err)
	}
	if srv.Healthy() {
		t.Fatal("Healthy = true after stop")
	}
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
	if _, err := srv.BackendDialAddress("missing"); !errors.Is(err, ErrBackendNotFound) {
		t.Fatalf("BackendDialAddress missing = %v", err)
	}
	cancel()
}

type fakeRunner struct {
	started  chan struct{}
	healthy  bool
	backends map[string]string
}

func (f *fakeRunner) run(ctx context.Context, _ *Server) error {
	if f.started == nil {
		f.started = make(chan struct{})
	}
	f.healthy = true
	close(f.started)
	<-ctx.Done()
	f.healthy = false
	return ctx.Err()
}

func (f *fakeRunner) isHealthy() bool { return f.healthy }

func (f *fakeRunner) backendAddress(name string) (string, error) {
	addr, ok := f.backends[name]
	if !ok {
		return "", ErrBackendNotFound
	}
	return addr, nil
}
