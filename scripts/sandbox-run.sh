#!/usr/bin/env bash
# Runs a one-shot command in the eval sandbox image after building the current
# Agent Runner checkout inside the container. This script owns Docker launch,
# explicit env/auth pass-through, workspace setup, and artifact mounting.
set -euo pipefail

IMAGE="${IMAGE:-agent-runner-dev:local}"
DOCKERFILE="${DOCKERFILE:-docker/dev/Dockerfile}"
ARTIFACT_DIR="${ARTIFACT_DIR:-}"
DRY_RUN=0
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
RUNNER_ROOT="$(cd -- "$SCRIPT_DIR/.." && pwd)"
DEFAULT_SECRETS_FILE="${SANDBOX_SECRETS_FILE:-$RUNNER_ROOT/.sandbox-secrets.env}"
LOAD_DEFAULT_SECRETS=1
MOUNT_CODEX_AUTH=0
MOUNT_CLAUDE_AUTH=0
ENV_VARS=()
ENV_FILES=()
DOCKER_RUN_ARGS=()

usage() {
  cat <<'USAGE'
Usage: sandbox-run.sh [options] -- <command>

Runs a command in the Agent Runner eval sandbox. The current checkout is mounted
read-only, copied inside the container, built there, and exposed on PATH as
agent-runner before the command runs.

Options:
  --dry-run              Print the docker commands instead of running them.
  --image IMAGE          Docker image tag. Default: agent-runner-dev:local
  --dockerfile PATH      Dockerfile path relative to repo root.
                          Default: docker/dev/Dockerfile
  --artifact-dir PATH    Host directory mounted at /artifacts. Default:
                          artifacts/sandbox-runs/<timestamp>
  --no-default-secrets   Do not automatically load .sandbox-secrets.env.
  --secrets-file PATH    Load a sandbox secrets env file. Default, when present:
                          .sandbox-secrets.env
  --env NAME             Pass through one named environment variable if set.
                          Repeatable. Values are not printed in dry-run output.
  --env-file PATH        Read simple NAME=value or export NAME=value entries
                          from a local env file and pass those variable names.
                          The file is parsed, not sourced.
  --mount-codex-auth     Mount host ~/.codex auth/config files read-only for
                          subscription-based Codex CLI auth. Files are copied
                          into writable container home before the command runs.
  --mount-claude-auth    Mount host ~/.claude auth/settings files read-only for
                          subscription-based Claude Code auth. Files are copied
                          into writable container home before the command runs.
  --docker-run-arg ARG   Extra docker run argument. Repeatable. Use this for
                          explicit opt-ins such as --ipc=host.
  -h, --help             Show this help.
USAGE
}

shell_quote() {
  printf "%q" "$1"
}

print_command() {
  local first=1
  for arg in "$@"; do
    if [[ "$first" == 1 ]]; then
      first=0
    else
      printf " "
    fi
    shell_quote "$arg"
  done
  printf "\n"
}

timestamp() {
  date -u +"%Y%m%dT%H%M%SZ"
}

while (($#)); do
  case "$1" in
    --dry-run)
      DRY_RUN=1
      shift
      ;;
    --image)
      IMAGE="${2:?missing value for --image}"
      shift 2
      ;;
    --dockerfile)
      DOCKERFILE="${2:?missing value for --dockerfile}"
      shift 2
      ;;
    --artifact-dir)
      ARTIFACT_DIR="${2:?missing value for --artifact-dir}"
      shift 2
      ;;
    --no-default-secrets)
      LOAD_DEFAULT_SECRETS=0
      shift
      ;;
    --secrets-file)
      ENV_FILES+=("${2:?missing value for --secrets-file}")
      shift 2
      ;;
    --env)
      ENV_VARS+=("${2:?missing value for --env}")
      shift 2
      ;;
    --env-file)
      ENV_FILES+=("${2:?missing value for --env-file}")
      shift 2
      ;;
    --docker-run-arg)
      DOCKER_RUN_ARGS+=("${2:?missing value for --docker-run-arg}")
      shift 2
      ;;
    --mount-codex-auth)
      MOUNT_CODEX_AUTH=1
      shift
      ;;
    --mount-claude-auth)
      MOUNT_CLAUDE_AUTH=1
      shift
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
      echo "Unknown option: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if (($# == 0)); then
  echo "Missing command. Use -- <command>." >&2
  usage >&2
  exit 2
fi

