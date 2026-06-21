package gate

import (
	"context"
	"errors"
	"log/slog"

	vialite "go.minekube.com/vialite"
)

type server interface {
	Start(ctx context.Context) error
	WaitReady(ctx context.Context) error
	Stop(ctx context.Context) error
	Healthy() bool
	BackendDialAddress(name string) (string, error)
}

type Via struct {
	server server
	logger *slog.Logger
}

var newSrv = func(opts vialite.Options) (server, error) {
	return vialite.New(opts)
}

func New(cfg Config, logger *slog.Logger) (*Via, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	opts, err := cfg.toOptions(logger)
	if err != nil {
		return nil, err
	}
	srv, err := newSrv(opts)
	if err != nil {
		return nil, err
	}
	return &Via{server: srv, logger: logger}, nil
}

func (v *Via) Start(ctx context.Context) error {
	if v == nil {
		return nil
	}
	err := v.server.Start(ctx)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil
	}
	return err
}

func (v *Via) Stop(ctx context.Context) error {
	if v == nil {
		return nil
	}
	return v.server.Stop(ctx)
}

func (v *Via) WaitReady(ctx context.Context) error {
	if v == nil {
		return vialite.ErrNotStarted
	}
	return v.server.WaitReady(ctx)
}

func (v *Via) Healthy() bool {
	if v == nil {
		return false
	}
	return v.server.Healthy()
}

func (v *Via) BackendDialAddress(name string) (string, error) {
	if v == nil {
		return "", vialite.ErrNotStarted
	}
	return v.server.BackendDialAddress(name)
}
