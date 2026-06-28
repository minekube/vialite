package vialite

import (
	"errors"
	"log/slog"
	"testing"
	"time"
)

func TestOptionsValidateDefaults(t *testing.T) {
	opts, err := Options{
		Backends: []Backend{{Name: "lobby", Address: "127.0.0.1:25566"}},
	}.validate()
	if err != nil {
		t.Fatalf("validate returned error: %v", err)
	}
	if opts.Mode != ModeEmbedded {
		t.Fatalf("Mode = %v, want ModeEmbedded", opts.Mode)
	}
	if opts.Bind != "127.0.0.1:0" {
		t.Fatalf("Bind = %q, want loopback ephemeral", opts.Bind)
	}
	if opts.GateProtocol != "auto" {
		t.Fatalf("GateProtocol = %q, want auto", opts.GateProtocol)
	}
	if opts.Logger == nil {
		t.Fatal("Logger is nil")
	}
	if opts.RestartPolicy == nil {
		t.Fatal("RestartPolicy is nil")
	}
	if opts.RestartPolicy.MinBackoff != time.Second {
		t.Fatalf("MinBackoff = %s, want 1s", opts.RestartPolicy.MinBackoff)
	}
	if opts.RestartPolicy.MaxBackoff != time.Minute {
		t.Fatalf("MaxBackoff = %s, want 1m", opts.RestartPolicy.MaxBackoff)
	}
	if opts.ShutdownTimeout != 30*time.Second {
		t.Fatalf("ShutdownTimeout = %s, want 30s", opts.ShutdownTimeout)
	}
	if opts.BackendStartupTimeout != 25*time.Second {
		t.Fatalf("BackendStartupTimeout = %s, want 25s", opts.BackendStartupTimeout)
	}
	if !opts.Backends[0].Detect {
		t.Fatal("backend Detect = false, want true for empty version")
	}
	if opts.Backends[0].Version != "auto" {
		t.Fatalf("backend Version = %q, want auto", opts.Backends[0].Version)
	}
	if opts.Backends[0].Forwarding != ForwardingNone {
		t.Fatalf("backend Forwarding = %q, want none", opts.Backends[0].Forwarding)
	}
}

func TestOptionsValidateKeepsExplicitValues(t *testing.T) {
	logger := slog.Default()
	policy := &RestartPolicy{MinBackoff: 2 * time.Second, MaxBackoff: 10 * time.Second, MaxRetries: 3}
	opts, err := Options{
		Mode:                  ModeSubprocess,
		GateProtocol:          "1.26",
		Bind:                  "127.0.0.1:25590",
		Logger:                logger,
		RestartPolicy:         policy,
		ShutdownTimeout:       5 * time.Second,
		BackendStartupTimeout: 7 * time.Second,
		Backends: []Backend{{
			Name:       "lobby",
			Address:    "127.0.0.1:25566",
			Version:    "1.20.4",
			Forwarding: ForwardingVelocity,
		}},
	}.validate()
	if err != nil {
		t.Fatalf("validate returned error: %v", err)
	}
	if opts.Mode != ModeSubprocess || opts.GateProtocol != "1.26" || opts.Bind != "127.0.0.1:25590" {
		t.Fatalf("explicit values not preserved: %#v", opts)
	}
	if opts.Logger != logger {
		t.Fatal("logger pointer was not preserved")
	}
	if opts.RestartPolicy != policy {
		t.Fatal("restart policy pointer was not preserved")
	}
	if opts.BackendStartupTimeout != 7*time.Second {
		t.Fatalf("BackendStartupTimeout = %s, want 7s", opts.BackendStartupTimeout)
	}
	if opts.Backends[0].Detect {
		t.Fatal("Detect = true, want false for explicit version")
	}
}

func TestOptionsValidateAllowsDynamicStartupWithoutBackends(t *testing.T) {
	for _, mode := range []Mode{ModeEmbedded, ModeSubprocess} {
		opts, err := Options{
			Mode:                 mode,
			AllowDynamicBackends: true,
		}.validate()
		if err != nil {
			t.Fatalf("validate mode %d returned error: %v", mode, err)
		}
		if !opts.AllowDynamicBackends {
			t.Fatal("AllowDynamicBackends = false")
		}
		if len(opts.Backends) != 0 {
			t.Fatalf("Backends = %d, want 0", len(opts.Backends))
		}
	}
}

func TestOptionsValidateErrors(t *testing.T) {
	tests := []struct {
		name string
		opts Options
		want error
	}{
		{
			name: "no backends",
			opts: Options{},
			want: ErrBackendRequired,
		},
		{
			name: "missing backend name",
			opts: Options{Backends: []Backend{{Address: "127.0.0.1:25565"}}},
			want: ErrBackendNameRequired,
		},
		{
			name: "duplicate backend name",
			opts: Options{Backends: []Backend{
				{Name: "lobby", Address: "127.0.0.1:25565"},
				{Name: "lobby", Address: "127.0.0.1:25566"},
			}},
			want: ErrDuplicateBackend,
		},
		{
			name: "duplicate backend name different case",
			opts: Options{Backends: []Backend{
				{Name: "Lobby", Address: "127.0.0.1:25565"},
				{Name: "lobby", Address: "127.0.0.1:25566"},
			}},
			want: ErrDuplicateBackend,
		},
		{
			name: "missing backend address",
			opts: Options{Backends: []Backend{{Name: "lobby"}}},
			want: ErrBackendAddressRequired,
		},
		{
			name: "invalid backend address",
			opts: Options{Backends: []Backend{{Name: "lobby", Address: "not a host:port"}}},
			want: ErrInvalidBackendAddress,
		},
		{
			name: "invalid forwarding",
			opts: Options{Backends: []Backend{{Name: "lobby", Address: "127.0.0.1:25565", Forwarding: "weird"}}},
			want: ErrInvalidForwardingMode,
		},
		{
			name: "invalid mode",
			opts: Options{Mode: Mode(99), Backends: []Backend{{Name: "lobby", Address: "127.0.0.1:25565"}}},
			want: ErrInvalidMode,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.opts.validate()
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
		})
	}
}
