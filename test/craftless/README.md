# Craftless Vialite CI

This directory tracks the Craftless-based real-client compatibility matrix for
Vialite.

Current Craftless status at the pinned CI ref:

- `:driver-fabric:fabricClientSmoke` launches a real Minecraft `1.21.6` Fabric
  client.
- The smoke task starts a local offline-mode Minecraft server, keeps it alive
  while the client runs, and verifies server-side join, chat, and disconnect
  evidence.
- Vialite CI runs a small Go action command between Craftless and the server:
  Craftless starts the backend server, the action command starts Vialite, and
  the Fabric client connects to Vialite's translated listener.
- The workflow builds the PR's Linux amd64 subprocess executable and points the
  Go action command at that local binary path.

The enabled matrix currently covers the real Craftless client version
`1.21.6`, Vialite subprocess mode, and the newer-client-to-older-backend Via
translation path. Embedded rows stay tracked but disabled until the native
isolate shutdown crash observed after successful join evidence is fixed.
Same-version subprocess coverage stays in the tracked matrix, but is not part of
required PR CI because the cross-version row already proves the real Vialite/Via
path.

Current real rows:

- subprocess Vialite, `1.21.6` client -> `1.21.4` backend

Embedded and older-client rows stay disabled until their blockers are resolved.
Each row collects Craftless client logs/events, Vialite logs, server logs,
gameplay results, and server evidence artifacts.
