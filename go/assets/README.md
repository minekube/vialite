# Embedded vialite Assets

Per-arch native artifacts used by the Go package. Linux artifacts can be
embedded into the host binary with `-tags vialite_embed` after fetching or
copying release assets into this directory.

Expected Linux embed layout:

- `vialite-linux-amd64`
- `libvialite-linux-amd64.so`
- `vialite-linux-arm64`
- `libvialite-linux-arm64.so`

GitHub Release auto-download supports:

- `libvialite-linux-amd64.so`
- `libvialite-linux-arm64.so`
- `vialite-linux-amd64`
- `vialite-linux-arm64`
- `vialite-windows-amd64.exe`
- `checksums.txt`

Windows uses subprocess mode with the released `.exe`; Windows embedded DLL
support is intentionally not shipped yet.
