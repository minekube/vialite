package vialite

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
)

type Server struct {
	opts   Options
	runner runner

	mu      sync.Mutex
	started atomic.Bool
	cancel  context.CancelFunc
	done    chan struct{}
	runErr  atomic.Pointer[error]
}

type runner interface {
	run(ctx context.Context, s *Server) error
	isHealthy() bool
	backendAddress(name string) (string, error)
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
	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.done = make(chan struct{})
	s.mu.Unlock()

	defer close(s.done)
	defer cancel()
	err := s.runner.run(runCtx, s)
	s.runErr.Store(&err)
	s.started.Store(false)
	return err
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
			return *errp
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Server) Healthy() bool {
	if s == nil || !s.started.Load() {
		return false
	}
	return s.runner.isHealthy()
}

func (s *Server) BackendDialAddress(name string) (string, error) {
	if s == nil || !s.started.Load() {
		return "", ErrNotStarted
	}
	addr, err := s.runner.backendAddress(name)
	if errors.Is(err, ErrBackendNotFound) {
		return "", err
	}
	if err != nil {
		return "", err
	}
	return addr, nil
}
