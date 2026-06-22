package vialite

import (
	"context"
	"sync/atomic"
	"unsafe"
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
	var isolate unsafe.Pointer
	var thread unsafe.Pointer
	if code := symbols.createIsolate(nil, &isolate, &thread); code != 0 {
		return nativeExitError{op: "create_isolate", code: code}
	}
	defer func() { _ = symbols.tearDownIsolate(thread) }()
	config, err := s.opts.nativeConfigJSON()
	if err != nil {
		return err
	}
	if code := symbols.init(thread, string(config)); code != 0 {
		return nativeExitError{op: "init", code: code}
	}
	r.backends = make(map[string]string, len(s.opts.Backends)*2)
	for _, backend := range s.opts.Backends {
		addr := symbols.backendAddress(thread, backend.Name)
		if addr == "" {
			return ErrBackendNotFound
		}
		storeBackendAddress(r.backends, backend.Name, addr)
	}
	r.healthy.Store(true)
	s.markReady(nil)
	defer r.healthy.Store(false)

	return r.runNative(ctx, symbols, thread)
}

func (r *embeddedRunner) isHealthy() bool {
	return r.healthy.Load()
}

func (r *embeddedRunner) backendAddress(name string) (string, error) {
	addr, ok := r.backends[name]
	if !ok {
		addr, ok = r.backends[backendLookupName(name)]
	}
	if !ok {
		return "", ErrBackendNotFound
	}
	return addr, nil
}

func (r *embeddedRunner) runNative(ctx context.Context, symbols *nativeSymbols, thread unsafe.Pointer) error {
	done := make(chan error, 1)
	go func() {
		if code := symbols.run(thread); code != 0 {
			done <- nativeExitError{op: "run", code: code}
			return
		}
		done <- nil
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		_ = symbols.shutdown(thread)
		return <-done
	}
}
