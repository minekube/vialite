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
	Bind       string `json:"bind,omitempty"`
	Version    string `json:"version"`
	Detect     bool   `json:"detect"`
	Forwarding string `json:"forwarding"`
}

func (o Options) nativeConfigJSON() ([]byte, error) {
	return o.nativeConfigJSONWithBackendBinds(nil)
}

func (o Options) nativeConfigJSONWithBackendBinds(backendBinds map[string]string) ([]byte, error) {
	cfg := nativeConfig{
		Bind:         o.Bind,
		GateProtocol: o.GateProtocol,
		Backends:     make([]nativeBackendConfig, 0, len(o.Backends)),
	}
	for _, backend := range o.Backends {
		bind := backendBinds[backend.Name]
		if bind == "" {
			bind = backendBinds[backendLookupName(backend.Name)]
		}
		cfg.Backends = append(cfg.Backends, nativeBackendConfig{
			Name:       backend.Name,
			Address:    backend.Address,
			Bind:       bind,
			Version:    backend.Version,
			Detect:     backend.Detect,
			Forwarding: string(backend.Forwarding),
		})
	}
	return json.Marshal(cfg)
}

func nativeBackendConfigJSON(backend Backend, bind string) ([]byte, error) {
	return json.Marshal(nativeBackendConfig{
		Name:       backend.Name,
		Address:    backend.Address,
		Bind:       bind,
		Version:    backend.Version,
		Detect:     backend.Detect,
		Forwarding: string(backend.Forwarding),
	})
}
