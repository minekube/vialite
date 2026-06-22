package vialite

import "encoding/json"

type nativeConfig struct {
	Bind         string                `json:"bind"`
	GateProtocol string                `json:"gate_protocol"`
	Backends     []nativeBackendConfig `json:"backends"`
}

type nativeBackendConfig struct {
	Name       string `json:"name"`
	Address    string `json:"address"`
	Version    string `json:"version"`
	Detect     bool   `json:"detect"`
	Forwarding string `json:"forwarding"`
}

func (o Options) nativeConfigJSON() ([]byte, error) {
	cfg := nativeConfig{
		Bind:         o.Bind,
		GateProtocol: o.GateProtocol,
		Backends:     make([]nativeBackendConfig, 0, len(o.Backends)),
	}
	for _, backend := range o.Backends {
		cfg.Backends = append(cfg.Backends, nativeBackendConfig{
			Name:       backend.Name,
			Address:    backend.Address,
			Version:    backend.Version,
			Detect:     backend.Detect,
			Forwarding: string(backend.Forwarding),
		})
	}
	return json.Marshal(cfg)
}

func (o Options) nativeConfigJSONForBackend(backend Backend, bind string) ([]byte, error) {
	cfg := nativeConfig{
		Bind:         bind,
		GateProtocol: o.GateProtocol,
		Backends: []nativeBackendConfig{{
			Name:       backend.Name,
			Address:    backend.Address,
			Version:    backend.Version,
			Detect:     backend.Detect,
			Forwarding: string(backend.Forwarding),
		}},
	}
	return json.Marshal(cfg)
}
