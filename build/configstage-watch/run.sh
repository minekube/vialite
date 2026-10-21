#!/usr/bin/env bash
# Behavioural proof for the CONFIGURATION stage of VialitePreConnectGuard.
#
#   overlay  the shipped guard (build/overlay/.../VialitePreConnectGuard.java)
#            -> every branch PASS, java exit 0                                  GREEN
#   red      the same guard with only the stage timer removed (red/...)
#            -> "fires-when-nothing-clientbound" FAIL, everything else PASS     RED
#
# The RED half is the point: it shows the fire-on-silence case can see the defect
# instead of passing vacuously. A non-zero exit from this script means an
# expectation below was not met.
#
# Needs a JDK 17+ and Netty jars. The jars are copied from NETTY_JAR_CACHE when
# that directory already has them, otherwise downloaded from Maven Central once
# into ./lib (git-ignored).
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
GUARD="$ROOT/build/overlay/vialite-native/src/main/java/com/minekube/vialite/bridge/VialitePreConnectGuard.java"
MUTANT="$HERE/red/VialitePreConnectGuard.java"
NETTY_VERSION="${NETTY_VERSION:-4.2.19.Final}"
LIB="$HERE/lib"
WORK="${WORK:-$(mktemp -d)}"

JAVAC="${JAVAC:-javac}"
JAVA="${JAVA:-java}"
if ! command -v "$JAVAC" >/dev/null 2>&1; then
  for candidate in "${JAVA_HOME:-}" /opt/data/jdk; do
    if [ -n "$candidate" ] && [ -x "$candidate/bin/javac" ]; then
      JAVAC="$candidate/bin/javac"
      JAVA="$candidate/bin/java"
      break
    fi
  done
fi
if ! command -v "$JAVAC" >/dev/null 2>&1; then
  echo "no javac found; set JAVA_HOME or JAVAC" >&2
  exit 2
fi

mkdir -p "$LIB"
for artifact in netty-common netty-buffer netty-transport netty-resolver; do
  jar="$LIB/$artifact-$NETTY_VERSION.jar"
  if [ -f "$jar" ]; then
    continue
  fi
  if [ -n "${NETTY_JAR_CACHE:-}" ] && [ -f "$NETTY_JAR_CACHE/$artifact-$NETTY_VERSION.jar" ]; then
    cp "$NETTY_JAR_CACHE/$artifact-$NETTY_VERSION.jar" "$jar"
  else
    curl -sSfL -o "$jar" \
      "https://repo1.maven.org/maven2/io/netty/$artifact/$NETTY_VERSION/$artifact-$NETTY_VERSION.jar"
  fi
done
CP="$(ls "$LIB"/*.jar | tr '\n' ':')"

STUBS=()
while IFS= read -r stub; do STUBS+=("$stub"); done < <(find "$HERE/stubs" -name '*.java' | sort)

# run_suite <label> <guard-source>
run_suite() {
  local label="$1"
  local guard="$2"
  local classes="$WORK/$label-classes"
  local rc
  mkdir -p "$classes"
  if ! "$JAVAC" -nowarn -cp "$CP" -d "$classes" \
      "${STUBS[@]}" \
      "$HERE/src/com/minekube/vialite/bridge/ConfigurationStageProbe.java" \
      "$guard" >"$WORK/$label.javac.log" 2>&1; then
    echo "FAIL $label: javac rejected the sources" >&2
    cat "$WORK/$label.javac.log" >&2
    echo 2 >"$WORK/$label.rc"
    return 0
  fi
  set +e
  "$JAVA" -cp "$CP:$classes" com.minekube.vialite.bridge.ConfigurationStageProbe \
      >"$WORK/$label.out" 2>&1
  rc=$?
  set -e
  echo "$rc" >"$WORK/$label.rc"
  echo "--- suite $label (java exit $rc)"
  sed 's/^/    /' "$WORK/$label.out"
}

# result <label> <case> -> PASS | FAIL | missing
result() {
  awk -v want="$2" '$1 == "RESULT" && $2 == want { print $3 }' "$WORK/$1.out" 2>/dev/null || true
}

status=0
expect_result() { # <label> <case> <PASS|FAIL>
  local got
  got="$(result "$1" "$2")"
  if [ "${got:-missing}" != "$3" ]; then
    echo "MISMATCH: $1 / $2 was '${got:-missing}', expected $3"
    status=1
  fi
}
expect_rc() { # <label> <exit-code>
  local got
  got="$(cat "$WORK/$1.rc" 2>/dev/null || echo missing)"
  if [ "$got" != "$2" ]; then
    echo "MISMATCH: $1 exited '$got', expected $2"
    status=1
  fi
}

run_suite overlay "$GUARD"
run_suite red "$MUTANT"

# The shipped guard must clear every branch.
expect_result overlay fires-when-nothing-clientbound PASS
expect_result overlay does-not-fire-when-clientbound-bytes-arrive PASS
expect_result overlay does-not-arm-for-pre-1.20.2-clients PASS
expect_result overlay diagnosis-names-stage-player-and-both-addresses PASS
expect_rc overlay 0

# Removing only the timer must break only the fire-on-silence branch.
expect_result red fires-when-nothing-clientbound FAIL
expect_result red does-not-fire-when-clientbound-bytes-arrive PASS
expect_result red does-not-arm-for-pre-1.20.2-clients PASS
expect_result red diagnosis-names-stage-player-and-both-addresses PASS
expect_rc red 1

if [ "$status" -ne 0 ]; then
  echo "EXPECTATIONS NOT MET"
  exit 1
fi
echo "GREEN: overlay guard clears every branch; the timer-less mutant fails only the fire-on-silence branch."
