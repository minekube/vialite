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
		if o.AllowDynamicBackends && o.Mode == ModeSubprocess {
			return o, nil
		}
		return o, ErrBackendRequired
	}

	seen := make(map[string]struct{}, len(o.Backends))
	for i := range o.Backends {
		b, err := normalizeBackend(o.Backends[i])
		if err != nil {
			return o, err
		}
		o.Backends[i] = b
		if b.Name == "" {
			return o, ErrBackendNameRequired
		}
		key := backendLookupName(b.Name)
		if _, ok := seen[key]; ok {
			return o, fmt.Errorf("%w: %s", ErrDuplicateBackend, b.Name)
		}
		seen[key] = struct{}{}
	}
	return o, nil
}

func normalizeBackend(b Backend) (Backend, error) {
	b.Name = strings.TrimSpace(b.Name)
	b.Address = strings.TrimSpace(b.Address)
	b.Version = strings.TrimSpace(b.Version)
	if b.Name == "" {
		return b, ErrBackendNameRequired
	}
	if b.Address == "" {
		return b, fmt.Errorf("%w: %s", ErrBackendAddressRequired, b.Name)
	}
	host, port, err := net.SplitHostPort(b.Address)
	if err != nil {
		return b, fmt.Errorf("%w: %s: %v", ErrInvalidBackendAddress, b.Name, err)
	}
	if strings.TrimSpace(host) == "" || strings.ContainsAny(host, " \t\r\n") {
		return b, fmt.Errorf("%w: %s: invalid host %q", ErrInvalidBackendAddress, b.Name, host)
	}
	portNum, err := strconv.Atoi(port)
	if err != nil || portNum < 1 || portNum > 65535 {
		return b, fmt.Errorf("%w: %s: invalid port %q", ErrInvalidBackendAddress, b.Name, port)
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
		return b, fmt.Errorf("%w: %s: %s", ErrInvalidForwardingMode, b.Name, b.Forwarding)
	}
	return b, nil
}

func backendLookupName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
