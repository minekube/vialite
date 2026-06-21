package gate

import (
	"context"
	"errors"
	"testing"

	vialite "go.minekube.com/vialite"
)

func TestNewDisabled(t *testing.T) {
	got, err := New(Config{}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got != nil {
		t.Fatalf("New disabled = %#v, want nil", got)
	}
}

func TestViaLifecycle(t *testing.T) {
	oldNewSrv := newSrv
	defer func() { newSrv = oldNewSrv }()
	fake := &fakeServer{backend: "127.0.0.1:40000"}
	newSrv = func(vialite.Options) (server, error) { return fake, nil }

	via, err := New(Config{
		Enabled: true,
		Backends: []BackendConfig{{
			Name:    "lobby",
			Address: "127.0.0.1:25566",
		}},
	}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if via == nil {
		t.Fatal("Via is nil")
	}
	if err := via.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !via.Healthy() {
		t.Fatal("Healthy = false")
	}
	addr, err := via.BackendDialAddress("lobby")
	if err != nil {
		t.Fatalf("BackendDialAddress: %v", err)
	}
	if addr != "127.0.0.1:40000" {
		t.Fatalf("BackendDialAddress = %q", addr)
	}
	if err := via.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if !fake.stopped {
		t.Fatal("fake server was not stopped")
	}
}

func TestStartTreatsContextCancellationAsClean(t *testing.T) {
	oldNewSrv := newSrv
	defer func() { newSrv = oldNewSrv }()
	newSrv = func(vialite.Options) (server, error) {
		return &fakeServer{startErr: context.Canceled}, nil
	}

	via, err := New(Config{Enabled: true, Backends: []BackendConfig{{Name: "lobby", Address: "127.0.0.1:25566"}}}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := via.Start(context.Background()); err != nil {
		t.Fatalf("Start canceled = %v, want nil", err)
	}
}

func TestBackendDialAddressNil(t *testing.T) {
	var via *Via
	_, err := via.BackendDialAddress("lobby")
	if !errors.Is(err, vialite.ErrNotStarted) {
		t.Fatalf("BackendDialAddress nil = %v", err)
	}
}

type fakeServer struct {
	startErr error
	stopped  bool
	healthy  bool
	backend  string
}

func (f *fakeServer) Start(context.Context) error {
	f.healthy = true
	return f.startErr
}

func (f *fakeServer) Stop(context.Context) error {
	f.stopped = true
	f.healthy = false
	return nil
}

func (f *fakeServer) Healthy() bool { return f.healthy }

func (f *fakeServer) BackendDialAddress(string) (string, error) {
	if f.backend == "" {
		return "", vialite.ErrBackendNotFound
	}
	return f.backend, nil
}
