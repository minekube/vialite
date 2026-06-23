package vialite

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

type Server struct {
	opts   Options
	runner runner

	mu          sync.Mutex
	started     atomic.Bool
	ready       atomic.Bool
	cancel      context.CancelFunc
	done        chan struct{}
	readyCh     chan struct{}
	readyClosed bool
	readyErr    error
	runErr      atomic.Pointer[error]
}

type runner interface {
	run(ctx context.Context, s *Server) error
	isHealthy() bool
	backendAddress(name string) (string, error)
	addBackend(ctx context.Context, backend Backend) (string, error)
	removeBackend(ctx context.Context, name string) error
}

var (
	newEmbeddedRunner   = func(Options) runner { return &embeddedRunner{} }
	newSubprocessRunner = func(Options) runner {
		return &subprocessRunner{}
	}
)

func New(opts Options) (*Server, error) {
	validated, err := opts.validate()
	if err != nil {
		return nil, err
	}
	s := &Server{opts: validated}
	switch validated.Mode {
	case ModeEmbedded:
		s.runner = newEmbeddedRunner(validated)
	case ModeSubprocess:
		s.runner = newSubprocessRunner(validated)
	default:
		return nil, ErrInvalidMode
	}
	return s, nil
}

func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.started.Load() {
		s.mu.Unlock()
		return ErrAlreadyStarted
	}
	s.started.Store(true)
	s.ready.Store(false)
	s.runErr.Store(nil)
	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.done = make(chan struct{})
	s.readyCh = make(chan struct{})
	s.readyClosed = false
	s.readyErr = nil
	s.mu.Unlock()

	defer close(s.done)
	defer cancel()
	err := s.runner.run(runCtx, s)
	s.markReady(err)
	s.runErr.Store(&err)
	s.started.Store(false)
	s.ready.Store(false)
	return err
}

// WaitReady blocks until the native runtime has published backend dial
// addresses or startup fails.
func (s *Server) WaitReady(ctx context.Context) error {
	if s == nil {
		return ErrNotStarted
	}
	for {
		s.mu.Lock()
		readyCh := s.readyCh
		if readyCh != nil {
			s.mu.Unlock()
			select {
			case <-readyCh:
				s.mu.Lock()
				err := s.readyErr
				s.mu.Unlock()
				return err
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if errp := s.runErr.Load(); errp != nil && *errp != nil {
			err := *errp
			s.mu.Unlock()
			return err
		}
		s.mu.Unlock()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Millisecond):
		}
	}
}

func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	if !s.started.Load() {
		s.mu.Unlock()
		return ErrNotStarted
	}
	cancel := s.cancel
	done := s.done
	s.mu.Unlock()

	cancel()
	select {
	case <-done:
		if errp := s.runErr.Load(); errp != nil {
			if errors.Is(*errp, context.Canceled) {
				return nil
			}
			return *errp
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Server) Healthy() bool {
	if s == nil || !s.started.Load() || !s.ready.Load() {
		return false
	}
	return s.runner.isHealthy()
}

func (s *Server) BackendDialAddress(name string) (string, error) {
	if s == nil || !s.started.Load() {
		return "", ErrNotStarted
	}
	if !s.ready.Load() {
		return "", ErrNotReady
	}
	addr, err := s.runner.backendAddress(name)
	if errors.Is(err, ErrBackendNotFound) {
		normalized := backendLookupName(name)
		if normalized != name {
			addr, err = s.runner.backendAddress(normalized)
		}
	}
	if errors.Is(err, ErrBackendNotFound) {
		return "", err
	}
	if err != nil {
		return "", err
	}
	return addr, nil
}

// AddBackend starts translating a backend registered after server startup and
// returns the loopback address Gate should dial.
func (s *Server) AddBackend(ctx context.Context, backend Backend) (string, error) {
	if s == nil || !s.started.Load() {
		return "", ErrNotStarted
	}
	if !s.ready.Load() {
		return "", ErrNotReady
	}
	backend, err := normalizeBackend(backend)
	if err != nil {
		return "", err
	}
	return s.runner.addBackend(ctx, backend)
}

// RemoveBackend stops translating a backend registered with AddBackend.
func (s *Server) RemoveBackend(ctx context.Context, name string) error {
	if s == nil || !s.started.Load() {
		return ErrNotStarted
	}
	if !s.ready.Load() {
		return ErrNotReady
	}
	return s.runner.removeBackend(ctx, name)
}

func (s *Server) markReady(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.readyCh == nil || s.readyClosed {
		return
	}
	s.readyErr = err
	if err == nil {
		s.ready.Store(true)
	}
	close(s.readyCh)
	s.readyClosed = true
}