load_env_file() {
  local file="$1"
  local line name value
  if [[ ! -f "$file" ]]; then
    echo "Env file not found: $file" >&2
    exit 2
  fi
  while IFS= read -r line || [[ -n "$line" ]]; do
    line="${line#"${line%%[![:space:]]*}"}"
    line="${line%"${line##*[![:space:]]}"}"
    [[ -z "$line" || "$line" == \#* ]] && continue
    if [[ "$line" == export[[:space:]]* ]]; then
      line="${line#export}"
      line="${line#"${line%%[![:space:]]*}"}"
    fi
    if [[ "$line" != *=* ]]; then
      echo "Invalid env file line in $file: expected NAME=value" >&2
      exit 2
    fi
    name="${line%%=*}"
    value="${line#*=}"
    name="${name%"${name##*[![:space:]]}"}"
    if [[ ! "$name" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]]; then
      echo "Invalid env var name in $file: $name" >&2
      exit 2
    fi
    export "$name=$value"
    ENV_VARS+=("$name")
  done < "$file"
}

if [[ "$LOAD_DEFAULT_SECRETS" == 1 && -f "$DEFAULT_SECRETS_FILE" ]]; then
  load_env_file "$DEFAULT_SECRETS_FILE"
fi

for file in "${ENV_FILES[@]+"${ENV_FILES[@]}"}"; do
  load_env_file "$file"
done

if [[ -z "$ARTIFACT_DIR" ]]; then
  ARTIFACT_DIR="$RUNNER_ROOT/artifacts/sandbox-runs/$(timestamp)"
elif [[ "$ARTIFACT_DIR" != /* ]]; then
  ARTIFACT_DIR="$RUNNER_ROOT/$ARTIFACT_DIR"
fi
mkdir -p "$ARTIFACT_DIR"

DOCKERFILE_ABS="$RUNNER_ROOT/$DOCKERFILE"
if [[ ! -f "$DOCKERFILE_ABS" ]]; then
  echo "Dockerfile not found: $DOCKERFILE" >&2
  exit 2
fi

AGENT_RUNNER_SOURCE_COMMIT="${AGENT_RUNNER_SOURCE_COMMIT:-$(git -C "$RUNNER_ROOT" rev-parse HEAD 2>/dev/null || true)}"
AGENT_RUNNER_SOURCE_DIRTY="${AGENT_RUNNER_SOURCE_DIRTY:-$(if git -C "$RUNNER_ROOT" diff --quiet --ignore-submodules -- 2>/dev/null && git -C "$RUNNER_ROOT" diff --cached --quiet --ignore-submodules -- 2>/dev/null; then echo false; else echo true; fi)}"
export AGENT_RUNNER_SOURCE_COMMIT AGENT_RUNNER_SOURCE_DIRTY

bootstrap=$(cat <<'BOOTSTRAP'
set -euo pipefail
mkdir -p /workspace/bin "$HOME" /tmp/agent-runner-local
if [[ -x /agent-runner-source/scripts/sandbox-sync-home.sh ]]; then
  /agent-runner-source/scripts/sandbox-sync-home.sh
fi
tar \
  --exclude ./.git \
  --exclude ./bin \
  --exclude ./coverage.out \
  --exclude ./coverage.html \
  --exclude ./artifacts \
  --exclude ./worktrees \
  -C /agent-runner-source \
  -cf - . | tar -C /tmp/agent-runner-local -xf -
cd /tmp/agent-runner-local
go build -ldflags "-X main.version=local-dev" -o /workspace/bin/agent-runner ./cmd/agent-runner
cd /workspace
BOOTSTRAP
)
if (($# == 1)); then
  container_script="${bootstrap}"$'\n'"$1"
  container_command=(bash -lc "$container_script")
else
  container_script="${bootstrap}"$'\n''exec "$@"'
  container_command=(bash -lc "$container_script" -- "$@")
fi

build_cmd=(docker build -t "$IMAGE" -f "$DOCKERFILE" "$RUNNER_ROOT")
run_cmd=(
  docker run
  --rm
  --init
  --shm-size=1g
  -w /workspace
  -e CI=1
  -e HOME=/workspace/home
  -e AGENT_RUNNER_SOURCE_COMMIT
  -e AGENT_RUNNER_SOURCE_DIRTY
  -v "$RUNNER_ROOT:/agent-runner-source:ro"
  -v "$ARTIFACT_DIR:/artifacts"
)

for name in "${ENV_VARS[@]+"${ENV_VARS[@]}"}"; do
  if [[ ! "$name" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]]; then
    echo "Invalid env var name: $name" >&2
    exit 2
  fi
  if [[ -n "${!name+x}" ]]; then
    run_cmd+=(-e "$name")
  fi
done

for arg in "${DOCKER_RUN_ARGS[@]+"${DOCKER_RUN_ARGS[@]}"}"; do
  run_cmd+=("$arg")
done

add_required_file_mount() {
  local source="$1"
  local target="$2"
  local label="$3"
  if [[ ! -f "$source" ]]; then
    echo "$label auth file not found: $source" >&2
    exit 2
  fi
  run_cmd+=(--mount "type=bind,source=$source,target=$target,readonly")
}

add_optional_file_mount() {
  local source="$1"
  local target="$2"
  if [[ -f "$source" ]]; then
    run_cmd+=(--mount "type=bind,source=$source,target=$target,readonly")
  fi
}

if [[ "$MOUNT_CODEX_AUTH" == 1 ]]; then
  add_required_file_mount "$HOME/.codex/auth.json" "/host-home/codex/auth.json" "Codex"
  add_optional_file_mount "$HOME/.codex/config.toml" "/host-home/codex/config.toml"
fi

if [[ "$MOUNT_CLAUDE_AUTH" == 1 ]]; then
  add_required_file_mount "$HOME/.claude/.credentials.json" "/host-home/claude/.credentials.json" "Claude"
  add_optional_file_mount "$HOME/.claude/settings.json" "/host-home/claude/settings.json"
  add_optional_file_mount "$HOME/.claude/settings.local.json" "/host-home/claude/settings.local.json"
fi

run_cmd+=("$IMAGE" "${container_command[@]}")

if [[ "$DRY_RUN" == 1 ]]; then
  print_command "${build_cmd[@]}"
  print_command "${run_cmd[@]}"
  exit 0
fi

"${build_cmd[@]}"
"${run_cmd[@]}"
