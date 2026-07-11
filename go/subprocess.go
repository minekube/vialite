package vialite

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

type subprocessRunner struct {
	healthy  atomic.Bool
	backends map[string]string

	mu      sync.Mutex
	cmd     *exec.Cmd
	bin     string
	opts    Options
	dynamic map[string]*subprocessBackendProcess
	adding  map[string]struct{}
	pending map[string]*subprocessBackendProcess
}

type subprocessBackendProcess struct {
	name       string
	addr       string
	configPath string
	cmd        *exec.Cmd
	done       chan struct{}
	waitMu     sync.Mutex
	waitErr    error
}

func (p *subprocessBackendProcess) setWaitErr(err error) {
	p.waitMu.Lock()
	p.waitErr = err
	p.waitMu.Unlock()
}

func (p *subprocessBackendProcess) err() error {
	p.waitMu.Lock()
	defer p.waitMu.Unlock()
	return p.waitErr
}

func (r *subprocessRunner) run(ctx context.Context, s *Server) error {
	bin, err := locateBinary(ctx, s.opts)
	if err != nil {
		return err
	}
	opts := s.opts
	r.mu.Lock()
	r.bin = bin
	r.opts = opts
	if r.backends == nil {
		r.backends = make(map[string]string)
	}
	if r.dynamic == nil {
		r.dynamic = make(map[string]*subprocessBackendProcess)
	}
	if r.adding == nil {
		r.adding = make(map[string]struct{})
	}
	if r.pending == nil {
		r.pending = make(map[string]*subprocessBackendProcess)
	}
	r.mu.Unlock()

	if len(opts.Backends) == 0 && opts.AllowDynamicBackends {
		r.healthy.Store(true)
		s.markReady(nil)
		<-ctx.Done()
		r.healthy.Store(false)
		r.stopDynamicBackends(context.Background(), opts.ShutdownTimeout)
		return nil
	}

	backends, err := loopbackBackendAddresses(opts.Bind, opts.Backends)
	if err != nil {
		return err
	}
	config, err := opts.nativeConfigJSONWithBackendBinds(backends)
	if err != nil {
		return err
	}
	configPath, err := writeTempConfig(config)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(configPath) }()

	r.mu.Lock()
	r.backends = backends
	r.mu.Unlock()
	staticBackends := cloneBackendAddresses(backends)

	backoff := s.opts.RestartPolicy.MinBackoff
	for attempt := 0; ; attempt++ {
		cmd := exec.Command(bin, "--config", configPath)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		r.mu.Lock()
		r.cmd = cmd
		r.mu.Unlock()
		if err := cmd.Start(); err != nil {
			return err
		}
		r.healthy.Store(true)
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()

		processDone, err := waitBackendListeners(ctx, done, staticBackends)
		if err != nil {
			r.healthy.Store(false)
			if ctx.Err() != nil {
				return nil
			}
			if !processDone {
				_ = terminateProcess(cmd.Process)
				<-done
			}
		} else {
			s.markReady(nil)
			err = r.waitProcess(ctx, cmd, done, s.opts.ShutdownTimeout)
			r.healthy.Store(false)
			if ctx.Err() != nil || err == nil {
				r.stopDynamicBackends(context.Background(), s.opts.ShutdownTimeout)
			}
		}

		if ctx.Err() != nil {
			return nil
		}
		if err == nil {
			return nil
		}
		if s.opts.RestartPolicy.MaxRetries >= 0 && attempt >= s.opts.RestartPolicy.MaxRetries {
			r.stopDynamicBackends(context.Background(), s.opts.ShutdownTimeout)
			return err
		}
		if sleepContext(ctx, backoff) != nil {
			return nil
		}
		backoff *= 2
		if backoff > s.opts.RestartPolicy.MaxBackoff {
			backoff = s.opts.RestartPolicy.MaxBackoff
		}
	}
}

