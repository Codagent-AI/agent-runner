#!/usr/bin/env bash
set -euo pipefail

REPO="${REPO:-https://github.com/Codagent-AI/and-scene.git}"
FIXTURE_REF="${FIXTURE_REF:-origin/eval/create-and-scene-spec-only}"
REFERENCE_REF="${REFERENCE_REF:-origin/change/create-and-scene}"
ARTIFACT_DIR="${ARTIFACT_DIR:-}"
DRY_RUN=0
PROOF_BROWSER=0
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
RUNNER_ROOT="$(cd -- "$SCRIPT_DIR/.." && pwd)"
SANDBOX_RUNNER="${SANDBOX_RUNNER:-$SCRIPT_DIR/sandbox-run.sh}"
ENV_ARGS=()
ENV_FILE_ARGS=()
AUTH_ARGS=()

usage() {
  cat <<'USAGE'
Usage: eval-and-scene.sh --proof-browser [options]

Runs the narrow and-scene browser/container proof. This is not the full scored
Agent Runner eval harness.

Credential posture: no host credentials are inherited by default. Pass only
short-lived, repo-scoped credentials with --env, for example --env GITHUB_TOKEN
for a private fixture clone, or use the sandbox runner's default
.sandbox-secrets.env file. Any env secret passed through is readable by
processes inside the container.

Options:
  --proof-browser        Run the container/browser proof.
  --dry-run              Print the sandbox command instead of running it.
  --artifact-dir PATH    Host artifact directory. Default:
                          artifacts/evals/and-scene-proof/<timestamp>
  --repo URL             and-scene repository URL.
  --fixture-ref REF      Spec-only fixture ref.
                          Default: origin/eval/create-and-scene-spec-only
  --reference-ref REF    Implemented/reference ref for npm run verify.
                          Default: origin/change/create-and-scene
  --env NAME             Pass through one named environment variable.
                          Repeatable.
  --env-file PATH        Read simple NAME=value or export NAME=value entries
                          from a local env file and pass those variable names.
                          The file is parsed by sandbox-run.sh, not sourced.
  --mount-codex-auth     Forward subscription-based Codex auth files into the
                          sandbox via sandbox-run.sh.
  --mount-claude-auth    Forward subscription-based Claude Code auth files into
                          the sandbox via sandbox-run.sh.
  -h, --help             Show this help.
USAGE
}

timestamp() {
  date -u +"%Y%m%dT%H%M%SZ"
}

shell_quote() {
  printf "%q" "$1"
}

while (($#)); do
  case "$1" in
    --proof-browser)
      PROOF_BROWSER=1
      shift
      ;;
    --dry-run)
      DRY_RUN=1
      shift
      ;;
    --artifact-dir)
      ARTIFACT_DIR="${2:?missing value for --artifact-dir}"
      shift 2
      ;;
    --repo)
      REPO="${2:?missing value for --repo}"
      shift 2
      ;;
    --fixture-ref)
      FIXTURE_REF="${2:?missing value for --fixture-ref}"
      shift 2
      ;;
    --reference-ref)
      REFERENCE_REF="${2:?missing value for --reference-ref}"
      shift 2
      ;;
    --env)
      ENV_ARGS+=(--env "${2:?missing value for --env}")
      shift 2
      ;;
    --env-file)
      ENV_FILE_ARGS+=(--env-file "${2:?missing value for --env-file}")
      shift 2
      ;;
    --mount-codex-auth)
      AUTH_ARGS+=(--mount-codex-auth)
      shift
      ;;
    --mount-claude-auth)
      AUTH_ARGS+=(--mount-claude-auth)
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

if [[ "$PROOF_BROWSER" != 1 ]]; then
  echo "Only --proof-browser is implemented in this scoped substrate proof." >&2
  usage >&2
  exit 2
fi

if [[ -z "$ARTIFACT_DIR" ]]; then
  ARTIFACT_DIR="$RUNNER_ROOT/artifacts/evals/and-scene-proof/$(timestamp)"
