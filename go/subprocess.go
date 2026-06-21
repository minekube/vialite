package vialite

import (
	"context"
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
	config, err := s.opts.nativeConfigJSON()
	if err != nil {
		return err
	}
	configPath, err := writeTempConfig(config)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(configPath) }()

	r.backends = make(map[string]string, len(s.opts.Backends))
	for i, backend := range s.opts.Backends {
		r.backends[backend.Name] = loopbackBackendAddress(s.opts.Bind, i)
	}

	cmd := exec.CommandContext(ctx, bin, "--config", configPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	r.mu.Lock()
	r.cmd = cmd
	r.mu.Unlock()
	if err := cmd.Start(); err != nil {
		return err
	}
	r.healthy.Store(true)
	defer r.healthy.Store(false)

	err = cmd.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
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

func loopbackBackendAddress(bind string, index int) string {
	if bind == "" || bind == "127.0.0.1:0" {
		return "127.0.0.1:0"
	}
	return bind
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