func (r *subprocessRunner) waitProcess(ctx context.Context, cmd *exec.Cmd, done <-chan error, shutdownTimeout time.Duration) error {
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		if err := terminateProcess(cmd.Process); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return err
		}
		select {
		case err := <-done:
			if ctx.Err() != nil {
				return nil
			}
			return err
		case <-time.After(shutdownTimeout):
			if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
				return err
			}
			<-done
			return nil
		}
	}
}

func waitBackendListeners(ctx context.Context, done <-chan error, backends map[string]string) (bool, error) {
	waitCtx, cancel := backendListenerReadyContext(ctx)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if allBackendsDialable(backends) {
			return false, nil
		}
		select {
		case err := <-done:
			if err == nil {
				err = errors.New("vialite: subprocess exited before backend listener became ready")
			}
			return true, err
		case <-waitCtx.Done():
			if ctx.Err() != nil {
				return false, ctx.Err()
			}
			return false, errors.New("vialite: subprocess backend listener did not become ready")
		case <-ticker.C:
		}
	}
}

func waitBackendListenersProcess(ctx context.Context, proc *subprocessBackendProcess, backends map[string]string) (bool, error) {
	waitCtx, cancel := backendListenerReadyContext(ctx)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if allBackendsDialable(backends) {
			return false, nil
		}
		select {
		case <-proc.done:
			err := proc.err()
			if err == nil {
				err = errors.New("vialite: subprocess exited before backend listener became ready")
			}
			return true, err
		case <-waitCtx.Done():
			if ctx.Err() != nil {
				return false, ctx.Err()
			}
			return false, errors.New("vialite: subprocess backend listener did not become ready")
		case <-ticker.C:
		}
	}
}

func backendListenerReadyContext(ctx context.Context) (context.Context, context.CancelFunc) {
	const fallbackTimeout = 10 * time.Second
	if ctx == nil {
		return context.WithTimeout(context.Background(), fallbackTimeout)
	}
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, fallbackTimeout)
}

func allBackendsDialable(backends map[string]string) bool {
	for _, addr := range backends {
		conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err != nil {
			return false
		}
		_ = conn.Close()
	}
	return true
}

func (r *subprocessRunner) isHealthy() bool {
	return r.healthy.Load()
}

