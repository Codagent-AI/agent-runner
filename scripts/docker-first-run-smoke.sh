#!/usr/bin/env bash
set -euo pipefail

IMAGE="${IMAGE:-homebrew/brew:latest}"
REPO="${REPO-https://github.com/heroku/node-js-getting-started.git}"
WORKDIR="${WORKDIR:-/home/linuxbrew/workspaces}"
PROJECT_DIR="${PROJECT_DIR:-}"
BREW_TAP="${BREW_TAP:-Codagent-AI/tap}"
INSTALL_FROM="${INSTALL_FROM:-brew}"
INSTALL_AGENT_CLIS="${INSTALL_AGENT_CLIS:-1}"
INSTALL_PROJECT_DEPS="${INSTALL_PROJECT_DEPS:-1}"
LAUNCH_AGENT_RUNNER="${LAUNCH_AGENT_RUNNER:-0}"
SMOKE_GIT_USER_NAME="${SMOKE_GIT_USER_NAME:-Agent Runner Smoke}"
SMOKE_GIT_USER_EMAIL="${SMOKE_GIT_USER_EMAIL:-agent-runner-smoke@example.local}"
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
RUNNER_ROOT="$(cd -- "$SCRIPT_DIR/.." && pwd)"

usage() {
  cat <<'USAGE'
Usage: docker-first-run-smoke.sh [--local] [--run]

Runs a manual Docker smoke test for Agent Runner. The container installs Agent
Runner from Homebrew or this checkout, verifies that Agent Runner and its
Homebrew dependencies are on PATH, prepares a fresh sample project, and then
leaves you in a shell so you can run Agent Runner for first-run setup and
onboarding.

Options:
  --local   Build and install agent-runner from this checkout instead of the
            published Homebrew cask. The agent-validator and agent-plugin
            dependencies still install from Homebrew.
  --run     Launch agent-runner automatically after setup.
  -h, --help
            Show this help.

Environment:
  IMAGE                 Docker image to use. Default: homebrew/brew:latest
  REPO                  Git repo to clone for the smoke project.
                        Default: https://github.com/heroku/node-js-getting-started.git
  WORKDIR               Container workspace. Default:
                        /home/linuxbrew/workspaces
  PROJECT_DIR           Project directory name inside WORKDIR. Defaults to the
                        basename of REPO, or agent-runner-first-run-smoke.
  BREW_TAP              Homebrew tap to install from. Default: Codagent-AI/tap
  INSTALL_FROM          Install agent-runner from brew or local. Default: brew
  INSTALL_AGENT_CLIS    Install codex and claude CLIs with npm. Default: 1
  INSTALL_PROJECT_DEPS  Run npm ci/install in the cloned project when possible.
                        Default: 1
  LAUNCH_AGENT_RUNNER   Launch agent-runner after setup. Default: 0
  SMOKE_GIT_USER_NAME   Repo-local Git author name for smoke commits.
                        Default: Agent Runner Smoke
  SMOKE_GIT_USER_EMAIL  Repo-local Git author email for smoke commits.
                        Default: agent-runner-smoke@example.local

Examples:
  scripts/docker-first-run-smoke.sh
  scripts/docker-first-run-smoke.sh --local
  scripts/docker-first-run-smoke.sh --run
  REPO= scripts/docker-first-run-smoke.sh
USAGE
}

while (($#)); do
  case "$1" in
    --local)
      INSTALL_FROM=local
      shift
      ;;
    --run)
      LAUNCH_AGENT_RUNNER=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

docker_args=(
  --rm
  -w /home/linuxbrew
  -e "REPO=$REPO"
  -e "WORKDIR=$WORKDIR"
  -e "PROJECT_DIR=$PROJECT_DIR"
  -e "BREW_TAP=$BREW_TAP"
  -e "INSTALL_FROM=$INSTALL_FROM"
  -e "INSTALL_AGENT_CLIS=$INSTALL_AGENT_CLIS"
  -e "INSTALL_PROJECT_DEPS=$INSTALL_PROJECT_DEPS"
  -e "LAUNCH_AGENT_RUNNER=$LAUNCH_AGENT_RUNNER"
  -e "SMOKE_GIT_USER_NAME=$SMOKE_GIT_USER_NAME"
  -e "SMOKE_GIT_USER_EMAIL=$SMOKE_GIT_USER_EMAIL"
)

if [[ -t 0 && -t 1 ]]; then
  docker_args+=(-it)
fi

if [[ "$INSTALL_FROM" == "local" ]]; then
  docker_args+=(-v "$RUNNER_ROOT:/agent-runner-source:ro")
