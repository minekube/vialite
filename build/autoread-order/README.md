# auto-read order test (silent CONFIGURATION stall)

Deterministic RED test for the stall that parks a small share of cold Gate
classic joins in CONFIGURATION on the translation hop, silently: the backend
channel stops being read and nothing is thrown or logged.

## The statement under test

`net.raphimc.viaproxy.proxy.util.ChannelUtil` keeps the saved auto-read states in
per-channel state and mutates it without any synchronisation:

```java
disableAutoRead(channel):      stack.push(channel.config().isAutoRead()); channel.config().setAutoRead(false);
restoreAutoRead(channel):      channel.config().setAutoRead(stack.pop());          // pop and write-back are not atomic
```

The four call sites do not agree on a thread:

| call site | transition | runs on |
| --- | --- | --- |
| `CompressionPacketHandler:47` | disable | backend (p2s) event loop |
| `LoginPacketHandler:191` | disable (nested) | backend (p2s) event loop |
| `CompressionPacketHandler:51` | restore (SetCompression write listener) | **client (c2p) event loop** |
| `ConfigurationPacketHandler:47` | restore (LoginAcknowledged listener) | backend (p2s) event loop |

Two restores overlapping pop the same stack and each writes back whatever it
popped. When the losing restore's `setAutoRead(false)` lands last, the backend
channel stops reading for good: the saved stack is empty, so nothing can repair
it, and both guard branches in `restoreAutoRead` are unreachable (they only fire
when `autoRead` is already `true` or the state was never saved), so nothing is
thrown or logged.

## What the runtime actually did

In-runtime trace of a cold join through a native build of this runtime (backend
socket `ESTABLISHED`, receive queue growing, nothing read again):

```text
+76ms  p2s loop: autoread DISABLE pushed=true  depth=1 autoRead=false   CompressionPacketHandler:47
+76ms  p2s loop: autoread DISABLE pushed=false depth=2 autoRead=false   LoginPacketHandler:191
+77ms  c2p loop: autoread RESTORE popped=false depth=1 autoRead=false   CompressionPacketHandler:51  (parked)
+79ms  p2s loop: autoread RESTORE popped=true  depth=0 autoRead=true    ConfigurationPacketHandler:47
+79ms  c2p loop: setAutoRead(false) lands last  -> autoRead=false, saved stack empty
```

After that transition the runtime issued zero reads on the backend channel for
the rest of the join's 30 s configuration budget, while the peer kept sending
CONFIGURATION keep-alives into the unread socket. The kernel socket view agrees:
receive queue monotonic, transmit count 0, state `ESTABLISHED`.

## Cases in `run.sh`

| case | class under test | expectation |
| --- | --- | --- |
| `red` | verbatim upstream `red/ChannelUtil.java` | the channel is left with `autoRead=false` and an empty saved stack -> exit 1 (**RED**) |
| `healthy` | verbatim upstream | both restores serialized -> channel still reading -> exit 0 (control) |
| `stress` | verbatim upstream | production call shape, 20000 iterations, no hooks (observation only: 2-13 leaks of 20000) |
| `stress` | `fix/ChannelUtil.java` | 0 leaks -> exit 0 (**GREEN**) |

`red` is deterministic: the test pre-installs its own `Stack` under the very
attribute `ChannelUtil` uses (`last-auto-read`), so the shipped implementation
runs unchanged and calls the real `Stack` API; the test only decides *when* the
losing restore's write-back is allowed to land. No modification, widening or
re-ordering of the class under test is involved.

`fix/ChannelUtil.java` is the same event-loop-confined file `build/overlay`
ships (every transition runs on `channel.eventLoop()`, so pushes and pops are
totally ordered and the last restore restores the outermost state).

## Running

```bash
build/autoread-order/run.sh            # JAVA/JAVAC overridable, netty 4.2.19.Final fetched once
```

Observed (2026-10-21, Temurin 21 and Temurin 17):

```text
upstream/red (traced-runtime schedule)  final: autoRead=false saved_depth=0 -> exit 1   (RED)
upstream/healthy-control                final: autoRead=true  saved_depth=0 -> exit 0
upstream/stress (observation)           iterations=20000 autoRead_left_false=4 (0.0200%) -> exit 1
fix/stress                              iterations=20000 autoRead_left_false=0 (0.0000%) -> exit 0 (GREEN)
```

The upstream `stress` count varies between runs (0-13 of 20000) because it
exercises the un-parked race; the parked `red` schedule is the deterministic
statement, and it fails on every run of unmodified upstream code.