func (r *subprocessRunner) backendAddress(name string) (string, error) {
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

func (r *subprocessRunner) addBackend(ctx context.Context, backend Backend) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.Lock()
	if !r.healthy.Load() || r.bin == "" {
		r.mu.Unlock()
		return "", ErrNotStarted
	}
	key := backendLookupName(backend.Name)
	if _, ok := r.backends[key]; ok {
		r.mu.Unlock()
		return "", fmt.Errorf("%w: %s", ErrDuplicateBackend, backend.Name)
	}
	if _, ok := r.dynamic[key]; ok {
		r.mu.Unlock()
		return "", fmt.Errorf("%w: %s", ErrDuplicateBackend, backend.Name)
	}
	if _, ok := r.adding[key]; ok {
		r.mu.Unlock()
		return "", fmt.Errorf("%w: %s", ErrDuplicateBackend, backend.Name)
	}
	if _, ok := r.pending[key]; ok {
		r.mu.Unlock()
		return "", fmt.Errorf("%w: %s", ErrDuplicateBackend, backend.Name)
	}
	bin := r.bin
	opts := r.opts
	if r.adding == nil {
		r.adding = make(map[string]struct{})
	}
	r.adding[key] = struct{}{}
	if r.pending == nil {
		r.pending = make(map[string]*subprocessBackendProcess)
	}
	r.mu.Unlock()

	cleanupAdding := func() {
		r.mu.Lock()
		delete(r.adding, key)
		r.mu.Unlock()
	}

	addr, err := dynamicLoopbackBackendAddress(opts.Bind)
	if err != nil {
		cleanupAdding()
		return "", fmt.Errorf("vialite: allocate subprocess backend %s: %w", backend.Name, err)
	}
	binds := map[string]string{backend.Name: addr, key: addr}
	opts.Backends = []Backend{backend}
	config, err := opts.nativeConfigJSONWithBackendBinds(binds)
	if err != nil {
		cleanupAdding()
		return "", err
	}
	configPath, err := writeTempConfig(config)
	if err != nil {
		cleanupAdding()
		return "", err
	}

	cmd := exec.Command(bin, "--config", configPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	done := make(chan error, 1)

	r.mu.Lock()
	if !r.healthy.Load() || r.bin == "" {
		delete(r.adding, key)
		r.mu.Unlock()
		_ = os.Remove(configPath)
		return "", ErrNotStarted
	}
	if err := cmd.Start(); err != nil {
		delete(r.adding, key)
		r.mu.Unlock()
		cleanupAdding()
		_ = os.Remove(configPath)
		return "", err
	}
	go func() { done <- cmd.Wait() }()
	proc := newSubprocessBackendProcess(backend.Name, addr, configPath, cmd, done)
	delete(r.adding, key)
	r.pending[key] = proc
	r.mu.Unlock()

	if _, err := waitBackendListenersProcess(ctx, proc, map[string]string{key: addr}); err != nil {
		_ = stopSubprocessBackend(context.Background(), proc, opts.ShutdownTimeout)
		r.mu.Lock()
		if r.pending[key] == proc {
			delete(r.pending, key)
		}
		stopped := !r.healthy.Load() || r.bin == ""
		r.mu.Unlock()
		if stopped {
			return "", ErrNotStarted
		}
		return "", err
	}

	r.mu.Lock()
	if !r.healthy.Load() || r.bin == "" {
		if r.pending[key] == proc {
			delete(r.pending, key)
		}
		r.mu.Unlock()
		_ = stopSubprocessBackend(context.Background(), proc, opts.ShutdownTimeout)
		return "", ErrNotStarted
	}
	if r.pending[key] != proc {
		r.mu.Unlock()
		_ = stopSubprocessBackend(context.Background(), proc, opts.ShutdownTimeout)
		return "", ErrNotStarted
	}
	delete(r.pending, key)
	if r.dynamic == nil {
		r.dynamic = make(map[string]*subprocessBackendProcess)
	}
	r.dynamic[key] = proc
	storeBackendAddress(r.backends, backend.Name, addr)
	r.mu.Unlock()
	go func() {
		<-proc.done
		r.dynamicBackendExited(key, proc)
	}()
	return addr, nil
}

func (r *subprocessRunner) removeBackend(ctx context.Context, name string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	key := backendLookupName(name)
	r.mu.Lock()
	proc, ok := r.dynamic[key]
	if !ok {
		r.mu.Unlock()
		if _, err := r.backendAddress(name); err == nil {
			return ErrDynamicBackendsUnsupported
		}
		return ErrBackendNotFound
	}
	delete(r.dynamic, key)
	delete(r.backends, proc.name)
	delete(r.backends, backendLookupName(proc.name))
	r.mu.Unlock()

	return stopSubprocessBackend(ctx, proc, r.opts.ShutdownTimeout)
}

func (r *subprocessRunner) stopDynamicBackends(ctx context.Context, timeout time.Duration) {
	r.mu.Lock()
	processes := make([]*subprocessBackendProcess, 0, len(r.dynamic)+len(r.pending))
	for key, proc := range r.dynamic {
		processes = append(processes, proc)
		delete(r.dynamic, key)
		delete(r.backends, proc.name)
		delete(r.backends, backendLookupName(proc.name))
	}
	for key, proc := range r.pending {
		processes = append(processes, proc)
		delete(r.pending, key)
	}
	for key := range r.adding {
		delete(r.adding, key)
	}
	r.mu.Unlock()
	for _, proc := range processes {
		_ = stopSubprocessBackend(ctx, proc, timeout)
	}
}

func (r *subprocessRunner) dynamicBackendExited(key string, proc *subprocessBackendProcess) {
	r.mu.Lock()
	if r.dynamic[key] == proc {
		delete(r.dynamic, key)
		delete(r.backends, proc.name)
		delete(r.backends, backendLookupName(proc.name))
	}
	if r.pending[key] == proc {
		delete(r.pending, key)
	}
	r.mu.Unlock()
	_ = os.Remove(proc.configPath)
}

func newSubprocessBackendProcess(name, addr, configPath string, cmd *exec.Cmd, wait <-chan error) *subprocessBackendProcess {
	proc := &subprocessBackendProcess{
		name:       name,
		addr:       addr,
		configPath: configPath,
		cmd:        cmd,
		done:       make(chan struct{}),
	}
	go func() {
		err := <-wait
		proc.setWaitErr(err)
		close(proc.done)
	}()
	return proc
}

func stopSubprocessBackend(ctx context.Context, proc *subprocessBackendProcess, shutdownTimeout time.Duration) error {
	defer func() { _ = os.Remove(proc.configPath) }()
	select {
	case <-proc.done:
		err := proc.err()
		if isExpectedProcessExit(err) {
			return nil
		}
		return err
	default:
	}
	if err := terminateProcess(proc.cmd.Process); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	select {
	case <-proc.done:
		err := proc.err()
		if isExpectedProcessExit(err) {
			return nil
		}
		return err
	case <-ctx.Done():
		if err := proc.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return err
		}
		<-proc.done
		return ctx.Err()
	case <-time.After(shutdownTimeout):
		if err := proc.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return err
		}
		<-proc.done
		return nil
	}
}

