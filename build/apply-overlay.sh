#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORK="$ROOT/build/.work"
SRC="$WORK/ViaProxy"
REF="$(tr -d '[:space:]' < "$ROOT/build/via.version")"

mkdir -p "$WORK"
if [ ! -d "$SRC/.git" ]; then
  git clone https://github.com/ViaVersion/ViaProxy.git "$SRC"
fi

git -C "$SRC" fetch --tags origin
git -C "$SRC" checkout --detach "$REF"
git -C "$SRC" reset --hard "$REF"
git -C "$SRC" clean -fdx

cp -R "$ROOT/build/overlay/." "$SRC/"

for java_file in \
  "$SRC/src/main/java/net/raphimc/viaproxy/ViaProxy.java" \
  "$SRC/src/main/java/net/raphimc/viaproxy/ui/ViaProxyWindow.java"; do
  perl -0pi -e 's#import net\.lenni0451\.lambdaevents\.generator\.LambdaMetaFactoryGenerator;\r?\n#import net.lenni0451.lambdaevents.generator.ReflectionGenerator;\n#g; s#import net\.lenni0451\.reflect\.JavaBypass;\r?\n##g; s#new LambdaMetaFactoryGenerator\(JavaBypass\.TRUSTED_LOOKUP\)#new ReflectionGenerator()#g' "$java_file"
done

if grep -R "JavaBypass.TRUSTED_LOOKUP" \
  "$SRC/src/main/java/net/raphimc/viaproxy/ViaProxy.java" \
  "$SRC/src/main/java/net/raphimc/viaproxy/ui/ViaProxyWindow.java"; then
  echo "ViaProxy overlay still references JavaBypass.TRUSTED_LOOKUP" >&2
  exit 1
fi

settings="$SRC/settings.gradle"
if [ ! -f "$settings" ]; then
  settings="$SRC/settings.gradle.kts"
fi

if ! grep -q 'include(":vialite-native")' "$settings"; then
  printf '\ninclude(":vialite-native")\n' >> "$settings"
fi

echo "ViaProxy overlay ready at $SRC ($REF)"
