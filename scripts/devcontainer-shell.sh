#!/usr/bin/env bash
# Starts the eval sandbox devcontainer for interactive Agent Runner development.
# By default it uses the safe devcontainer config; --with-host-config generates
# a local derived config that mounts selected host auth/shell/git files.
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
RUNNER_ROOT="$(cd -- "$SCRIPT_DIR/.." && pwd)"
CONFIG="$RUNNER_ROOT/.devcontainer/eval/devcontainer.json"
REBUILD=0
WITH_HOST_CONFIG=0

usage() {
  cat <<'USAGE'
Usage: devcontainer-shell.sh [--rebuild] [--with-host-config] [command...]

Starts the Agent Runner devcontainer, then executes a command inside it.
With no command, opens an interactive zsh login shell. Commands are executed
through zsh so .sandbox-secrets.env is available consistently.

By default this does not mount host auth, shell, git, or sandbox secret files.
Use --with-host-config only for trusted checkouts where sharing those files with
the container is intentional.

Examples:
  scripts/devcontainer-shell.sh
  scripts/devcontainer-shell.sh --rebuild
  scripts/devcontainer-shell.sh --with-host-config codex exec 'what is 2 + 2?'
  scripts/devcontainer-shell.sh codex --version
  scripts/devcontainer-shell.sh claude --version
  scripts/devcontainer-shell.sh make test
USAGE
}

while (($#)); do
  case "$1" in
    --rebuild)
      REBUILD=1
      shift
      ;;
    --with-host-config)
      WITH_HOST_CONFIG=1
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
      break
      ;;
  esac
done

if (($# == 0)); then
  set -- zsh -lc 'scripts/sandbox-sync-home.sh && exec zsh -l'
else
  command_string=""
  for arg in "$@"; do
    printf -v quoted "%q" "$arg"
    if [[ -z "$command_string" ]]; then
      command_string="$quoted"
    else
      command_string+=" $quoted"
    fi
  done
  set -- zsh -lc "scripts/sandbox-sync-home.sh && { source \"\$HOME/.sandbox-env\" 2>/dev/null || true; }; $command_string"
fi

mkdir -p "$RUNNER_ROOT/artifacts/devcontainer"
if [[ ! -f "$RUNNER_ROOT/.sandbox-secrets.env" ]]; then
  : > "$RUNNER_ROOT/.sandbox-secrets.env"
  chmod 600 "$RUNNER_ROOT/.sandbox-secrets.env"
fi

if [[ "$WITH_HOST_CONFIG" == 1 ]]; then
  CONFIG="$RUNNER_ROOT/artifacts/devcontainer/with-host-config/devcontainer.json"
  mkdir -p "$(dirname -- "$CONFIG")"
  BASE_CONFIG="$RUNNER_ROOT/.devcontainer/eval/devcontainer.json" \
  OUT_CONFIG="$CONFIG" \
  HOST_HOME="$HOME" \
  RUNNER_ROOT="$RUNNER_ROOT" \
  node <<'NODE'
const fs = require("fs");
const path = require("path");

const config = JSON.parse(fs.readFileSync(process.env.BASE_CONFIG, "utf8"));
const hostHome = process.env.HOST_HOME;
const runnerRoot = process.env.RUNNER_ROOT;
const outConfigDir = path.dirname(process.env.OUT_CONFIG);
const optionalHostMounts = [
  [path.join(hostHome, ".codex", "auth.json"), "source=${localEnv:HOME}/.codex/auth.json,target=/host-home/codex/auth.json,type=bind,readonly"],
  [path.join(hostHome, ".codex", "config.toml"), "source=${localEnv:HOME}/.codex/config.toml,target=/host-home/codex/config.toml,type=bind,readonly"],
  [path.join(hostHome, ".claude", ".credentials.json"), "source=${localEnv:HOME}/.claude/.credentials.json,target=/host-home/claude/.credentials.json,type=bind,readonly"],
  [path.join(hostHome, ".claude", "settings.json"), "source=${localEnv:HOME}/.claude/settings.json,target=/host-home/claude/settings.json,type=bind,readonly"],
  [path.join(hostHome, ".claude", "settings.local.json"), "source=${localEnv:HOME}/.claude/settings.local.json,target=/host-home/claude/settings.local.json,type=bind,readonly"],
  [path.join(hostHome, ".zshrc"), "source=${localEnv:HOME}/.zshrc,target=/host-home/shell/.zshrc,type=bind,readonly"],
  [path.join(hostHome, ".zprofile"), "source=${localEnv:HOME}/.zprofile,target=/host-home/shell/.zprofile,type=bind,readonly"],
  [path.join(hostHome, ".gitconfig"), "source=${localEnv:HOME}/.gitconfig,target=/host-home/git/.gitconfig,type=bind,readonly"],
  [path.join(hostHome, ".gitignore"), "source=${localEnv:HOME}/.gitignore,target=/host-home/git/.gitignore,type=bind,readonly"],
  [path.join(hostHome, ".config", "git", "ignore"), "source=${localEnv:HOME}/.config/git/ignore,target=/host-home/git/config-ignore,type=bind,readonly"],
];
const requiredHostMounts = [
  [path.join(runnerRoot, ".sandbox-secrets.env"), "source=${localWorkspaceFolder}/.sandbox-secrets.env,target=/host-home/sandbox-secrets.env,type=bind,readonly"],
];
const hostMounts = [
  ...optionalHostMounts.filter(([source]) => fs.existsSync(source)).map(([, mount]) => mount),
  ...requiredHostMounts.map(([, mount]) => mount),
];

config.name = `${config.name} (host config)`;
config.build = {
  ...config.build,
  dockerfile: path.relative(outConfigDir, path.join(runnerRoot, "docker", "dev", "Dockerfile")),
  context: path.relative(outConfigDir, runnerRoot),
};
config.mounts = (config.mounts || []).map((mount) =>
  mount.startsWith("source=agent-runner-dev-home,target=/workspace/home,")
    ? mount.replace("source=agent-runner-dev-home,", "source=agent-runner-dev-home-host-config,")
    : mount
);
config.mounts.push(...hostMounts);

fs.writeFileSync(process.env.OUT_CONFIG, `${JSON.stringify(config, null, 2)}\n`);
NODE
fi

up_args=(
  --workspace-folder "$RUNNER_ROOT"
  --config "$CONFIG"
)
if [[ "$REBUILD" == 1 ]]; then
  up_args+=(--remove-existing-container)
fi

npx --yes @devcontainers/cli up \
  "${up_args[@]}"

exec npx --yes @devcontainers/cli exec \
  --workspace-folder "$RUNNER_ROOT" \
  --config "$CONFIG" \
  "$@"
