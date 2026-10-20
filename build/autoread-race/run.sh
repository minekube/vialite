#!/usr/bin/env bash
# RED/GREEN proof for the backend-channel autoRead leak (silent CONFIGURATION stall).
#
#   red/ChannelUtil.java               verbatim upstream viaproxy/proxy/util/ChannelUtil.java
#   red-deterministic/ChannelUtil.java same, window between pop and write-back widened
#   ../overlay/src/main/java/.../ChannelUtil.java  the fix shipped with this patch
#
# Expected outcome (a non-zero exit means the harness itself disagrees with the expectation):
#   upstream             -> some channels left with autoRead == false and no exception  (RED)
#   upstream + widened   -> same, at a much higher rate, deterministically               (RED)
#   overlay fix          -> no channel ever left unreadable                              (GREEN)
#
# Needs a JDK 11+ (javac) and network access to Maven Central on first run.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
NETTY_VERSION="${NETTY_VERSION:-4.2.19.Final}"
JAVA="${JAVA:-java}"
JAVAC="${JAVAC:-javac}"
LIB="$HERE/lib"
mkdir -p "$LIB"

for artifact in netty-common netty-buffer netty-transport netty-resolver; do
  jar="$LIB/$artifact-$NETTY_VERSION.jar"
  if [ ! -f "$jar" ]; then
    curl -sSfL -o "$jar" "https://repo1.maven.org/maven2/io/netty/$artifact/$NETTY_VERSION/$artifact-$NETTY_VERSION.jar"
  fi
done
CP="$(ls "$LIB"/*.jar | tr '\n' ':')"

run_case() { # <name> <channel-util-source> <iterations>
  local name="$1" util="$2" iterations="$3"
  local out rc
  out="$(mktemp -d)"
  "$JAVAC" -nowarn -cp "$CP" -d "$out" "$util" "$HERE/src/racer/AutoReadRace.java"
  set +e
  "$JAVA" -cp "$CP:$out" racer.AutoReadRace "$iterations"
  rc=$?
  set -e
  rm -rf "$out"
  echo "$name exit=$rc"
  printf '%s %s\n' "$name" "$rc" >> "$RESULTS"
}

RESULTS="$(mktemp)"
trap 'rm -f "$RESULTS"' EXIT

run_case upstream "$HERE/red/ChannelUtil.java" 50000
run_case upstream-widened "$HERE/red-deterministic/ChannelUtil.java" 5000
run_case overlay-fix "$HERE/../overlay/src/main/java/net/raphimc/viaproxy/proxy/util/ChannelUtil.java" 50000

status=0
check() { # <case-name> <expected-exit>
  local actual
  actual="$(awk -v n="$1" '$1 == n { print $2 }' "$RESULTS")"
  if [ "$actual" != "$2" ]; then
    echo "MISMATCH: $1 exited $actual, expected $2"
    status=1
  fi
}
check upstream 1
check upstream-widened 1
check overlay-fix 0

if [ "$status" -eq 0 ]; then
  echo "OK: upstream leaks autoRead (RED), overlay fix does not (GREEN)"
fi
exit "$status"
