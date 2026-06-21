package gate

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"

	vialite "go.minekube.com/vialite"
)

var (
	ErrInvalidMode       = errors.New("gate/vialite: invalid mode")
	ErrInvalidForwarding = errors.New("gate/vialite: invalid forwarding")
)

type Config struct {
	Enabled bool `yaml:"enabled" json:"enabled"`

	Mode         string `yaml:"mode" json:"mode"`
	GateProtocol string `yaml:"gate_protocol" json:"gate_protocol"`
	Bind         string `yaml:"bind" json:"bind"`

	LibraryPath string `yaml:"library_path" json:"library_path"`
	BinaryPath  string `yaml:"binary_path" json:"binary_path"`
	Version     string `yaml:"version" json:"version"`
	Mirror      string `yaml:"mirror" json:"mirror"`
	Offline     bool   `yaml:"offline" json:"offline"`

	Backends []BackendConfig `yaml:"backends" json:"backends"`
}

type BackendConfig struct {
	Name       string `yaml:"name" json:"name"`
	Address    string `yaml:"address" json:"address"`
	Version    string `yaml:"version" json:"version"`
	Forwarding string `yaml:"forwarding" json:"forwarding"`
}

func (c Config) toOptions(logger *slog.Logger) (vialite.Options, error) {
	opts := vialite.Options{
		GateProtocol: c.GateProtocol,
		Bind:         c.Bind,
		LibraryPath:  c.LibraryPath,
		BinaryPath:   c.BinaryPath,
		Version:      c.Version,
		Mirror:       c.Mirror,
		Offline:      c.Offline,
		Logger:       logger,
		Backends:     make([]vialite.Backend, 0, len(c.Backends)),
	}
	switch strings.ToLower(c.Mode) {
	case "", "embedded":
		opts.Mode = vialite.ModeEmbedded
	case "subprocess":
		opts.Mode = vialite.ModeSubprocess
	default:
		return vialite.Options{}, fmt.Errorf("%w: %s", ErrInvalidMode, c.Mode)
	}
	for _, backend := range c.Backends {
		forwarding, err := parseForwarding(backend.Forwarding)
		if err != nil {
			return vialite.Options{}, err
		}
		opts.Backends = append(opts.Backends, vialite.Backend{
			Name:       backend.Name,
			Address:    backend.Address,
			Version:    backend.Version,
			Forwarding: forwarding,
		})
	}
	return opts, nil
}

func parseForwarding(value string) (vialite.ForwardingMode, error) {
	switch strings.ToLower(value) {
	case "", "none":
		return vialite.ForwardingNone, nil
	case "legacy":
		return vialite.ForwardingLegacy, nil
	case "velocity":
		return vialite.ForwardingVelocity, nil
	default:
		return "", fmt.Errorf("%w: %s", ErrInvalidForwarding, value)
	}
}