elif [[ "$ARTIFACT_DIR" != /* ]]; then
  ARTIFACT_DIR="$RUNNER_ROOT/$ARTIFACT_DIR"
fi

REPO_Q="$(shell_quote "$REPO")"
FIXTURE_REF_Q="$(shell_quote "$FIXTURE_REF")"
REFERENCE_REF_Q="$(shell_quote "$REFERENCE_REF")"

proof_script=$(cat <<PROOF
set -euo pipefail
mkdir -p /artifacts/logs /workspace/runs
if [[ " \${NODE_OPTIONS:-} " != *" --dns-result-order="* ]]; then
  export NODE_OPTIONS="\${NODE_OPTIONS:-} --dns-result-order=ipv4first"
fi

configure_github_https_auth() {
  local token="\${GITHUB_TOKEN:-\${GH_TOKEN:-}}"
  if [ -n "\$token" ]; then
    {
      printf '%s\n' '#!/usr/bin/env sh'
      printf '%s\n' 'case "\$1" in'
      printf '%s\n' '  *Username*) printf "%s\n" x-access-token ;;'
      printf '%s\n' '  *Password*) printf "%s\n" "\${GITHUB_TOKEN:-\${GH_TOKEN:-}}" ;;'
      printf '%s\n' '  *) printf "\n" ;;'
      printf '%s\n' 'esac'
    } > "\$HOME/.git-askpass"
    chmod 700 "\$HOME/.git-askpass"
    export GIT_ASKPASS="\$HOME/.git-askpass"
    export GIT_TERMINAL_PROMPT=0
  fi
}

write_metadata() {
  jq -n \\
    --arg repo $REPO_Q \\
    --arg fixture_ref $FIXTURE_REF_Q \\
    --arg reference_ref $REFERENCE_REF_Q \\
    --arg started_at "\${STARTED_AT}" \\
    --arg ended_at "\$(date -u +"%Y-%m-%dT%H:%M:%SZ")" \\
    --arg agent_runner_commit "\${AGENT_RUNNER_SOURCE_COMMIT:-}" \\
    --arg agent_runner_dirty "\${AGENT_RUNNER_SOURCE_DIRTY:-}" \\
    --arg fixture_commit "\${FIXTURE_COMMIT:-}" \\
    --arg reference_commit "\${REFERENCE_COMMIT:-}" \\
    --arg node_version "\$(node --version 2>/dev/null || true)" \\
    --arg npm_version "\$(npm --version 2>/dev/null || true)" \\
    --arg playwright_browsers_path "\${PLAYWRIGHT_BROWSERS_PATH:-}" \\
    --arg exit_code "\${EXIT_CODE:-0}" \\
    '{
      repo: \$repo,
      fixture_ref: \$fixture_ref,
      reference_ref: \$reference_ref,
      started_at: \$started_at,
      ended_at: \$ended_at,
      agent_runner_commit: \$agent_runner_commit,
      agent_runner_dirty: (\$agent_runner_dirty == "true"),
      fixture_commit: \$fixture_commit,
      reference_commit: \$reference_commit,
      cli_versions: {
        node: \$node_version,
        npm: \$npm_version
      },
      playwright_browsers_path: \$playwright_browsers_path,
      exit_code: (\$exit_code | tonumber)
    }' > /artifacts/proof-metadata.json
}

STARTED_AT="\$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
EXIT_CODE=0
trap 'EXIT_CODE=\$?; write_metadata; exit \$EXIT_CODE' EXIT

configure_github_https_auth

git clone $REPO_Q /workspace/runs/fixture 2>&1 | tee /artifacts/logs/fixture-clone.log
cd /workspace/runs/fixture
git fetch origin 2>&1 | tee -a /artifacts/logs/fixture-clone.log
git checkout $FIXTURE_REF_Q 2>&1 | tee -a /artifacts/logs/fixture-clone.log
FIXTURE_COMMIT="\$(git rev-parse HEAD)"
npm ci 2>&1 | tee /artifacts/logs/fixture-npm-ci.log
npm run build 2>&1 | tee /artifacts/logs/fixture-build.log

git clone $REPO_Q /workspace/runs/reference 2>&1 | tee /artifacts/logs/reference-clone.log
cd /workspace/runs/reference
git fetch origin 2>&1 | tee -a /artifacts/logs/reference-clone.log
git checkout $REFERENCE_REF_Q 2>&1 | tee -a /artifacts/logs/reference-clone.log
REFERENCE_COMMIT="\$(git rev-parse HEAD)"
npm ci 2>&1 | tee /artifacts/logs/reference-npm-ci.log
npm run build 2>&1 | tee /artifacts/logs/reference-build.log
npm run verify 2>&1 | tee /artifacts/logs/reference-verify.log

echo "and-scene browser proof passed" | tee /artifacts/tier1-result.txt
PROOF
)

sandbox_args=(--artifact-dir "$ARTIFACT_DIR")
if [[ "$DRY_RUN" == 1 ]]; then
  sandbox_args=(--dry-run "${sandbox_args[@]}")
fi
sandbox_args+=("${ENV_ARGS[@]+"${ENV_ARGS[@]}"}")
sandbox_args+=("${ENV_FILE_ARGS[@]+"${ENV_FILE_ARGS[@]}"}")
sandbox_args+=("${AUTH_ARGS[@]+"${AUTH_ARGS[@]}"}")

exec "$SANDBOX_RUNNER" "${sandbox_args[@]}" -- "$proof_script"
