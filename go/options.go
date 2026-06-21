package vialite

import (
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"
)

func (o Options) validate() (Options, error) {
	switch o.Mode {
	case ModeEmbedded, ModeSubprocess:
	default:
		return o, fmt.Errorf("%w: %d", ErrInvalidMode, o.Mode)
	}
	if o.GateProtocol == "" {
		o.GateProtocol = "auto"
	}
	if o.Bind == "" {
		o.Bind = "127.0.0.1:0"
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.RestartPolicy == nil {
		o.RestartPolicy = &RestartPolicy{
			MinBackoff: time.Second,
			MaxBackoff: time.Minute,
		}
	}
	if o.ShutdownTimeout == 0 {
		o.ShutdownTimeout = 30 * time.Second
	}
	if len(o.Backends) == 0 {
		return o, ErrBackendRequired
	}

	seen := make(map[string]struct{}, len(o.Backends))
	for i := range o.Backends {
		b := &o.Backends[i]
		b.Name = strings.TrimSpace(b.Name)
		b.Address = strings.TrimSpace(b.Address)
		b.Version = strings.TrimSpace(b.Version)
		if b.Name == "" {
			return o, ErrBackendNameRequired
		}
		if _, ok := seen[b.Name]; ok {
			return o, fmt.Errorf("%w: %s", ErrDuplicateBackend, b.Name)
		}
		seen[b.Name] = struct{}{}
		if b.Address == "" {
			return o, fmt.Errorf("%w: %s", ErrBackendAddressRequired, b.Name)
		}
		host, port, err := net.SplitHostPort(b.Address)
		if err != nil {
			return o, fmt.Errorf("%w: %s: %v", ErrInvalidBackendAddress, b.Name, err)
		}
		if strings.TrimSpace(host) == "" || strings.ContainsAny(host, " \t\r\n") {
			return o, fmt.Errorf("%w: %s: invalid host %q", ErrInvalidBackendAddress, b.Name, host)
		}
		portNum, err := strconv.Atoi(port)
		if err != nil || portNum < 1 || portNum > 65535 {
			return o, fmt.Errorf("%w: %s: invalid port %q", ErrInvalidBackendAddress, b.Name, port)
		}
		if b.Version == "" || strings.EqualFold(b.Version, "auto") {
			b.Version = "auto"
			b.Detect = true
		}
		if b.Forwarding == "" {
			b.Forwarding = ForwardingNone
		}
		switch b.Forwarding {
		case ForwardingNone, ForwardingLegacy, ForwardingVelocity:
		default:
			return o, fmt.Errorf("%w: %s: %s", ErrInvalidForwardingMode, b.Name, b.Forwarding)
		}
	}
	return o, nil
}
