<!-- markdownlint-disable MD041 -->

<div align="center">

# vialite

**Native Via compatibility for Gate backend connections.**
**Embeddable in Go; built for Gate classic edge proxies.**

[![Go Reference](https://pkg.go.dev/badge/go.minekube.com/vialite.svg)](https://pkg.go.dev/go.minekube.com/vialite)
[![Discord](https://img.shields.io/discord/633708750032863232.svg?color=%237289da&label=discord)](https://minekube.com/discord)

</div>

`vialite` packages Via-powered Minecraft protocol translation behind a
GraalVM native shared library and a small Go API. Gate can load it
in-process, route selected backend connections through it, and keep
owning frontend auth, events, routing, Connect, and backend login.

Target topology:

```text
player / Bedrock
  -> optional geyserlite
  -> Gate classic
  -> libvialite loaded in-process
  -> backend server
```

The first supported use case is backend version compatibility. If Gate
accepts the player, `vialite` helps Gate speak to backend servers running
different Java protocol versions. It is not a Gate Lite feature and it is
not the first frontend compatibility layer for clients Gate cannot parse.

## What This Repo Provides

| Component | Status |
| --- | --- |
| Go module `go.minekube.com/vialite` | Lifecycle, readiness, artifact lookup, embedded/subprocess runners, backend dial address API |
| Gate adapter `go.minekube.com/vialite/integration/gate` | Gate-shaped config and fakeable lifecycle wrapper |
| Native build scaffold | ViaProxy soft-fork overlay and isolate-thread-aware C ABI contract |
| Release/update loop | CI, release-please, Renovate, checksummed GitHub Release assets |

The current native artifact is an ABI/build scaffold. It proves the
GraalVM shared-library shape and Go loading path, but full Via runtime
translation, backend probing, and forwarding preservation are target
behavior for the next implementation slice.

## Go Quick Start

```go
srv, err := vialite.New(vialite.Options{
    GateProtocol: "auto",
    Backends: []vialite.Backend{
        {
            Name:       "lobby",
            Address:    "127.0.0.1:25566",
            Version:    "auto",
            Forwarding: vialite.ForwardingVelocity,
        },
    },
})
if err != nil {
    return err
}
go func() { _ = srv.Start(ctx) }()
if err := srv.WaitReady(ctx); err != nil {
    return err
}

addr, err := srv.BackendDialAddress("lobby")
if err != nil {
    return err
}
// Gate dials addr instead of the raw backend address.
```

## Local Development

```sh
mise trust && mise install
task setup
task test
task lint
```

The native build path is scaffolded separately:

```sh
task overlay:apply
task build:native
```

## Repository Layout

```text
vialite/
├── go/                   # Go module: go.minekube.com/vialite
│   └── integration/gate/ # Gate-shaped lifecycle adapter
├── build/                # native-image pipeline + Via overlay
├── docs/                 # architecture, Gate integration, troubleshooting
└── .github/workflows/    # CI, native build, release automation
```

See [docs/architecture.md](docs/architecture.md) and
[docs/gate.md](docs/gate.md) for the detailed integration model.
