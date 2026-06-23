#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/prism-smoke.sh <instance> <server> [offline-player]

Launches a Prism Launcher instance, joins a server, and watches the instance
latest.log for a successful join or a disconnect/error.

Environment:
  PRISM_BIN               Prism binary path, default: prismlauncher
  PRISM_ROOT              Prism app root, default: ~/Library/Application Support/PrismLauncher
  PRISM_SMOKE_TIMEOUT     Seconds to wait, default: 90
  PRISM_SMOKE_PROFILE     Prism account profile name. If set, uses online profile instead of --offline.
  PRISM_SMOKE_KEEP_CLIENT Set to 1 to leave the launched client running.
EOF
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

if [[ $# -lt 2 ]]; then
  usage >&2
  exit 2
fi

instance="$1"
server="$2"
player="${3:-VialiteSmoke}"
timeout="${PRISM_SMOKE_TIMEOUT:-90}"
root="${PRISM_ROOT:-$HOME/Library/Application Support/PrismLauncher}"
prism="${PRISM_BIN:-prismlauncher}"
instance_dir="$root/instances/$instance"
log_file="$instance_dir/minecraft/logs/latest.log"
launcher_log="$(mktemp -t prism-smoke-launcher.XXXXXX.log)"
watch_log="$(mktemp -t prism-smoke-client.XXXXXX.log)"

if [[ ! -d "$instance_dir" ]]; then
  echo "Prism instance not found: $instance" >&2
  exit 2
fi

if ! command -v "$prism" >/dev/null 2>&1; then
  echo "Prism binary not found: $prism" >&2
  exit 2
fi

file_size() {
  if stat -f%z "$1" >/dev/null 2>&1; then
    stat -f%z "$1"
  else
    stat -c%s "$1"
  fi
}

existing_pids="$(pgrep -f "$instance_dir" || true)"

cleanup() {
  rm -f "$launcher_log" "$watch_log"
  if [[ "${PRISM_SMOKE_KEEP_CLIENT:-0}" == "1" ]]; then
    return
  fi
  while IFS= read -r pid; do
    [[ -z "$pid" ]] && continue
    if ! grep -qx "$pid" <<<"$existing_pids"; then
      kill "$pid" >/dev/null 2>&1 || true
    fi
  done < <(pgrep -f "$instance_dir" || true)
}
trap cleanup EXIT

offset=0
if [[ -f "$log_file" ]]; then
  offset="$(file_size "$log_file")"
fi

args=(--launch "$instance" --server "$server")
if [[ -n "${PRISM_SMOKE_PROFILE:-}" ]]; then
  args+=(--profile "$PRISM_SMOKE_PROFILE")
else
  args+=(--offline "$player")
fi

"$prism" "${args[@]}" >"$launcher_log" 2>&1 &
launcher_pid=$!

deadline=$((SECONDS + timeout))
success_re='joined the game|Loaded [0-9]+ advancements'
failure_re='Connection Lost|Failed to connect|Disconnected|multiplayer\.disconnect|Outdated|Incompatible|Can'\''t connect|Internal Exception'

while (( SECONDS < deadline )); do
  if ! kill -0 "$launcher_pid" >/dev/null 2>&1; then
    if ! wait "$launcher_pid"; then
      echo "Prism launcher exited before the client joined." >&2
      tail -80 "$launcher_log" >&2 || true
      exit 1
    fi
  fi

  if [[ -f "$log_file" ]]; then
    size="$(file_size "$log_file")"
    if (( size < offset )); then
      offset=0
    fi
    if (( size > offset )); then
      dd if="$log_file" bs=1 skip="$offset" count=$((size - offset)) 2>/dev/null >>"$watch_log" || true
      offset="$size"
    fi
    if grep -Eiq "$failure_re" "$watch_log"; then
      echo "Prism smoke failed for $instance -> $server" >&2
      grep -Ein "$failure_re" "$watch_log" | tail -20 >&2 || true
      exit 1
    fi
    if grep -Eq "$success_re" "$watch_log"; then
      echo "Prism smoke passed for $instance -> $server"
      grep -E "$success_re" "$watch_log" | tail -5
      exit 0
    fi
  fi
  sleep 1
done

echo "Timed out waiting for Prism smoke result: $instance -> $server" >&2
tail -120 "$watch_log" >&2 || true
tail -80 "$launcher_log" >&2 || true
exit 1
