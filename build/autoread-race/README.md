# auto-read race harness

Reproduction and RED/GREEN proof for the backend-channel `autoRead` leak that parks
~0.2% of Gate classic joins in CONFIGURATION: the runtime's backend socket stops being
read, the backend's config head never reaches the client, and nothing is logged.

Run it with:

```bash
build/autoread-race/run.sh          # JAVA/JAVAC can be overridden, netty 4.2.19.Final is fetched once
```

Observed on the lab (4 vCPU, Temurin 21) and reproduced while validating the runtime
(Temurin 17, 8 vCPU, `t_09872d82`):

```text
# lab, Temurin 21
upstream          iterations=50000 autoRead_left_false=16 (0.0320%) restore_errors=0 stale_saved_states=0  exit=1
upstream-widened  iterations=5000  autoRead_left_false=73 (1.4600%) restore_errors=0 stale_saved_states=0  exit=1
overlay-fix       iterations=50000 autoRead_left_false=0  (0.0000%) restore_errors=0 stale_saved_states=0  exit=0

# runtime validation, Temurin 17
upstream          iterations=50000 autoRead_left_false=43 (0.0860%) restore_errors=0 stale_saved_states=0  exit=1
upstream-widened  iterations=5000  autoRead_left_false=88 (1.7600%) restore_errors=0 stale_saved_states=0  exit=1
overlay-fix       iterations=50000 autoRead_left_false=0  (0.0000%) restore_errors=0 stale_saved_states=0  exit=0
```

`restore_errors=0` on every run is the point: the failure is silent, exactly like the
production stall (no `Race condition detected`, no `Tried to restore auto read`, no
pipeline exception, the channel simply never reads again and the kernel receive queue
keeps growing).

## What the harness replays

`src/racer/AutoReadRace.java` builds a real Netty backend channel on event loop A and a
second loop B, then per iteration does what ViaProxy does on `LoginSuccess` when the
client is 1.20.2+:

| step | caller | loop |
| --- | --- | --- |
| `disableAutoRead()` | `CompressionPacketHandler.handleP2S` (`S2CLoginGameProfilePacket` branch) | A (backend) |
| `disableAutoRead()` | `LoginPacketHandler.handleP2S` (`S2CLoginGameProfilePacket` branch) | A (backend) |
| `restoreAutoRead()` | `CompressionPacketHandler` listener of the SetCompression write | B (client c2p) |
| `restoreAutoRead()` | `ConfigurationPacketHandler.handleC2P` listener of the client's LoginAcknowledged write | A (backend) |

The two restores are released together, which is what a loopback client does when it
answers `LoginSuccess` inside the round trip of the `SetCompression` write. Both pop the
same `Stack<Boolean>` channel attribute (`ChannelUtil`, `last-auto-read`) and both write
back their own popped value; when the inner `false` is written last the channel is left
with `autoRead=false` and an empty saved stack, so no later call ever repairs it.

## The fix under test

`../overlay/src/main/java/net/raphimc/viaproxy/proxy/util/ChannelUtil.java` runs every
transition on `channel.eventLoop()`, so pushes and pops are totally ordered and the last
restore always restores the outermost state.
