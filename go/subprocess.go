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

	mu       sync.Mutex
	children map[string]*subprocessChild
}

type subprocessChild struct {
	name       string
	addr       string
	configPath string
	cmd        *exec.Cmd
	done       chan struct{}
	err        error
}

func (r *subprocessRunner) run(ctx context.Context, s *Server) error {
	bin, err := locateBinary(ctx, s.opts)
	if err != nil {
		return err
	}
	opts := s.opts
	if err := validateSubprocessBind(opts.Bind, len(opts.Backends)); err != nil {
		return err
	}
	backoff := s.opts.RestartPolicy.MinBackoff
	for attempt := 0; ; attempt++ {
		children, backends, err := r.startChildren(ctx, bin, opts)
		if err != nil {
			return err
		}
		r.mu.Lock()
		r.children = children
		r.backends = backends
		r.mu.Unlock()
		r.healthy.Store(true)

		err = waitChildListeners(ctx, children)
		if err != nil {
			r.healthy.Store(false)
			stopChildren(children, s.opts.ShutdownTimeout)
			if ctx.Err() != nil {
				return nil
			}
		} else {
			s.markReady(nil)
			err = waitAnyChildExit(ctx, children)
			r.healthy.Store(false)
			stopChildren(children, s.opts.ShutdownTimeout)
		}

		r.mu.Lock()
		r.children = nil
		r.mu.Unlock()
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

func validateSubprocessBind(bind string, backendCount int) error {
	if backendCount <= 1 {
		return nil
	}
	_, port, err := net.SplitHostPort(bind)
	if err != nil {
		return err
	}
	if port != "0" {
		return fmt.Errorf("vialite: subprocess bind %s cannot be shared by %d backends; use port 0", bind, backendCount)
	}
	return nil
}

func (r *subprocessRunner) startChildren(ctx context.Context, bin string, opts Options) (map[string]*subprocessChild, map[string]string, error) {
	children := make(map[string]*subprocessChild, len(opts.Backends))
	backends := make(map[string]string, len(opts.Backends))
	for i, backend := range opts.Backends {
		addr, err := loopbackBackendAddress(opts.Bind, i)
		if err != nil {
			stopChildren(children, opts.ShutdownTimeout)
			return nil, nil, fmt.Errorf("vialite: allocate subprocess backend %s: %w", backend.Name, err)
		}
		config, err := opts.nativeConfigJSONForBackend(backend, addr)
		if err != nil {
			stopChildren(children, opts.ShutdownTimeout)
			return nil, nil, err
		}
		configPath, err := writeTempConfig(config)
		if err != nil {
			stopChildren(children, opts.ShutdownTimeout)
			return nil, nil, err
		}
		cmd := exec.CommandContext(ctx, bin, "--config", configPath)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		child := &subprocessChild{
			name:       backend.Name,
			addr:       addr,
			configPath: configPath,
			cmd:        cmd,
			done:       make(chan struct{}),
		}
		if err := cmd.Start(); err != nil {
			_ = os.Remove(configPath)
			stopChildren(children, opts.ShutdownTimeout)
			return nil, nil, err
		}
		go func() {
			child.err = cmd.Wait()
			close(child.done)
		}()
		children[backend.Name] = child
		backends[backend.Name] = addr
	}
	return children, backends, nil
}

func waitChildListeners(ctx context.Context, children map[string]*subprocessChild) error {
	backends := make(map[string]string, len(children))
	done := make(chan error, len(children))
	for name, child := range children {
		backends[name] = child.addr
		go func(c *subprocessChild) {
			<-c.done
			if c.err != nil {
				done <- fmt.Errorf("vialite: subprocess backend %s exited: %w", c.name, c.err)
				return
			}
			done <- fmt.Errorf("vialite: subprocess backend %s exited", c.name)
		}(child)
	}
	return waitBackendListeners(ctx, done, backends)
}

func waitAnyChildExit(ctx context.Context, children map[string]*subprocessChild) error {
	done := make(chan error, len(children))
	for _, child := range children {
		go func(c *subprocessChild) {
			<-c.done
			if c.err != nil {
				done <- fmt.Errorf("vialite: subprocess backend %s exited: %w", c.name, c.err)
				return
			}
			done <- fmt.Errorf("vialite: subprocess backend %s exited", c.name)
		}(child)
	}
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return nil
	}
}

func stopChildren(children map[string]*subprocessChild, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	for _, child := range children {
		if child.cmd != nil && child.cmd.Process != nil && child.cmd.ProcessState == nil {
			_ = terminateProcess(child.cmd.Process)
		}
	}
	for _, child := range children {
		select {
		case <-child.done:
		case <-ctx.Done():
			if child.cmd != nil && child.cmd.Process != nil {
				_ = child.cmd.Process.Kill()
			}
			select {
			case <-child.done:
			case <-time.After(time.Second):
			}
		}
		_ = os.Remove(child.configPath)
	}
}

func waitBackendListeners(ctx context.Context, done <-chan error, backends map[string]string) error {
	deadline := time.After(10 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if allBackendsDialable(backends) {
			return nil
		}
		select {
		case err := <-done:
			return err
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return errors.New("vialite: subprocess backend listener did not become ready")
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
	addr, ok := r.backends[name]
	if !ok {
		return "", ErrBackendNotFound
	}
	return addr, nil
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
	addrs := make(map[string]string, len(backends))
	for i, backend := range backends {
		addr, err := loopbackBackendAddress(bind, i)
		if err != nil {
			return nil, fmt.Errorf("vialite: allocate subprocess backend %s: %w", backend.Name, err)
		}
		addrs[backend.Name] = addr
	}
	return addrs, nil
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
