#!/usr/bin/env bash
# RED/GREEN proof for the silent CONFIGURATION stall caused by a raced auto-read
# restore.
#
#   red/ChannelUtil.java   verbatim upstream viaproxy/proxy/util/ChannelUtil.java
#                          (pinned ViaProxy 63eea3ae, unmodified)
#   fix/ChannelUtil.java   the event-loop-confined overlay replacement that
#                          build/overlay ships
#
# Cases:
#   red        the schedule the runtime produced, parked so the losing restore writes
#              last -> upstream must leave the channel unreadable (exit 1)
#   healthy    control: one restore only -> must stay readable (exit 0)
#   stress     production call shape, 20000 iterations, no hooks ->
#              upstream leaks some channels, the fix never does
#
# Usage: build/autoread-order/run.sh            (JAVA/JAVAC overridable)
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
NETTY_VERSION="${NETTY_VERSION:-4.2.19.Final}"
JAVA="${JAVA:-java}"
JAVAC="${JAVAC:-javac}"
LIB="$HERE/lib"
mkdir -p "$LIB"

for artifact in netty-common netty-buffer netty-transport netty-resolver; do
  jar="$LIB/$artifact-$NETTY_VERSION.jar"
  if [ ! -f "$jar" ]; then
    echo "fetching $artifact-$NETTY_VERSION"
    curl -sSfL -o "$jar" \
      "https://repo1.maven.org/maven2/io/netty/$artifact/$NETTY_VERSION/$artifact-$NETTY_VERSION.jar" || exit 3
  fi
done
CP="$(ls "$LIB"/*.jar | tr '\n' ':')"

status=0

run() { # <label> <channel-util> <expected-exit|any> <args...>
  local label="$1" util="$2" expected="$3"; shift 3
  local out rc
  out="$(mktemp -d)"
  "$JAVAC" -nowarn -cp "$CP" -d "$out" "$util" "$HERE/src/racer/AutoReadOrder.java" || { echo "$label: COMPILE FAILED"; return 3; }
  echo "--- $label ---"
  "$JAVA" -cp "$CP:$out" racer.AutoReadOrder "$@"
  rc=$?
  rm -rf "$out"
  echo "$label exit=$rc (expected $expected)"
  if [ "$expected" != "any" ] && [ "$rc" != "$expected" ]; then
    echo "MISMATCH: $label exited $rc, expected $expected"
    status=1
  fi
}

run "upstream/red (traced-runtime schedule)" "$HERE/red/ChannelUtil.java" 1 red
run "upstream/healthy-control"               "$HERE/red/ChannelUtil.java" 0 healthy
# Observation only: the un-parked race is stochastic (13/20000 and 0/20000 on
# successive runs of the same code), which is exactly why the deterministic
# schedule above is the RED test.
run "upstream/stress (observation)"         "$HERE/red/ChannelUtil.java" any stress 20000
# No "fix/red": the parked-stack hook is typed as java.util.Stack (what upstream
# uses), while the fix keeps its saved states in a Deque, so the same hook cannot
# be installed for it. The fix's GREEN is the stress case below, where the
# event-loop confinement removes the interleaving the red case forces.
run "fix/stress"                            "$HERE/fix/ChannelUtil.java" 0 stress 20000

if [ "$status" -eq 0 ]; then
  echo "OK: upstream leaks the backend channel on the traced schedule (RED), the event-loop-confined fix does not (GREEN)"
fi
exit "$status"
