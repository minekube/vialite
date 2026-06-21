package vialite

import (
	"context"
	"sync/atomic"
)

type embeddedRunner struct {
	healthy  atomic.Bool
	backends map[string]string
}

func (r *embeddedRunner) run(ctx context.Context, s *Server) error {
	lib, err := locateLibrary(ctx, s.opts)
	if err != nil {
		return err
	}
	symbols, err := loadNativeSymbols(lib)
	if err != nil {
		return err
	}
	config, err := s.opts.nativeConfigJSON()
	if err != nil {
		return err
	}
	if code := symbols.init(string(config)); code != 0 {
		return nativeExitError{op: "init", code: code}
	}
	r.backends = make(map[string]string, len(s.opts.Backends))
	for _, backend := range s.opts.Backends {
		addr := symbols.backendAddress(backend.Name)
		if addr == "" {
			return ErrBackendNotFound
		}
		r.backends[backend.Name] = addr
	}
	r.healthy.Store(true)
	defer r.healthy.Store(false)

	done := make(chan error, 1)
	go func() {
		if code := symbols.run(); code != 0 {
			done <- nativeExitError{op: "run", code: code}
			return
		}
		done <- nil
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		_ = symbols.shutdown()
		return ctx.Err()
	}
}

func (r *embeddedRunner) isHealthy() bool {
	return r.healthy.Load()
}

func (r *embeddedRunner) backendAddress(name string) (string, error) {
	addr, ok := r.backends[name]
	if !ok {
		return "", ErrBackendNotFound
	}
	return addr, nil
}
