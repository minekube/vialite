# vialite In-Process Native Design

## Summary

`vialite` is Minekube's native Via compatibility layer for Gate. It packages the Via protocol translation stack behind a GraalVM native shared library and exposes a Go API that Gate can import, manage, and call in-process.

The target topology is:

```text
player / Bedrock
  -> optional geyserlite
  -> Gate classic
  -> libvialite loaded in-process
  -> backend server
```

The first scope is backend version compatibility for Gate classic. `vialite` translates between the protocol Gate speaks on a backend connection and the protocol a selected backend server actually runs. Frontend support for clients Gate cannot parse is excluded from this spec.

Licensing strategy is intentionally not part of this technical spec. The implementation assumes Minekube will secure the rights needed to use Via code in this integration form.

## Goals

- Build a new public repository at `minekube/vialite`.
- Mirror the successful `geyserlite` product shape where it applies: native artifacts, Go library, embedded mode, subprocess fallback, auto-download, embed tag, release automation, and Gate integration package.
- Make embedded shared-library mode the primary runtime path.
- Keep subprocess mode as a debugging and crash-isolation fallback, not as the ideal architecture.
- Give Gate a backend-compatibility API that can route selected backend connections through Via translation without changing Gate's frontend auth/session ownership.
- Preserve Gate's normal player authentication, events, routing, forwarding, and backend login behavior.
- Support per-backend target protocol detection and explicit target-version configuration.
- Prove compatibility with Gate legacy forwarding and Velocity forwarding through integration tests.

## Non-Goals

- Do not add Via support to Gate Lite. Lite's value is raw backend-owned auth and a minimal TCP pipe.
- Do not support arbitrary older/newer Java clients into Gate in the first implementation.
- Do not reimplement Via's packet mapping logic in Go.
- Do not make an external TCP sidecar the primary design.
- Do not modify Gate core in the initial `vialite` repository scaffold. Gate integration is represented by an adapter package and tests in this repository.

## Architecture

`vialite` has three layers:

1. Native Java layer
   - A minimal/headless Via platform built from ViaProxy/ViaVersion components.
   - Compiled with GraalVM `native-image --shared` into `libvialite.so`.
   - Exposes a small C ABI through GraalVM `@CEntryPoint` methods.

2. Go library layer
   - Module path: `go.minekube.com/vialite`.
   - Loads `libvialite.so` through `purego` on supported platforms.
   - Provides subprocess fallback for debugging and crash isolation.
   - Handles artifact location, embedded assets, runtime auto-download, checksum verification, lifecycle, and health.

3. Gate adapter layer
   - Package path: `go.minekube.com/vialite/integration/gate`.
   - Converts Gate-shaped configuration into `vialite.Options`.
   - Provides a fakeable interface for tests.
   - Eventually plugs into Gate's backend connection path, not Lite.

The preferred runtime model is in-process:

```text
Gate backend connection request
  -> vialite Go adapter
  -> libvialite C ABI
  -> in-process Via/Netty translation runtime
  -> backend server
```

The first implementation uses a native-library-owned loopback listener internally because ViaProxy already models translation around Netty channels. That loopback listener is an implementation detail of the loaded library, not an externally managed sidecar. The public Go API must not expose "start this external proxy" as its primary concept; it exposes backend translation lifecycle and connection routing.

## Public Go API

The initial Go package exposes:

```go
package vialite

type Mode int

const (
    ModeEmbedded Mode = iota
    ModeSubprocess
)

type Options struct {
    Mode Mode

    GateProtocol string
    Bind string

    LibraryPath string
    BinaryPath string

    Version string
    Mirror string
    Offline bool

    Logger *slog.Logger
    RestartPolicy *RestartPolicy
    ShutdownTimeout time.Duration

    Backends []Backend
}

type Backend struct {
    Name string
    Address string
    Version string
    Detect bool
    Forwarding ForwardingMode
}

type ForwardingMode string

const (
    ForwardingNone ForwardingMode = "none"
    ForwardingLegacy ForwardingMode = "legacy"
    ForwardingVelocity ForwardingMode = "velocity"
)

type Server struct {
}

func New(opts Options) (*Server, error)
func (s *Server) Start(ctx context.Context) error
func (s *Server) Stop(ctx context.Context) error
func (s *Server) Healthy() bool
func (s *Server) BackendDialAddress(name string) (string, error)
```

`BackendDialAddress` returns the local address Gate should dial for a translated backend. This keeps the first implementation compatible with Gate's current `net.Conn` backend connection model. Direct file-descriptor or socketpair handoff is excluded from this spec.

## Native C ABI

The native layer exports a compact lifecycle API:

