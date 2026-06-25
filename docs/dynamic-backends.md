# Dynamic backend runtime

Gate integrations that register temporary backends, such as Connect tunnel
sessions, need a single warmed Via runtime per proxy process. Starting a new
native Via subprocess for every backend registration is too expensive for that
shape: each join pays process startup, Via protocol registration, mapping load,
and extra memory.

The preferred runtime for dynamic backends is embedded mode:

1. Gate starts one `libvialite` runtime.
2. The runtime initializes Via once.
3. Static backends are registered during startup.
4. Dynamic backends call `vialite_add_backend`.
5. Removed dynamic backends call `vialite_remove_backend`.

Subprocess mode remains useful as a portable fallback and for static Gate
configs, but it should not be the default for high-churn dynamic session
registrations.

## Local Prism smoke tests

Use `scripts/prism-smoke.sh` to launch a real Prism Launcher instance, join a
server, and fail/pass from the Minecraft client log.

This is an interim local harness. It is useful on a developer machine that
already has Prism instances installed, but it should not become the long-term
CI contract. The CI target is Craftwright once its real client backend is
available: scenario-driven real Minecraft client automation with stable CLI
output, artifacts, and cleanup.

```bash
scripts/prism-smoke.sh 1.21.11 127.0.0.1:25665
scripts/prism-smoke.sh 1.21.10 old.localhost:25669
scripts/prism-smoke.sh 26.1.2 new.localhost:25665
```

The script uses offline mode by default:

```bash
scripts/prism-smoke.sh 1.21.11 127.0.0.1:25665 VialiteSmoke
```

To use an online Prism profile instead:

```bash
PRISM_SMOKE_PROFILE=RoboFlax2 scripts/prism-smoke.sh 1.21.11 127.0.0.1:25665
```

Useful environment variables:

- `PRISM_BIN`: Prism binary path. Defaults to `prismlauncher`.
- `PRISM_ROOT`: Prism app data root. Defaults to
  `~/Library/Application Support/PrismLauncher`.
- `PRISM_SMOKE_TIMEOUT`: join timeout in seconds. Defaults to `90`.
- `PRISM_SMOKE_KEEP_CLIENT=1`: keep the launched Minecraft client running.

The script watches only new bytes written to
`instances/<instance>/minecraft/logs/latest.log`, so stale successful joins do
not make a test pass.

## Recommended local matrix

For Gate/Via compatibility:

- latest client -> latest backend, Via enabled
- latest client -> older backend, Via enabled
- older client -> latest backend, Via enabled
- same matrix with Via disabled as a control

For Connect-like dynamic backends:

- start Gate with `via.enabled=true`, `via.mode=embedded`, and no static
  servers;
- register one temporary backend per join;
- assert only one `vialite` runtime exists while multiple sessions are created;
- run Prism smoke joins for at least `1.21.10`, `1.21.11`, and `26.1.2`.

When Craftwright can launch real clients, replace the Prism commands with
Craftwright scenarios that cover the same matrix and publish client logs,
disconnect reasons, screenshots, and Gate/vialite logs as CI artifacts.
