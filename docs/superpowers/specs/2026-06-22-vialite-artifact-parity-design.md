# vialite Artifact Parity Design

## Context

Gate can now import and start `vialite`, but local Via-enabled startup on macOS
fails because the `vialite` Go module only auto-downloads Linux shared
libraries and the release workflow currently builds only the Linux amd64 shared
library path. That is weaker than `geyserlite`, whose release shape supports
Linux amd64/arm64 embedded mode, Linux amd64/arm64 subprocess mode, and Windows
amd64 subprocess mode.

The goal of this slice is distribution and runtime artifact parity with
`geyserlite`. It does not claim the Via translation runtime is complete; it
makes the artifact pipeline ready for testing and consuming the runtime once the
native bridge is fully implemented.

## Supported Artifact Matrix

Every GitHub release should publish:

- `libvialite-linux-amd64.so`
- `libvialite-linux-arm64.so`
- `vialite-linux-amd64`
- `vialite-linux-arm64`
- `vialite-windows-amd64.exe`
- `libvialite.h`
- `checksums.txt`

Linux shared libraries are the supported embedded-mode artifacts. Linux and
Windows executables are the supported subprocess-mode artifacts. macOS remains a
manual developer path through `LibraryPath`, `BinaryPath`, `VIALITE_LIBRARY`, or
`VIALITE_BINARY` until there is a separate decision to support signed/notarized
Darwin artifacts.

## Go Runtime Behavior

`locateLibrary` keeps the existing resolution order: explicit path, environment,
embedded asset, system library path, auto-download, then error.

`locateBinary` keeps the existing resolution order: explicit path, environment,
embedded asset, `PATH`, auto-download, then error.

Auto-download should resolve these assets:

- embedded mode on `linux/amd64`: `libvialite-linux-amd64.so`
- embedded mode on `linux/arm64`: `libvialite-linux-arm64.so`
- subprocess mode on `linux/amd64`: `vialite-linux-amd64`
- subprocess mode on `linux/arm64`: `vialite-linux-arm64`
- subprocess mode on `windows/amd64`: `vialite-windows-amd64.exe`

Unsupported runtime combinations should fail with errors that tell the operator
to provide `BinaryPath` or `LibraryPath` manually.

The `vialite_embed` build tag should support Linux amd64 and Linux arm64 for
both the subprocess binary and shared library assets. Non-Linux embed builds
remain explicitly unsupported.

## CI And Release

`native-image.yml` should build Linux artifacts on native GitHub-hosted runners:

- `ubuntu-latest` for amd64
- `ubuntu-24.04-arm` for arm64

Each Linux job should stage both the executable and the shared library, assert
the `vialite_*` C ABI declarations in `libvialite.h`, and upload an artifact
named by architecture.

A Windows job should build the subprocess executable on `windows-latest` and
upload `vialite-windows-amd64.exe`. Windows embedded DLL support is out of
scope.

`release.yml` should follow the `geyserlite` pattern: download matching
`native-image.yml` artifacts for the release input commit, generate
`checksums.txt`, and publish the collected assets to the GitHub release. It
should not rebuild only one architecture during release.

## Tests

Go tests should lock the asset resolver behavior for all supported and
unsupported combinations. Existing download checksum tests should continue to
prove that assets are fetched by runtime-specific names and verified through
`checksums.txt`.

Workflow changes are validated by YAML lint and by running the Go tests locally.
Full native artifact builds require GitHub Actions or a Linux/Windows builder.

## Non-Goals

- Implementing full Via protocol translation.
- Adding macOS release artifacts.
- Adding Windows embedded DLL support.
- Adding Rust bindings.
- Changing Gate's `via` configuration.

## Success Criteria

- `go test ./...` passes in `vialite/go`.
- Asset lookup tests prove parity with the `geyserlite` support matrix.
- CI workflows build and publish all listed release assets.
- Gate users on supported Linux hosts can enable `via.enabled` without manually
  supplying a `LibraryPath` once a release with these assets exists.