fi

docker run "${docker_args[@]}" \
  "$IMAGE" \
  bash -lc '
    set -euo pipefail

    require_cmd() {
      local name="$1"
      if ! command -v "$name" >/dev/null 2>&1; then
        echo "Missing expected command on PATH: $name" >&2
        exit 1
      fi
    }

    show_version() {
      local name="$1"
      if "$name" --version >/dev/null 2>&1; then
        "$name" --version
      elif "$name" -version >/dev/null 2>&1; then
        "$name" -version
      else
        echo "$name installed at $(command -v "$name")"
      fi
    }

    project_name_from_repo() {
      local repo="$1"
      if [[ -z "$repo" ]]; then
        printf "%s\n" "agent-runner-first-run-smoke"
        return
      fi
      basename "$repo" .git
    }

    copy_source() {
      local src="$1"
      local dest="$2"
      mkdir -p "$dest"
      tar \
        --exclude ./.git \
        --exclude ./bin \
        --exclude ./coverage.out \
        --exclude ./coverage.html \
        -C "$src" \
        -cf - . | tar -C "$dest" -xf -
    }

    configure_smoke_git_author() {
      git config user.name "$SMOKE_GIT_USER_NAME"
      git config user.email "$SMOKE_GIT_USER_EMAIL"
    }

    install_agent_runner_from_local_source() {
      echo "Installing Agent Runner dependencies from Homebrew..."
      brew install agent-validator agent-plugin go

      echo
      echo "Building and installing local Agent Runner..."
      copy_source /agent-runner-source /tmp/agent-runner-local
      cd /tmp/agent-runner-local
      go build -ldflags "-X main.version=local-dev" -o /home/linuxbrew/.linuxbrew/bin/agent-runner ./cmd/agent-runner
      cd /home/linuxbrew
    }

    install_agent_runner_from_brew() {
      echo "Installing Agent Runner from Homebrew..."
      brew install --cask agent-runner
    }

    require_cmd brew
    brew tap "$BREW_TAP"
    case "$INSTALL_FROM" in
      brew)
        install_agent_runner_from_brew
        ;;
      local)
        install_agent_runner_from_local_source
        ;;
      *)
        echo "Unsupported INSTALL_FROM: $INSTALL_FROM" >&2
        exit 2
        ;;
    esac

    echo
    echo "Verifying Agent Runner and Homebrew dependencies..."
    require_cmd agent-runner
    require_cmd agent-validator
    require_cmd agent-plugin
    show_version agent-runner
    show_version agent-validator
    show_version agent-plugin

    if [[ "$INSTALL_AGENT_CLIS" == "1" ]]; then
      echo
      echo "Installing agent CLIs for first-run setup choices..."
      require_cmd npm
      npm install -g @openai/codex @anthropic-ai/claude-code
      require_cmd codex
      require_cmd claude
      show_version codex
      show_version claude
    fi

    mkdir -p "$WORKDIR"
    cd "$WORKDIR"

    if [[ -z "$PROJECT_DIR" ]]; then
      PROJECT_DIR="$(project_name_from_repo "$REPO")"
    fi

    if [[ -n "$REPO" ]]; then
      echo
      echo "Cloning smoke project..."
      git clone "$REPO" "$PROJECT_DIR"
    else
      echo
      echo "Creating empty smoke project..."
      mkdir -p "$PROJECT_DIR"
      cd "$PROJECT_DIR"
      git init
      cd "$WORKDIR"
    fi

    cd "$PROJECT_DIR"
    configure_smoke_git_author

    if [[ -z "$REPO" && ! -f README.md ]]; then
      printf "# Agent Runner first-run smoke\n" > README.md
      git add README.md
      git commit -m "chore: initial smoke project"
    fi

    if [[ "$INSTALL_PROJECT_DEPS" == "1" && -f package.json ]]; then
      echo
      echo "Installing smoke project dependencies..."
      if [[ -f package-lock.json ]]; then
        npm ci
      else
        npm install
      fi
    fi

    echo
    echo "Setup complete."
    echo "Project: $(pwd)"
    echo "Home:    $HOME"
    echo "Run codex login or claude login first if your onboarding path needs authenticated agents."
    echo

    if [[ "$LAUNCH_AGENT_RUNNER" == "1" ]]; then
      echo "Launching first-run Agent Runner setup. Exit Agent Runner to return to the container shell."
      agent-runner || true
      echo
    fi

    echo "You are in the smoke project. Run agent-runner to start first-run setup, or agent-runner --reset-onboarding to reset onboarding state."
    exec bash -l
  '
