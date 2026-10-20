# CONFIGURATION stage watch proof

`run.sh` is the behavioural proof for the CONFIGURATION stage of
`build/overlay/vialite-native/.../VialitePreConnectGuard.java`: the branch that
turns the hop's second silent stall class into a named disconnect.

## What it runs

The real guard source is compiled against stub versions of its ViaProxy
neighbours (`Logger`, `ProxyConnection`, the login-success packet, the backend
route descriptor) and real Netty 4.2.x jars. Each case drives the actual pipeline
handlers on an `EmbeddedChannel`, at the positions
`VialiteClient2ProxyChannelInitializer` installs them (byte counter at the
socket end, `LoginSuccessWatch` at the packet end):

| Case | What it asserts |
| --- | --- |
| `fires-when-nothing-clientbound` | LoginSuccess written to the client and nothing after it: the connection is closed and one ERROR names the stage, the player, the configured and resolved backend address, the bytes seen in both directions and the elapsed time. |
| `does-not-fire-when-clientbound-bytes-arrive` | The same, followed by the ~40 kB configuration burst: the join is left alone. This is the "must not fire on a healthy join" branch. |
| `does-not-arm-for-pre-1.20.2-clients` | A client without a CONFIGURATION state is never armed. |
| `diagnosis-names-stage-player-and-both-addresses` | The unwritable branch of the diagnosis text. |

## RED half

`red/VialitePreConnectGuard.java` is the shipped guard with only the stage timer
removed. It must fail exactly one case — `fires-when-nothing-clientbound` — and
leave the other three passing. That is what makes the fire-on-silence case a real
test instead of a vacuous one: a guard that never fires cannot satisfy it.

## Expected result

```text
--- suite overlay (java exit 0)
    ... every case PASS ...
ALL PASS
--- suite red (java exit 1)
    RESULT fires-when-nothing-clientbound FAIL
    ... other cases PASS ...
GREEN: overlay guard clears every branch; the timer-less mutant fails only the fire-on-silence branch.
```

`run.sh` exits non-zero if any of those expectations is not met.

## Requirements

A JDK 17+ and the four Netty jars (`netty-common`, `netty-buffer`,
`netty-transport`, `netty-resolver`). `run.sh` copies them from
`NETTY_JAR_CACHE` when that directory already holds them and otherwise downloads
them once from Maven Central into `lib/`, which is git-ignored.

```sh
NETTY_JAR_CACHE=/path/to/netty-jars ./run.sh
```

## Scope

This proves the guard's own behaviour. It does not prove a real Gate -> vialite
-> Paper join, which needs the lab (`stall-nat-lab`, namespace
`moxy-bedrock-e2e`) or a disposable pod; no production edge is involved.
