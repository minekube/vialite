package vialite

import (
	"encoding/json"
	"testing"
)

func TestNativeConfigJSON(t *testing.T) {
	opts, err := Options{
		GateProtocol: "1.26",
		Bind:         "127.0.0.1:25590",
		Backends: []Backend{
			{Name: "lobby", Address: "127.0.0.1:25566", Version: "auto", Forwarding: ForwardingVelocity},
			{Name: "legacy", Address: "127.0.0.1:25567", Version: "1.8", Forwarding: ForwardingLegacy},
		},
	}.validate()
	if err != nil {
		t.Fatalf("validate: %v", err)
	}

	data, err := opts.nativeConfigJSON()
	if err != nil {
		t.Fatalf("nativeConfigJSON: %v", err)
	}

	var cfg nativeConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	if cfg.Bind != "127.0.0.1:25590" {
		t.Fatalf("Bind = %q", cfg.Bind)
	}
	if cfg.GateProtocol != "1.26" {
		t.Fatalf("GateProtocol = %q", cfg.GateProtocol)
	}
	if len(cfg.Backends) != 2 {
		t.Fatalf("len(Backends) = %d, want 2", len(cfg.Backends))
	}
	if cfg.Backends[0].Name != "lobby" || cfg.Backends[0].Detect != true || cfg.Backends[0].Forwarding != "velocity" {
		t.Fatalf("unexpected lobby backend: %#v", cfg.Backends[0])
	}
	if cfg.Backends[1].Name != "legacy" || cfg.Backends[1].Detect != false || cfg.Backends[1].Version != "1.8" {
		t.Fatalf("unexpected legacy backend: %#v", cfg.Backends[1])
	}
}
