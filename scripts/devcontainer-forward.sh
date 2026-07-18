#!/usr/bin/env bash
# Forwards a devcontainer-local browser server to the host. It handles the common
# case where Vite listens on 127.0.0.1 inside the container by creating an inner
# container forward, then a host-published Docker sidecar.
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
RUNNER_ROOT="$(cd -- "$SCRIPT_DIR/.." && pwd)"
HOST_AUTH_CONFIG="$RUNNER_ROOT/artifacts/devcontainer/with-host-auth/devcontainer.json"
BASE_CONFIG="$RUNNER_ROOT/.devcontainer/eval/devcontainer.json"
DRY_RUN=0
CONTAINER_ID=""
CONTAINER_IP=""
FORWARD_NAME=""

usage() {
  cat <<'USAGE'
Usage: devcontainer-forward.sh [options] HOST_PORT:CONTAINER_PORT [PATH]

Forwards a server running inside the Agent Runner devcontainer to localhost on
the host. PATH defaults to / and is used for the readiness check and printed URL.

Examples:
  scripts/devcontainer-forward.sh 5174:5173 /agent-tool-loop
  scripts/devcontainer-forward.sh --dry-run --container-id abc --container-ip 172.17.0.4 5174:5173 /agent-tool-loop

Options:
  --dry-run                  Print commands without running them
  --container-id ID          Use an explicit devcontainer id
  --container-ip IP          Use an explicit devcontainer bridge IP
  --name NAME                Docker sidecar container name
  -h, --help                 Show this help
USAGE
}

while (($#)); do
  case "$1" in
    --dry-run)
      DRY_RUN=1
      shift
      ;;
    --container-id)
      CONTAINER_ID="${2:-}"
      shift 2
      ;;
    --container-ip)
      CONTAINER_IP="${2:-}"
      shift 2
      ;;
    --name)
      FORWARD_NAME="${2:-}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    --)
      shift
      break
      ;;
    *)
      break
      ;;
  esac
done

if (($# < 1 || $# > 2)); then
  usage >&2
  exit 2
fi

PORT_SPEC="$1"
CHECK_PATH="${2:-/}"
if [[ "$CHECK_PATH" != /* ]]; then
  CHECK_PATH="/$CHECK_PATH"
fi

if [[ "$PORT_SPEC" != *:* ]]; then
  echo "Expected HOST_PORT:CONTAINER_PORT, got: $PORT_SPEC" >&2
  exit 2
fi
HOST_PORT="${PORT_SPEC%%:*}"
CONTAINER_PORT="${PORT_SPEC##*:}"
for port in "$HOST_PORT" "$CONTAINER_PORT"; do
  if [[ ! "$port" =~ ^[0-9]+$ || "$port" -lt 1 || "$port" -gt 65535 ]]; then
    echo "Invalid port: $port" >&2
    exit 2
  fi
done

if ((HOST_PORT <= 55535)); then
  INNER_PORT=$((HOST_PORT + 10000))
else
  INNER_PORT=$((HOST_PORT - 10000))
fi
if [[ -z "$FORWARD_NAME" ]]; then
  FORWARD_NAME="agent-runner-devcontainer-forward-$HOST_PORT"
fi

print_command() {
  printf '+'
  printf ' %q' "$@"
  printf '\n'
}

run_or_print() {
  if [[ "$DRY_RUN" == 1 ]]; then
    print_command "$@"
  else
    "$@"
  fi
}

detect_config() {
  if [[ -f "$HOST_AUTH_CONFIG" ]]; then
    printf '%s' "$HOST_AUTH_CONFIG"
  else
    printf '%s' "$BASE_CONFIG"
  fi
}

detect_container_id() {
  local config="$1"
  docker ps \
    --filter "label=devcontainer.local_folder=$RUNNER_ROOT" \
    --filter "label=devcontainer.config_file=$config" \
    --format '{{.ID}}' | head -n 1
}

CONFIG="$(detect_config)"
if [[ -z "$CONTAINER_ID" ]]; then
  CONTAINER_ID="$(detect_container_id "$CONFIG")"
fi
if [[ -z "$CONTAINER_ID" ]]; then
  echo "No running Agent Runner devcontainer found. Start it first with scripts/devcontainer-shell.sh --with-host-config." >&2
  exit 2
fi
if [[ -z "$CONTAINER_IP" ]]; then
  CONTAINER_IP="$(docker inspect -f '{{range.NetworkSettings.Networks}}{{if .IPAddress}}{{.IPAddress}}{{println}}{{end}}{{end}}' "$CONTAINER_ID" | head -n 1)"
fi
if [[ -z "$CONTAINER_IP" ]]; then
  echo "Could not resolve container IP for $CONTAINER_ID" >&2
  exit 2
fi

INNER_SCRIPT=$(cat <<INNER
set -euo pipefail
command -v socat >/dev/null
pidfile=/tmp/agent-runner-forward-$INNER_PORT.pid
if [[ -f "\$pidfile" ]]; then
  old_pid="\$(cat "\$pidfile" 2>/dev/null || true)"
  if [[ -n "\$old_pid" ]]; then
    kill "\$old_pid" 2>/dev/null || true
  fi
fi
nohup socat TCP-LISTEN:$INNER_PORT,fork,reuseaddr,bind=0.0.0.0 TCP:127.0.0.1:$CONTAINER_PORT >/tmp/agent-runner-forward-$INNER_PORT.log 2>&1 &
echo \$! > "\$pidfile"
INNER
)

run_or_print docker exec -u pwuser "$CONTAINER_ID" bash -lc "$INNER_SCRIPT"
if [[ "$DRY_RUN" == 1 ]]; then
  print_command docker rm -f "$FORWARD_NAME"
else
  docker rm -f "$FORWARD_NAME" >/dev/null 2>&1 || true
fi
run_or_print docker run -d --name "$FORWARD_NAME" -p "127.0.0.1:$HOST_PORT:$HOST_PORT" alpine/socat \
  "TCP-LISTEN:$HOST_PORT,fork,reuseaddr" "TCP:$CONTAINER_IP:$INNER_PORT"

URL="http://127.0.0.1:$HOST_PORT$CHECK_PATH"
if [[ "$DRY_RUN" == 1 ]]; then
  print_command curl -fsS --max-time 2 "$URL"
  printf 'Open: http://localhost:%s%s\n' "$HOST_PORT" "$CHECK_PATH"
  exit 0
fi

for _ in {1..20}; do
  if curl -fsS --max-time 2 "$URL" >/dev/null; then
    printf 'Open: http://localhost:%s%s\n' "$HOST_PORT" "$CHECK_PATH"
    exit 0
  fi
  sleep 0.5
done

echo "Forward started, but $URL did not become reachable." >&2
exit 1
