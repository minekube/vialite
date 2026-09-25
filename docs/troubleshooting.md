# Troubleshooting

## Native Library Not Found

Embedded mode resolves `libvialite.so` in this order:

1. `Options.LibraryPath`
2. `$VIALITE_LIBRARY`
3. embedded asset built with `-tags vialite_embed`
4. system library paths
5. GitHub Release auto-download

If `Options.Version` is empty, `auto`, or `latest`, auto-download resolves the
latest stable `minekube/vialite` release and then verifies the downloaded
artifact against that release's `checksums.txt`. Set `Options.Version` to an
exact tag, such as `v0.3.1`, to pin the artifact.

Custom mirrors are asked for their own latest release first, so a mirror
deployment follows new releases exactly like GitHub does. A mirror that only
serves files (no `/latest` JSON with a `tag_name` field) cannot answer, and
that is the single case where vialite falls back to the compiled-in
`DefaultMirrorVersion` instead of failing to start. It logs a warning naming
the fallback version and the remedy:

```text
level=WARN msg="vialite: mirror does not report a latest release; using the pinned fallback runtime" mirror=... fallbackVersion=v0.3.1 hint="pin via.version to v0.3.1 to make this deliberate, or serve <mirror>/latest with {\"tag_name\":\"...\"} to follow your mirror's newest release"
```

An explicit `auto`/`latest` against a mirror that cannot answer stays a hard
error: the operator asked for newest, and silently downgrading would be worse.

Which runtime is actually in use is logged once per start, with its version and
provenance, so support does not have to infer it from the runtime's own output:

```text
INFO msg="vialite: resolved runtime" kind=binary source=download version=v0.3.1 path=/home/gate/.cache/vialite/v0.3.1/<sha>/vialite-linux-amd64 url=https://github.com/minekube/vialite/releases/download/v0.3.1/vialite-linux-amd64
INFO msg="vialite: resolved runtime" kind=binary source=cache version=v0.3.1 path=...
INFO msg="vialite: resolved runtime" kind=binary source=binaryPath path=/usr/local/bin/vialite
```

`source` is one of `download`, `cache`, `binaryPath`, `env:VIALITE_BINARY`,
`embedded`, `path` (binary) or `libraryPath`, `env:VIALITE_LIBRARY`,
`embedded`, `system` (library).

Set `Options.Offline=true` to disable auto-download.

## Native Binary Not Found

Subprocess mode resolves `vialite` in this order:

1. `Options.BinaryPath`
2. `$VIALITE_BINARY`
3. embedded asset built with `-tags vialite_embed`
4. `$PATH`
5. GitHub Release auto-download

GitHub Release auto-download supports Linux amd64/arm64 subprocess binaries and
the Windows amd64 subprocess binary. If `Options.Version` is empty, `auto`, or
`latest`, the latest stable release is resolved before checksums and the binary
are downloaded. Set `Options.BinaryPath` to bypass download and use a local
binary.

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
