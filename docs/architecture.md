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
`native-image --shared`. It exposes a compact C ABI:

```c
int vialite_init(char* config_json);
int vialite_run(void);
int vialite_shutdown(void);
int vialite_status(void);
char* vialite_backend_address(char* backend_name);
```

The first implementation uses a native-library-owned loopback listener
internally because ViaProxy already models translation around Netty
channels. That listener is internal to the loaded library. Operators
manage `vialite` as an in-process Gate capability, not as a standalone
sidecar.

### Go Layer

The Go module loads `libvialite.so` through `purego`, validates options,
materializes native config JSON, starts the native runtime, and resolves a
backend name to the local translated address Gate should dial.

Subprocess mode uses the same config surface and exists for debugging and
crash isolation. Embedded mode is the default and primary path.

### Gate Adapter

The Gate adapter is intentionally small. It converts YAML-friendly config
to `vialite.Options` and exposes `Start`, `Stop`, `Healthy`, and
`BackendDialAddress`.

Gate core integration belongs in Gate's backend connection path, not in
Lite. Lite preserves backend-owned auth by raw-piping after the handshake,
which is incompatible with packet translation as a transparent feature.

## Backend Detection

Backends can be configured with an explicit protocol version or `auto`.
In auto mode, the native runtime probes the backend status response and
caches the detected protocol by backend name. Startup fails if a required
backend cannot be detected.

## Forwarding

Because Gate owns identity, `vialite` must preserve backend forwarding:

- `none`: normal offline-mode backend login
- `legacy`: BungeeCord-style null-delimited handshake data
- `velocity`: login plugin message forwarding

These are first-class compatibility gates for the initial implementation.
