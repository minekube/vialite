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

## Recommended compatibility matrix

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
- run real-client joins for at least `1.21.10`, `1.21.11`, and `26.1.2`.

Craftwright is the intended CI runner for this matrix once its real client
backend is available. The scenarios should publish client logs, disconnect
reasons, screenshots, and Gate/vialite logs as CI artifacts.

The tracked target matrix lives in
[`test/craftwright/matrix.json`](../test/craftwright/matrix.json). Until
Craftwright exposes a real client runner, Vialite CI only validates the current
Craftwright API/SDK contract and keeps those join rows disabled.
