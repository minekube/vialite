# Overlay

Files in this directory are copied into `build/.work/ViaProxy` by
`build/apply-overlay.sh`, after the script has checked out the ViaProxy ref
pinned in `build/via.version`.

The overlay adds the `vialite-native` Gradle subproject and Java bridge
classes that expose the GraalVM C ABI, plus a small set of upstream files that
are shipped in an adapted form.

## Intentional deltas from upstream

Keep this list in sync with the overlay contents. Every delta exists so the
runtime works as a native image behind Gate classic; nothing here may change
protocol behaviour.

- `src/main/java/net/raphimc/viaproxy/ViaProxy.java`,
  `.../ui/ViaProxyWindow.java`: rewritten by `apply-overlay.sh` — the
  `LambdaMetaFactoryGenerator`/`JavaBypass.TRUSTED_LOOKUP` event generator is
  replaced with `ReflectionGenerator` (the trusted-lookup path is unavailable
  in a native image). The script hard-fails if the symbol survives.
- `.../protocoltranslator/ProtocolTranslator.java`: the `ViaBedrock` platform is
  never initialized, and `.../protocoltranslator/impl/ViaProxyPlatformLoader.java`:
  the ViaBedrock `NettyPipelineProvider` is not registered. vialite is the
  backend-side Java translator for Gate classic; Bedrock clients reach Gate
  through geyserlite, and ViaBedrock's runtime-only initialization does not
  belong in this image.
- `.../proxy/packethandler/LoginPacketHandler.java`: the login key pair is
  created lazily instead of in a static initializer, so the native image does
  not generate an RSA key pair at build time.
- `.../saves/SaveManager.java`, `.../saves/impl/AccountsSave.java`: save
  loading avoids reflection streams (`RStream`), tolerates missing plugin
  managers and absent save files, and keeps the field list explicit.
- `src/main/resources/log4j2.xml`: console-only, uncoloured pattern without the
  `ip_redactor` lookup and without the rolling file appenders, so Gate can parse
  the subprocess output line by line and the native image writes no log files.

When bumping `build/via.version`, re-derive these files from the new upstream
revision and re-apply only the deltas above — do not keep stale copies, since
upstream API changes (renamed classes, new accessors) break the build.
