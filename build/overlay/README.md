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
- `.../proxy/util/ChannelUtil.java`: auto-read save/restore transitions are moved
  onto the channel's own event loop. Upstream mutates a plain `Stack<Boolean>`
  channel attribute from two different loops (`CompressionPacketHandler`'s
  restore runs in a listener on the client channel's loop, while
  `LoginPacketHandler`/`ConfigurationPacketHandler` push and pop on the backend
  channel's loop), so two restores can interleave, both pop, and the loser's
  `setAutoRead(false)` wins - leaving the backend channel unreadable forever with
  no exception (the ~0.2% silent CONFIGURATION stall: backend socket receive
  queue grows, nothing reaches the client, no `Configuration finished`).
  Semantics are unchanged: same pushed values, same order a single-threaded
  sequence would produce. Repro/proof: `build/autoread-order/run.sh`.
- `src/main/resources/log4j2.xml`: console-only, uncoloured pattern without the
  `ip_redactor` lookup and without the rolling file appenders, so Gate can parse
  the subprocess output line by line and the native image writes no log files.
- `vialite-native/src/main/java/com/minekube/vialite/bridge/VialiteHandshakePacket.java`,
  `.../VialitePacketRegistry.java`, `.../VialiteClient2ProxyChannelInitializer.java`
  (plus the one-line wiring in `VialiteBridge.addBackend`): the client-to-proxy
  handshake is decoded with an address limit of `Short.MAX_VALUE` instead of the
  vanilla 255 characters netminecraft's
  `C2SHandshakingClientIntentionPacket` enforces. Gate classic with
  `forwarding.mode: legacy` appends `host\0ip\0uuid\0<profile properties JSON>` to
  the handshake address (same as BungeeCord), and an authenticated player's Mojang
  profile properties are ~2.8 kB, so the stock limit rejects every online-mode join
  with `DecoderException: The received encoded string buffer length is longer than
  maximum allowed (… > 1020)` before Via translation starts. BungeeCord, Spigot and
  Paper read this field with `Short.MAX_VALUE` for the same reason. This is a
  decode-side limit only; vialite's outgoing handshake is unchanged.
- `vialite-native/.../VialiteBridge.java`: installs the vialite packet registry for
  the per-backend listeners it creates (`VialiteClient2ProxyChannelInitializer`
  instead of `Client2ProxyChannelInitializer`).
- `vialite-native/.../VialitePreConnectGuard.java` (plus `VialiteBridge`'s stage
  marking/`describeRoute` and the arming line in
  `VialiteClient2ProxyChannelInitializer`): bounds and names the window between
  accepting Gate's bridge connection and dialling the backend. Without it an
  accepted connection whose handshake never reaches `Client2ProxyHandler` (a
  pipeline/event-loop stall, a flow-control hold) logged nothing, dialled no
  backend and left Gate's join parked on a read that a Gate without `gate#1197`
  never times out — the 2026-09-27 Foxof7207 case. The guard runs on its own
  daemon thread, reports the backend, configured address, resolved address and
  the stage reached, and closes the connection so the join fails instead of
  hanging. It adds no translation behaviour and does not change any ViaProxy
  deadline.
  The same guard also owns the CONFIGURATION stage, the hop's second silent
  class: a join that logs `Connected successfully! Switching to CONFIGURATION
  state` and then writes nothing toward the client (backend socket still being
  read, client socket empty, no kick). `LoginSuccessWatch` arms the stage when
  the runtime writes LoginSuccess toward Gate's bridge connection (1.20.2+
  clients only — older clients have no CONFIGURATION state), a socket-level
  counter records clientbound and serverbound bytes, and after
  `configuration_deadline_ms` (default 15000 ms, below Gate's 30000 ms default
  read timeout) with zero clientbound bytes — or an unwritable channel — the
  connection is closed with one named diagnosis: player, backend with configured
  and resolved address, stage, bytes in both directions and elapsed time. A
  healthy join is never touched: the configuration burst (plugin.Message plus
  ~40 kB of registry data) arrives within milliseconds of LoginSuccess.
  The arm is deferred to the channel's next event-loop task: the login-success
  write carries one promise through the whole outbound pipeline, the encoder
  turns it into the bytes the socket sends, and `TrafficCounter` (at the head)
  registers its write-completion listener on that same promise after
  `LoginSuccessWatch` (at the tail) did. Promise listeners run in registration
  order, so an inline arm would count the LoginSuccess write itself as
  clientbound progress and the stage would never fire for a real stall — found
  by the lab smoke in the native image (kanban t_bc6083f6).
  `build/configstage-watch/run.sh` proves the branches on real Netty, with the
  packet encoder kept in the test pipeline so this arm/count ordering is
  covered.

When bumping `build/via.version`, re-derive these files from the new upstream
revision and re-apply only the deltas above — do not keep stale copies, since
upstream API changes (renamed classes, new accessors) break the build.
