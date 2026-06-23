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
}

type subprocessBackendProcess struct {
	name       string
	addr       string
	configPath string
	cmd        *exec.Cmd
	done       chan error
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

		processDone, err := waitBackendListeners(ctx, done, backends)
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
			r.stopDynamicBackends(context.Background(), s.opts.ShutdownTimeout)
		}

		if ctx.Err() != nil {
			return nil
		}
		if err == nil {
			return nil
		}
		if s.opts.RestartPolicy.MaxRetries >= 0 && attempt >= s.opts.RestartPolicy.MaxRetries {
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
	deadline := time.After(10 * time.Second)
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
		case <-ctx.Done():
			return false, ctx.Err()
		case <-deadline:
			return false, errors.New("vialite: subprocess backend listener did not become ready")
		case <-ticker.C:
		}
	}
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
	defer r.mu.Unlock()
	if !r.healthy.Load() || r.bin == "" {
		return "", ErrNotStarted
	}
	key := backendLookupName(backend.Name)
	if _, ok := r.backends[key]; ok {
		return "", fmt.Errorf("%w: %s", ErrDuplicateBackend, backend.Name)
	}
	addr, err := loopbackBackendAddress(r.opts.Bind, 0)
	if err != nil {
		return "", fmt.Errorf("vialite: allocate subprocess backend %s: %w", backend.Name, err)
	}
	binds := map[string]string{backend.Name: addr, key: addr}
	opts := r.opts
	opts.Backends = []Backend{backend}
	config, err := opts.nativeConfigJSONWithBackendBinds(binds)
	if err != nil {
		return "", err
	}
	configPath, err := writeTempConfig(config)
	if err != nil {
		return "", err
	}

	cmd := exec.Command(r.bin, "--config", configPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		_ = os.Remove(configPath)
		return "", err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	if processDone, err := waitBackendListeners(ctx, done, map[string]string{key: addr}); err != nil {
		if !processDone {
			_ = terminateProcess(cmd.Process)
			<-done
		}
		_ = os.Remove(configPath)
		return "", err
	}

	if r.dynamic == nil {
		r.dynamic = make(map[string]*subprocessBackendProcess)
	}
	r.dynamic[key] = &subprocessBackendProcess{
		name:       backend.Name,
		addr:       addr,
		configPath: configPath,
		cmd:        cmd,
		done:       done,
	}
	storeBackendAddress(r.backends, backend.Name, addr)
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
	processes := make([]*subprocessBackendProcess, 0, len(r.dynamic))
	for key, proc := range r.dynamic {
		processes = append(processes, proc)
		delete(r.dynamic, key)
		delete(r.backends, proc.name)
		delete(r.backends, backendLookupName(proc.name))
	}
	r.mu.Unlock()
	for _, proc := range processes {
		_ = stopSubprocessBackend(ctx, proc, timeout)
	}
}

func stopSubprocessBackend(ctx context.Context, proc *subprocessBackendProcess, shutdownTimeout time.Duration) error {
	defer func() { _ = os.Remove(proc.configPath) }()
	select {
	case err := <-proc.done:
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
	case err := <-proc.done:
		if isExpectedProcessExit(err) {
			return nil
		}
		return err
	case <-ctx.Done():
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