```c
int vialite_init(char* config_json);
int vialite_run(void);
int vialite_shutdown(void);
int vialite_status(void);
char* vialite_backend_address(char* backend_name);
```

The Go embedded runner creates a Graal isolate, calls `vialite_init`, starts `vialite_run`, calls `vialite_backend_address` for configured backends, and uses `vialite_shutdown` on context cancellation.

`config_json` carries a stable JSON structure with bind address, Gate protocol, backend list, forwarding mode, version detection mode, and logging configuration. JSON is preferred over many C ABI arguments because it keeps the ABI stable as configuration grows.

## Backend Version Detection

`vialite` supports two backend-version modes:

- Explicit: `Backend.Version` is set to a Via protocol version name or numeric protocol id.
- Auto: `Backend.Detect` is true or `Backend.Version` is empty, so `vialite` probes the backend status response and caches the detected protocol.

The detector mirrors ViaVersion's Velocity strategy: key protocol information by backend name and refresh it on startup and on first use. Periodic refresh is excluded from this spec.

If detection fails, startup fails for required backends. Lazy degraded mode is excluded from this spec so broken backend compatibility is visible.

## Forwarding Compatibility

Gate owns authentication and player identity. `vialite` must preserve Gate's backend forwarding data.

The first compatibility matrix is:

| Gate forwarding mode | Expected behavior |
|---|---|
| none | Vialite forwards normal offline-mode login data to backend. |
| legacy | Vialite preserves null-delimited BungeeCord-style forwarding data in the handshake address. |
| velocity | Vialite preserves login plugin message forwarding between Gate and backend. |

The first proof of usefulness is an integration test where Gate connects through `vialite` to a backend requiring each forwarding mode and the backend receives the expected identity data.

## Repository Layout

```text
vialite/
├── README.md
├── docs/
│   ├── architecture.md
│   ├── gate.md
│   ├── troubleshooting.md
│   └── superpowers/
│       └── specs/
├── build/
│   ├── via.version
│   ├── graalvm.version
│   ├── Dockerfile
│   ├── apply-overlay.sh
│   ├── overlay/
│   └── agent-config/
├── go/
│   ├── go.mod
│   ├── vialite.go
│   ├── options.go
│   ├── server.go
│   ├── embedded.go
│   ├── subprocess.go
│   ├── locate.go
│   ├── download.go
│   ├── embed*.go
│   └── integration/
│       └── gate/
└── .github/
    └── workflows/
```

Rust bindings are deliberately excluded from the first implementation. The Go module and Gate adapter are enough to validate the Minekube path. Rust can follow once the native ABI and artifact names settle.

## Release And Update Loop

`vialite` should reuse the `geyserlite` automation shape:

- `build/via.version` pins the upstream Via source revision.
- `build/graalvm.version` pins the GraalVM build image.
- Renovate watches the upstream Via source revision and opens `fix(deps): bump Via to <sha>` PRs.
- CI applies the overlay, builds the native image, runs Go tests, and runs integration smoke tests.
- Release-please cuts version tags from conventional commits.
- Release workflow uploads native executable, shared library, header, checksums, signatures, and attestations.
- Go auto-download resolves artifacts from GitHub Releases and verifies `checksums.txt`.

The first release needs Linux amd64 and Linux arm64 shared libraries. Windows can start as subprocess-only if shared library support is not ready.

## Testing Strategy

The implementation plan must start with unit tests for the Go library:

- option defaults and validation
- asset naming
- checksum parsing and verified download path
- locate order
- embedded/subprocess runner lifecycle fakes
- Gate adapter config conversion

Native smoke tests then prove:

- `libvialite.so` exports all required C ABI symbols.
- embedded mode can start and report healthy.
- subprocess mode can start and report healthy.
- backend version auto-detection returns an expected protocol id against a test backend.

Gate compatibility tests then prove:

- legacy forwarding survives through `vialite`
- Velocity forwarding survives through `vialite`
- status ping works through `vialite`
- login works through `vialite`

## Error Handling

The Go API returns sentinel errors for common operator mistakes:

- missing backend name
- duplicate backend name
- invalid backend address
- unsupported platform for embedded mode
- missing native library
- missing native binary
- auto-download disabled and artifact not found
- backend protocol detection failure
- backend not found in the native runtime

Subprocess mode uses exponential restart backoff. Embedded mode does not restart inside the host process; if the native runtime returns an unrecoverable error, `Start` returns that error and Gate's process manager decides what to do.

## Open Decisions Locked By This Spec

- `vialite` is built as a separate repository.
- The ideal runtime is embedded shared-library mode.
- The first public API returns a backend dial address instead of a direct `net.Conn`.
- The first supported integration point is Gate classic backend connections.
- Gate Lite integration is out of scope.
- Rust bindings are out of scope for the first implementation.
