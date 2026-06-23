package vialite

import (
	"context"
	"sync"
	"sync/atomic"
	"unsafe"
)

type embeddedRunner struct {
	healthy  atomic.Bool
	backends map[string]string

	mu      sync.Mutex
	symbols *nativeSymbols
	thread  unsafe.Pointer
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
	r.mu.Lock()
	r.symbols = symbols
	r.thread = thread
	r.mu.Unlock()
	r.healthy.Store(true)
	s.markReady(nil)
	defer func() {
		r.mu.Lock()
		r.symbols = nil
		r.thread = nil
		r.backends = nil
		r.mu.Unlock()
		r.healthy.Store(false)
	}()

	return r.runNative(ctx, symbols, thread)
}

func (r *embeddedRunner) isHealthy() bool {
	return r.healthy.Load()
}

func (r *embeddedRunner) backendAddress(name string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	addr, ok := r.backends[name]
	if !ok {
		addr, ok = r.backends[backendLookupName(name)]
	}
	if !ok {
		return "", ErrBackendNotFound
	}
	return addr, nil
}

func (r *embeddedRunner) addBackend(ctx context.Context, backend Backend) (string, error) {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
	}
	data, err := nativeBackendConfigJSON(backend, "")
	if err != nil {
		return "", err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.symbols == nil || r.thread == nil || !r.healthy.Load() {
		return "", ErrNotStarted
	}
	key := backendLookupName(backend.Name)
	if _, ok := r.backends[key]; ok {
		return "", ErrDuplicateBackend
	}
	addr := r.symbols.addBackend(r.thread, string(data))
	if addr == "" {
		return "", ErrBackendNotFound
	}
	if r.backends == nil {
		r.backends = make(map[string]string)
	}
	storeBackendAddress(r.backends, backend.Name, addr)
	return addr, nil
}

func (r *embeddedRunner) removeBackend(ctx context.Context, name string) error {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.symbols == nil || r.thread == nil || !r.healthy.Load() {
		return ErrNotStarted
	}
	key := backendLookupName(name)
	if _, ok := r.backends[key]; !ok {
		return ErrBackendNotFound
	}
	if code := r.symbols.removeBackend(r.thread, name); code != 0 {
		return ErrBackendNotFound
	}
	for alias := range r.backends {
		if backendLookupName(alias) == key {
			delete(r.backends, alias)
		}
	}
	return nil
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
