#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORK="$ROOT/build/.work"
SRC="$WORK/ViaProxy"
REF="$(tr -d '[:space:]' < "$ROOT/build/via.version")"

mkdir -p "$WORK"
if [ ! -d "$SRC/.git" ]; then
  git clone https://github.com/RaphiMC/ViaProxy.git "$SRC"
fi

git -C "$SRC" fetch --tags origin
git -C "$SRC" checkout --detach "$REF"
git -C "$SRC" reset --hard "$REF"
git -C "$SRC" clean -fdx

cp -R "$ROOT/build/overlay/." "$SRC/"

settings="$SRC/settings.gradle"
if [ ! -f "$settings" ]; then
  settings="$SRC/settings.gradle.kts"
fi

if ! grep -q 'include(":vialite-native")' "$settings"; then
  printf '\ninclude(":vialite-native")\n' >> "$settings"
fi

echo "ViaProxy overlay ready at $SRC ($REF)"
