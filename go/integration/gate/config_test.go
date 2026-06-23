package gate

import (
	"errors"
	"testing"

	vialite "go.minekube.com/vialite"
)

func TestConfigToOptions(t *testing.T) {
	opts, err := Config{
		Enabled:      true,
		Mode:         "subprocess",
		GateProtocol: "1.26",
		Bind:         "127.0.0.1:25590",
		Backends: []BackendConfig{{
			Name:       "lobby",
			Address:    "127.0.0.1:25566",
			Version:    "auto",
			Forwarding: "velocity",
		}},
	}.toOptions(nil)
	if err != nil {
		t.Fatalf("toOptions: %v", err)
	}
	if opts.Mode != vialite.ModeSubprocess {
		t.Fatalf("Mode = %v", opts.Mode)
	}
	if opts.GateProtocol != "1.26" || opts.Bind != "127.0.0.1:25590" {
		t.Fatalf("unexpected options: %#v", opts)
	}
	if len(opts.Backends) != 1 || opts.Backends[0].Forwarding != vialite.ForwardingVelocity {
		t.Fatalf("unexpected backends: %#v", opts.Backends)
	}
}

func TestConfigToOptionsDefaultsSubprocess(t *testing.T) {
	opts, err := Config{Enabled: true}.toOptions(nil)
	if err != nil {
		t.Fatalf("toOptions: %v", err)
	}
	if opts.Mode != vialite.ModeSubprocess {
		t.Fatalf("Mode = %v", opts.Mode)
	}
}

func TestConfigToOptionsEmbedded(t *testing.T) {
	opts, err := Config{Enabled: true, Mode: "embedded"}.toOptions(nil)
	if err != nil {
		t.Fatalf("toOptions: %v", err)
	}
	if opts.Mode != vialite.ModeEmbedded {
		t.Fatalf("Mode = %v", opts.Mode)
	}
}

func TestConfigToOptionsLegacyForwarding(t *testing.T) {
	opts, err := Config{
		Enabled: true,
		Backends: []BackendConfig{{
			Name:       "legacy",
			Address:    "127.0.0.1:25567",
			Version:    "1.8",
			Forwarding: "legacy",
		}},
	}.toOptions(nil)
	if err != nil {
		t.Fatalf("toOptions: %v", err)
	}
	if opts.Backends[0].Forwarding != vialite.ForwardingLegacy {
		t.Fatalf("Forwarding = %q", opts.Backends[0].Forwarding)
	}
}

func TestConfigToOptionsErrors(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want error
	}{
		{
			name: "bad mode",
			cfg:  Config{Enabled: true, Mode: "weird"},
			want: ErrInvalidMode,
		},
		{
			name: "bad forwarding",
			cfg: Config{Enabled: true, Backends: []BackendConfig{{
				Name:       "lobby",
				Address:    "127.0.0.1:25566",
				Forwarding: "weird",
			}}},
			want: ErrInvalidForwarding,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.cfg.toOptions(nil)
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
		})
	}
}
