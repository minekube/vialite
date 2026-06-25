# Native Build

The native build reconstitutes ViaProxy from upstream at the pinned ref in
`build/via.version`, overlays the `vialite-native` Gradle subproject, and
builds a GraalVM shared library exposing the vialite C ABI.

```sh
mise run overlay:apply
mise run build:native
```

The overlay is additive. Do not maintain a hard fork of ViaProxy in this
repository.
