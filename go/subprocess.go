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

	mu  sync.Mutex
	cmd *exec.Cmd
}

func (r *subprocessRunner) run(ctx context.Context, s *Server) error {
	bin, err := locateBinary(ctx, s.opts)
	if err != nil {
		return err
	}
	opts := s.opts
	bind, err := concreteLoopbackBind(opts.Bind)
	if err != nil {
		return err
	}
	opts.Bind = bind
	config, err := opts.nativeConfigJSON()
	if err != nil {
		return err
	}
	configPath, err := writeTempConfig(config)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(configPath) }()

	backends, err := loopbackBackendAddresses(opts.Bind, opts.Backends)
	if err != nil {
		return err
	}
	r.backends = backends

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

		err := waitBackendListeners(ctx, done, backends)
		if err != nil {
			r.healthy.Store(false)
			if ctx.Err() != nil {
				return nil
			}
			if cmd.ProcessState == nil {
				_ = terminateProcess(cmd.Process)
				<-done
			}
		} else {
			s.markReady(nil)
			err = r.waitProcess(ctx, cmd, done, s.opts.ShutdownTimeout)
			r.healthy.Store(false)
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
