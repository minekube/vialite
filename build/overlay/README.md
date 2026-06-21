# Overlay

Files in this directory are copied into `build/.work/ViaProxy` by
`build/apply-overlay.sh`.

The overlay adds the `vialite-native` Gradle subproject and Java bridge
classes that expose the GraalVM C ABI.
