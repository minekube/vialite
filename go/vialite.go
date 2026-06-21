package vialite

import (
	"errors"
	"log/slog"
	"time"
)

// Mode selects how vialite is executed.
type Mode int

const (
	// ModeEmbedded loads libvialite through purego and calls the C ABI in-process.
	ModeEmbedded Mode = iota
	// ModeSubprocess starts the vialite native binary as a child process.
	ModeSubprocess
)

// ForwardingMode tells vialite which Gate backend forwarding scheme must be preserved.
type ForwardingMode string

const (
	ForwardingNone     ForwardingMode = "none"
	ForwardingLegacy   ForwardingMode = "legacy"
	ForwardingVelocity ForwardingMode = "velocity"
)

// Options configures a Server.
type Options struct {
	Mode Mode

	// GateProtocol is the protocol Gate speaks to vialite. Empty defaults to "auto".
	GateProtocol string
	// Bind is the native runtime's internal loopback bind address.
	Bind string

	LibraryPath string
	BinaryPath  string

	Version string
	Mirror  string
	Offline bool

	Logger          *slog.Logger
	RestartPolicy   *RestartPolicy
	ShutdownTimeout time.Duration

	Backends []Backend
}

// Backend describes a translated backend server.
type Backend struct {
	Name       string
	Address    string
	Version    string
	Detect     bool
	Forwarding ForwardingMode
}

// RestartPolicy controls subprocess restart behavior.
type RestartPolicy struct {
	MinBackoff time.Duration
	MaxBackoff time.Duration
	MaxRetries int
}

var (
	ErrInvalidMode             = errors.New("vialite: invalid mode")
	ErrBackendRequired         = errors.New("vialite: at least one backend is required")
	ErrBackendNameRequired     = errors.New("vialite: backend name is required")
	ErrDuplicateBackend        = errors.New("vialite: duplicate backend name")
	ErrBackendAddressRequired  = errors.New("vialite: backend address is required")
	ErrInvalidBackendAddress   = errors.New("vialite: invalid backend address")
	ErrInvalidForwardingMode   = errors.New("vialite: invalid forwarding mode")
	ErrNotStarted              = errors.New("vialite: server not started")
	ErrAlreadyStarted          = errors.New("vialite: server already started")
	ErrNotReady                = errors.New("vialite: server not ready")
	ErrBackendNotFound         = errors.New("vialite: backend not found")
	ErrNoBinary                = errors.New("vialite: native binary not found")
	ErrNoLibrary               = errors.New("vialite: native library not found")
	ErrUnsupportedEmbeddedMode = errors.New("vialite: embedded mode is unsupported on this platform")
	ErrInvalidChecksum         = errors.New("vialite: invalid checksum")
)
