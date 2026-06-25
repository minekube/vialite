# Craftwright Vialite CI

This directory tracks the Craftwright-based compatibility matrix Vialite wants
to run once Craftwright exposes a real Minecraft Java client runner.

Current Craftwright status at the pinned CI ref:

- `mcw clients api` starts a local session API.
- The TypeScript SDK can create fake clients, connect them, send chat, and wait
  for fake chat events.
- The public real-client bridge is still plan/evidence level, so Vialite must
  not treat this as a real join proof yet.

The GitHub workflow validates the current Craftwright API/SDK contract and this
target matrix. The matrix rows stay disabled until Craftwright can launch real
clients from CI and assert server-side join/chat evidence.

When Craftwright adds the real client runner, this should become the required
join matrix:

- embedded Vialite, latest client -> latest backend
- embedded Vialite, latest client -> older backend
- embedded Vialite, older client -> latest backend
- subprocess Vialite, latest client -> latest backend
- subprocess Vialite, latest client -> older backend
- subprocess Vialite, older client -> latest backend

Each real row should collect Craftwright client logs/events, Gate logs, Vialite
logs, server logs, and the disconnect reason when a join fails.
