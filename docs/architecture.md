# Architecture

`vialite` follows the same operational shape as `geyserlite`: a native
runtime with a small Go lifecycle wrapper. The key difference is where it
sits. `geyserlite` is an ingress translator before Gate; `vialite` is a
backend translator after Gate classic chooses a server.

```text
client or Bedrock client
  -> optional geyserlite
  -> Gate classic frontend
  -> Gate backend connection selection
  -> vialite translated backend address
  -> backend server
```

Gate remains the auth and session boundary. It still authenticates the
player, runs events, applies routing, and performs backend login. `vialite`
only translates protocol data between Gate's backend-facing protocol and
the selected backend's protocol.

## Runtime Layers

### Native Layer

The native layer is a headless Via runtime compiled with GraalVM
`native-image --shared`. Its entrypoints are called with the
`graal_isolatethread_t*` created by `graal_create_isolate`, so Gate's Go
process owns the native isolate lifecycle. It exposes a compact C ABI:

```c
int vialite_init(graal_isolatethread_t* thread, char* config_json);
int vialite_run(graal_isolatethread_t* thread);
int vialite_shutdown(graal_isolatethread_t* thread);
int vialite_status(graal_isolatethread_t* thread);
char* vialite_backend_address(graal_isolatethread_t* thread, char* backend_name);
```

The checked-in native layer is currently an ABI and native-image scaffold.
The intended runtime uses a native-library-owned loopback listener
internally because ViaProxy already models translation around Netty
channels. That listener stays internal to the loaded library. Operators
manage `vialite` as an in-process Gate capability, not as a standalone
sidecar.

### Go Layer

The Go module loads `libvialite.so` through `purego`, validates options,
materializes native config JSON, creates the Graal isolate, starts the
native runtime, waits for readiness, and resolves a backend name to the
local translated address Gate should dial.

Subprocess mode uses the same config surface and exists for debugging and
crash isolation. Embedded mode is the default and primary path.

### Gate Adapter

The Gate adapter is intentionally small. It converts YAML-friendly config
to `vialite.Options` and exposes `Start`, `Stop`, `Healthy`, and
`BackendDialAddress`.

Gate core integration belongs in Gate's backend connection path, not in
Lite. Lite preserves backend-owned auth by raw-piping after the handshake,
which is incompatible with packet translation as a transparent feature.

## Current Scaffold Versus Target Runtime

The current native scaffold validates the build and ABI path only. It can
publish placeholder backend dial addresses so the Go lifecycle, Gate
adapter, release automation, and native loading behavior can be tested
before the full Via runtime is wired.

The target runtime supports explicit backend protocol versions and `auto`
detection. In auto mode, the native runtime will probe the backend status
response and cache the detected protocol by backend name. Startup should
fail if a required backend cannot be detected.

## Forwarding

Because Gate owns identity, the target Via runtime must preserve backend
forwarding:

- `none`: normal offline-mode backend login
- `legacy`: BungeeCord-style null-delimited handshake data
- `velocity`: login plugin message forwarding

The Go config surface already carries these modes. Full packet-level
forwarding preservation belongs to the Via runtime wiring that follows
this scaffold.
