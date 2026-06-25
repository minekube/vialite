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

Craftless is the CI runner for the currently supported real-client subset of
this matrix. The Vialite workflow builds PR-native Linux artifacts, starts a
real Minecraft client, joins through Vialite, sends chat, and verifies
server-side join/chat/disconnect evidence.

The tracked target matrix lives in
[`test/craftless/matrix.json`](../test/craftless/matrix.json). Enabled rows
currently cover the Craftless `1.21.6` Fabric client against an older backend
server in subprocess mode. Same-version subprocess coverage remains tracked for
manual or nightly expansion, but required PR CI focuses on the cross-version Via
path. Embedded rows remain tracked but disabled until the native isolate
shutdown crash observed after successful join evidence is fixed. Older client
rows remain disabled until Craftless ships real client drivers for those
versions.
