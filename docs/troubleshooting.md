# Troubleshooting

## Native Library Not Found

Embedded mode resolves `libvialite.so` in this order:

1. `Options.LibraryPath`
2. `$VIALITE_LIBRARY`
3. embedded asset built with `-tags vialite_embed`
4. system library paths
5. GitHub Release auto-download

Set `Options.Offline=true` to disable auto-download.

## Native Binary Not Found

Subprocess mode resolves `vialite` in this order:

1. `Options.BinaryPath`
2. `$VIALITE_BINARY`
3. embedded asset built with `-tags vialite_embed`
4. `$PATH`
5. GitHub Release auto-download

## Backend Detection

The current native artifact is an ABI scaffold. The Go config already
supports `version: auto`, but real backend status probing is target
runtime behavior for the next implementation slice.

Once the Via runtime wiring is active, a backend using `version: auto`
should be probed through the backend status response. Detection can fail
if the backend is offline, blocks status pings, or speaks a protocol Via
does not recognize.

Use an explicit backend version when status pings are intentionally
blocked.

## Forwarding Problems

The Go config surface carries forwarding mode now. Full packet-level
forwarding preservation depends on the Via runtime wiring after this ABI
scaffold.

If backend identity is wrong once runtime forwarding is enabled, verify
that the backend's configured forwarding mode matches Gate:

- Gate `legacy` forwarding requires `forwarding: legacy`
- Gate `velocity` forwarding requires `forwarding: velocity`
- No forwarding requires `forwarding: none`

The first Go integration tests cover config conversion for these modes.