func isExpectedProcessExit(err error) bool {
	if err == nil {
		return true
	}
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr)
}

func writeTempConfig(data []byte) (string, error) {
	f, err := os.CreateTemp("", "vialite-*.json")
	if err != nil {
		return "", err
	}
	path := f.Name()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

func loopbackBackendAddresses(bind string, backends []Backend) (map[string]string, error) {
	if bind == "" {
		bind = "127.0.0.1:0"
	}
	if len(backends) > 1 {
		_, port, err := net.SplitHostPort(bind)
		if err != nil {
			return nil, err
		}
		if port != "0" {
			return nil, fmt.Errorf("vialite: subprocess bind %s cannot be shared by %d backends", bind, len(backends))
		}
	}
	addrs := make(map[string]string, len(backends)*2)
	for i, backend := range backends {
		addr, err := loopbackBackendAddress(bind, i)
		if err != nil {
			return nil, fmt.Errorf("vialite: allocate subprocess backend %s: %w", backend.Name, err)
		}
		storeBackendAddress(addrs, backend.Name, addr)
	}
	return addrs, nil
}

func storeBackendAddress(addrs map[string]string, name string, addr string) {
	addrs[name] = addr
	addrs[backendLookupName(name)] = addr
}

func cloneBackendAddresses(addrs map[string]string) map[string]string {
	clone := make(map[string]string, len(addrs))
	for name, addr := range addrs {
		clone[name] = addr
	}
	return clone
}

func concreteLoopbackBind(bind string) (string, error) {
	if bind == "" {
		bind = "127.0.0.1:0"
	}
	host, port, err := net.SplitHostPort(bind)
	if err != nil {
		return "", err
	}
	if port != "0" {
		return bind, nil
	}
	if host == "" {
		host = "127.0.0.1"
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		return "", err
	}
	addr := ln.Addr().String()
	return addr, ln.Close()
}

func loopbackBackendAddress(bind string, index int) (string, error) {
	return concreteLoopbackBind(bind)
}

func dynamicLoopbackBackendAddress(bind string) (string, error) {
	if bind == "" {
		bind = "127.0.0.1:0"
	}
	host, _, err := net.SplitHostPort(bind)
	if err != nil {
		return "", err
	}
	if host == "" {
		host = "127.0.0.1"
	}
	return concreteLoopbackBind(net.JoinHostPort(host, "0"))
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
