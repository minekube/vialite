# vialite Subprocess ViaProxy Runtime Design

## Context

`vialite v0.1.0` publishes the required native artifact matrix and Gate now
depends on that released module, but the native bridge is still an ABI scaffold.
It exports `vialite_init`, `vialite_run`, `vialite_shutdown`,
`vialite_status`, and `vialite_backend_address`, but it does not run Via packet
translation. The current bridge returns a placeholder backend address, so
Gate cannot yet perform meaningful newer-client-to-older-backend or
older-client-to-newer-backend compatibility testing.

ViaProxy already contains the real Netty/Via pipeline:

- `ViaProxy.startProxy()` starts a `NetServer` with
  `Client2ProxyChannelInitializer`.
- `Client2ProxyHandler` reads the client handshake, chooses the configured
  target server and target protocol, then creates a `ProxyConnection` with
  `Proxy2ServerChannelInitializer`.
- Both sides install `ViaProxyViaCodec`, which delegates packet translation to
  the Via stack.

The complication is that ViaProxy is currently organized around static global
state: one static `ViaProxy.CONFIG` and one static `currentProxyServer`.
Supporting several Gate backends in one embedded in-process runtime would
require invasive ViaProxy changes. The fastest path to real compatibility
testing is therefore subprocess-first.

## Decision

Implement the first real runtime in `vialite` subprocess mode.

The Go subprocess runner will start one native `vialite` process per configured
Gate backend. Each native process owns exactly one loopback listener and one
target backend. Gate receives one loopback address per backend from the Go
runner and dials those addresses instead of dialing Paper directly. The native
process then proxies that connection through ViaProxy to the configured Paper
backend.

```text
player -> Gate classic -> 127.0.0.1:<vialite backend listener>
       -> ViaProxy pipeline inside native vialite process
       -> Paper backend
```

Embedded mode remains supported as a scaffold in this slice, but it is not the
runtime used for real protocol compatibility testing. A later embedded slice can
remove the one-process-per-backend cost after the subprocess behavior is proven.

## Native Process Contract

The native executable accepts:

```text
vialite --config /path/to/native-config.json
```

For subprocess mode, the Go runner writes one JSON config per backend. Each file
contains:

- `bind`: concrete loopback address the native process must listen on
- `gate_protocol`: Gate-side protocol setting, currently `auto`
- exactly one backend entry:
  - `name`
  - `address`
  - `version` or `auto`
  - `detect`
  - `forwarding`

The native process must:

1. parse the JSON config;
2. reject configs with zero or more than one backend;
3. generate a ViaProxy YAML config in a temporary work directory;
4. set the ViaProxy bind address to `bind`;
5. set the ViaProxy target address to the backend address;
6. set ViaProxy target version to the configured backend version, or auto;
7. configure forwarding preservation:
   - `none`: no BungeeCord player info passthrough and no backend HAProxy
   - `legacy`: enable BungeeCord player info passthrough
   - `velocity`: fail clearly in this slice unless ViaProxy can preserve the
     Velocity forwarding payload without Gate-side packet changes;
8. start ViaProxy in headless config mode;
9. stay alive until terminated by the Go runner.

The process should disable update checks and UI behavior. It must fail fast with
a non-zero exit if the config cannot be parsed, the target version is unknown,
the bind address is invalid, or ViaProxy startup fails.

## Go Runner Contract

The Go `subprocessRunner` will split `Options.Backends` into per-backend native
processes:

- allocate one concrete loopback bind per backend;
- write one JSON config per backend;
- start one native executable per backend;
- wait until every loopback listener accepts TCP connections;
- publish `BackendDialAddress(name)` only after all configured backend listeners
  are ready;
- stop every child process when the context is cancelled;
- if any child exits before readiness, fail startup;
- if any child exits after readiness, stop the remaining children and return an
  error so the existing restart policy can restart the group.

Backend names remain the public lookup keys used by Gate. The Go runner should
not expose ViaProxy's single-target limitation to Gate.

## Version Handling

The first implementation must support:

- `auto` or empty backend version: ViaProxy auto-detects the backend protocol;
- ViaProxy protocol names accepted by its `ProtocolVersionTypeSerializer`, such
  as `1.21.10` and `1.20.4`, if provided explicitly.

Unsupported version strings must fail during native process startup with a clear
message. The Go side only validates that the string is non-empty or `auto`; the
native side owns ViaProxy-specific version parsing.

## Forwarding

Gate owns frontend login and backend identity forwarding. In this subprocess
shape, Gate sends its backend connection to vialite, and vialite opens the real
backend connection. That means vialite must preserve the forwarding payload Gate
placed in the handshake.

For this slice:

- `legacy` forwarding maps to ViaProxy's BungeeCord player info passthrough.
- `none` leaves the handshake untouched except for ViaProxy's normal target
  rewrite.
- `velocity` is not considered proven until tested against a Velocity-forwarding
  Paper backend. If the ViaProxy pipeline cannot preserve the required login
  plugin forwarding without additional Gate/vialite coordination, velocity mode
  must fail explicitly instead of silently degrading identity forwarding.

## Testing

Unit tests:

- Go subprocess runner starts one process per backend and maps backend names to
  distinct loopback addresses.
- Go subprocess runner fails startup if any child exits before its listener is
  ready.
- Java config parser rejects zero and multiple backends.
- Java config writer emits ViaProxy settings for bind address, target address,
  target version, and forwarding mode.

Integration tests:

- Native executable starts from a one-backend config and opens the expected
  loopback listener.
- Gate starts with `via.enabled: true` and `via.mode: subprocess`.
- Newer client to older Paper works through Gate and vialite.
- Older client to newer Paper works through Gate and vialite if the Via stack
  supports that direction for the chosen versions.

## Release Flow

After implementation:

1. run Go tests and overlay/Gradle checks;
2. merge the vialite runtime PR;
3. create the next vialite release;
4. verify release assets and checksums;
5. update Gate to the new vialite version;
6. run Via-enabled compatibility tests against Paper backends.

## Non-Goals

- Embedded in-process multi-backend runtime.
- Bedrock protocol translation.
- Windows embedded DLL support.
- Changing Gate's classic/lite architecture.
- Implementing a custom ViaVersion pipeline separate from ViaProxy.
