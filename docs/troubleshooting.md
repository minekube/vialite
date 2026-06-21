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

## Backend Detection Failed

When a backend uses `version: auto`, `vialite` probes the backend status
response. Detection fails if the backend is offline, blocks status pings,
or speaks a protocol Via does not recognize.

Use an explicit backend version when status pings are intentionally
blocked.

## Forwarding Problems

If backend identity is wrong, verify that the backend's configured
forwarding mode matches Gate:

- Gate `legacy` forwarding requires `forwarding: legacy`
- Gate `velocity` forwarding requires `forwarding: velocity`
- No forwarding requires `forwarding: none`

The first integration tests cover all three modes.
