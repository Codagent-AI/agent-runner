#!/usr/bin/env bash
# Runs and-scene evals inside the browser-capable sandbox substrate.
set -euo pipefail

REPO="${REPO:-https://github.com/Codagent-AI/and-scene.git}"
FIXTURE_REF="${FIXTURE_REF:-origin/eval/create-and-scene-spec-only}"
REFERENCE_REF="${REFERENCE_REF:-origin/change/create-and-scene}"
CHANGE_NAME="${CHANGE_NAME:-create-and-scene}"
WORKFLOW="${WORKFLOW:-/tmp/agent-runner-local/scripts/eval-workflows/and-scene-implement.yaml}"
AGENT="${AGENT:-claude}"
MODEL="${MODEL:-}"
JUDGE_MODEL="${JUDGE_MODEL:-}"
JUDGE_COMMAND="${JUDGE_COMMAND:-}"
ARTIFACT_DIR="${ARTIFACT_DIR:-}"
DRY_RUN=0
PROOF_BROWSER=0
RUN_AGENT=0
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
RUNNER_ROOT="$(cd -- "$SCRIPT_DIR/.." && pwd)"
SANDBOX_RUNNER="${SANDBOX_RUNNER:-$SCRIPT_DIR/sandbox-run.sh}"
ENV_ARGS=()
ENV_FILE_ARGS=()
AUTH_ARGS=()
MOUNT_CODEX_AUTH=0
MOUNT_CLAUDE_AUTH=0

usage() {
  cat <<'USAGE'
Usage: eval-and-scene.sh (--proof-browser | --run-agent) [options]

Runs and-scene evals in the Agent Runner sandbox.

Credential posture: no host credentials are inherited by default except the
chosen agent auth mount in --run-agent mode. Pass only short-lived, repo-scoped
credentials with --env, for example --env GITHUB_TOKEN for a private fixture
clone, or use the sandbox runner's default .sandbox-secrets.env file. Any env
secret passed through is readable by processes inside the container.

Modes:
  --proof-browser        Run the narrow container/browser proof.
  --run-agent            Run the scored Agent Runner eval harness.

Options:
  --dry-run              Print the sandbox command instead of running it.
  --artifact-dir PATH    Host artifact directory. Default:
                          proof: artifacts/evals/and-scene-proof/<timestamp>
                          run:   artifacts/evals/and-scene/<timestamp>
  --repo URL             and-scene repository URL.
  --fixture-ref REF      Implementation-ready fixture ref.
                          Default: origin/eval/create-and-scene-spec-only
  --reference-ref REF    Implemented/reference ref.
                          Default: origin/change/create-and-scene
  --workflow REF         Workflow name or container-visible YAML path for
                          --run-agent. Default:
                          /tmp/agent-runner-local/scripts/eval-workflows/and-scene-implement.yaml
  --change-name NAME     OpenSpec change name. Default: create-and-scene
  --agent CLI            Implementor CLI for --run-agent. Default: claude.
  --model MODEL          Implementor model override recorded in injected config.
  --judge-model MODEL    Tier-2 judge model label recorded in metadata.
  --judge-command CMD    Optional shell command that reads the judge prompt from
                          stdin and writes structured JSON to stdout. Example:
                          codex exec --model gpt-5 --output-schema /artifacts/tier2-schema.json -
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
    --run-agent)
      RUN_AGENT=1
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
    --workflow)
      WORKFLOW="${2:?missing value for --workflow}"
      shift 2
      ;;
    --change-name)
      CHANGE_NAME="${2:?missing value for --change-name}"
      shift 2
      ;;
    --agent)
      AGENT="${2:?missing value for --agent}"
      shift 2
      ;;
    --model)
      MODEL="${2:?missing value for --model}"
      shift 2
      ;;
    --judge-model)
      JUDGE_MODEL="${2:?missing value for --judge-model}"
      shift 2
      ;;
    --judge-command)
      JUDGE_COMMAND="${2:?missing value for --judge-command}"
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
      MOUNT_CODEX_AUTH=1
      AUTH_ARGS+=(--mount-codex-auth)
      shift
      ;;
    --mount-claude-auth)
      MOUNT_CLAUDE_AUTH=1
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

if ((PROOF_BROWSER + RUN_AGENT != 1)); then
  echo "Choose exactly one mode: --proof-browser or --run-agent." >&2
  usage >&2
  exit 2
fi

if [[ "$RUN_AGENT" == 1 ]]; then
  case "$AGENT" in
    claude)
      if [[ "$MOUNT_CLAUDE_AUTH" != 1 ]]; then
        AUTH_ARGS+=(--mount-claude-auth)
      fi
      ;;
    codex)
      if [[ "$MOUNT_CODEX_AUTH" != 1 ]]; then
        AUTH_ARGS+=(--mount-codex-auth)
      fi
      ;;
    *)
      echo "Unsupported --agent $AGENT for auth forwarding; expected claude or codex." >&2
      exit 2
      ;;
  esac
fi

if [[ -z "$ARTIFACT_DIR" ]]; then
  if [[ "$PROOF_BROWSER" == 1 ]]; then
    ARTIFACT_DIR="$RUNNER_ROOT/artifacts/evals/and-scene-proof/$(timestamp)"
  else
    ARTIFACT_DIR="$RUNNER_ROOT/artifacts/evals/and-scene/$(timestamp)"
  fi
elif [[ "$ARTIFACT_DIR" != /* ]]; then
  ARTIFACT_DIR="$RUNNER_ROOT/$ARTIFACT_DIR"
fi

REPO_Q="$(shell_quote "$REPO")"
FIXTURE_REF_Q="$(shell_quote "$FIXTURE_REF")"
REFERENCE_REF_Q="$(shell_quote "$REFERENCE_REF")"
CHANGE_NAME_Q="$(shell_quote "$CHANGE_NAME")"
CHANGE_PARAM_Q="$(shell_quote "change_name=$CHANGE_NAME")"
WORKFLOW_Q="$(shell_quote "$WORKFLOW")"
AGENT_Q="$(shell_quote "$AGENT")"
MODEL_Q="$(shell_quote "$MODEL")"
JUDGE_MODEL_Q="$(shell_quote "$JUDGE_MODEL")"
JUDGE_COMMAND_Q="$(shell_quote "$JUDGE_COMMAND")"

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

agent_script=$(cat <<AGENT
set -euo pipefail
mkdir -p /artifacts/logs /artifacts/run-state /artifacts/screenshots /workspace/runs
if [[ " \${NODE_OPTIONS:-} " != *" --dns-result-order="* ]]; then
  export NODE_OPTIONS="\${NODE_OPTIONS:-} --dns-result-order=ipv4first"
fi

REPO=$REPO_Q
FIXTURE_REF=$FIXTURE_REF_Q
REFERENCE_REF=$REFERENCE_REF_Q
CHANGE_NAME=$CHANGE_NAME_Q
WORKFLOW_PATH=$WORKFLOW_Q
EVAL_AGENT=$AGENT_Q
EVAL_MODEL=$MODEL_Q
JUDGE_MODEL=$JUDGE_MODEL_Q
JUDGE_COMMAND=$JUDGE_COMMAND_Q
FIXTURE_COMMIT=""
REFERENCE_COMMIT=""
FINAL_COMMIT=""
DIFF_HASH=""
RUN_SESSION_DIR=""
AGENT_RUNNER_EXIT_CODE=0
NPM_CI_EXIT_CODE=0
BUILD_EXIT_CODE=0
VERIFY_EXIT_CODE=0
JUDGE_EXIT_CODE=0
TIER2_STATUS="not_configured"

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

run_logged() {
  local name="\$1"
  shift
  "\$@" 2>&1 | tee "/artifacts/logs/\${name}.log"
  return "\${PIPESTATUS[0]}"
}

write_eval_config() {
  mkdir -p .agent-runner
  {
    printf '%s\n' 'active_profile: eval'
    printf '%s\n' 'profiles:'
    printf '%s\n' '  eval:'
    printf '%s\n' '    agents:'
    printf '%s\n' '      implementor:'
    printf '%s\n' '        default_mode: autonomous'
    printf '        cli: %s\n' "\$EVAL_AGENT"
    if [ -n "\$EVAL_MODEL" ]; then
      printf '        model: %s\n' "\$EVAL_MODEL"
    fi
  } > .agent-runner/config.yaml
}

latest_run_dir() {
  ls -td "\$HOME"/.agent-runner/projects/*/runs/* 2>/dev/null | head -n 1 || true
}

capture_run_state() {
  RUN_SESSION_DIR="\$(latest_run_dir)"
  if [ -n "\$RUN_SESSION_DIR" ]; then
    cp "\$RUN_SESSION_DIR/state.json" /artifacts/run-state/state.json 2>/dev/null || true
    cp "\$RUN_SESSION_DIR/audit.log" /artifacts/run-state/audit.log 2>/dev/null || true
  fi
}

capture_screenshots() {
  cd /workspace/runs/fixture
  while IFS= read -r file; do
    local target="/artifacts/screenshots/\${file#./}"
    mkdir -p "\$(dirname "\$target")"
    cp "\$file" "\$target"
  done < <(find . \\
    -path './node_modules' -prune -o \\
    -path './.git' -prune -o \\
    -type f \\( -name '*.png' -o -name '*.jpg' -o -name '*.jpeg' -o -name '*.webp' \\) -print)
}

write_diff() {
  cd /workspace/runs/fixture
  FINAL_COMMIT="\$(git rev-parse HEAD 2>/dev/null || true)"
  git ls-files --others --exclude-standard | while IFS= read -r file; do
    if [[ "\$file" == .agent-runner/* || "\$file" == .validator/* || "\$file" == validator_logs/* || "\$file" == .openspec/* || "\$file" == .codex/* || "\$file" == .claude/* || "\$file" == node_modules/* || "\$file" == dist/* ]]; then
      continue
    fi
    git add -N -- "\$file" 2>/dev/null || true
  done
  git diff --binary "\$FIXTURE_COMMIT" -- \\
    . \\
    ':(exclude).agent-runner/**' \\
    ':(exclude).validator/**' \\
    ':(exclude)validator_logs/**' \\
    ':(exclude).openspec/**' \\
    ':(exclude).codex/**' \\
    ':(exclude).claude/**' \\
    ':(exclude)node_modules/**' \\
    ':(exclude)dist/**' \\
    > /artifacts/implementation.diff
  DIFF_HASH="\$(sha256sum /artifacts/implementation.diff | awk '{print \$1}')"
  printf '%s\n' "\$DIFF_HASH" > /artifacts/diff-hash.txt
}

write_tier1_result() {
  jq -n \\
    --argjson agent_runner_exit_code "\$AGENT_RUNNER_EXIT_CODE" \\
    --argjson npm_ci_exit_code "\$NPM_CI_EXIT_CODE" \\
    --argjson build_exit_code "\$BUILD_EXIT_CODE" \\
    --argjson verify_exit_code "\$VERIFY_EXIT_CODE" \\
    '{
      pass: (
        \$agent_runner_exit_code == 0 and
        \$npm_ci_exit_code == 0 and
        \$build_exit_code == 0 and
        \$verify_exit_code == 0
      ),
      agent_runner_exit_code: \$agent_runner_exit_code,
      npm_ci_exit_code: \$npm_ci_exit_code,
      build_exit_code: \$build_exit_code,
      verify_exit_code: \$verify_exit_code
    }' > /artifacts/tier1-result.json
}

write_judge_schema() {
  cat > /artifacts/tier2-schema.json <<'JSON'
{
  "type": "object",
  "additionalProperties": false,
  "required": ["scenarios", "overall_score", "pass", "rationale"],
  "properties": {
    "scenarios": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["id", "critical", "verdict", "note"],
        "properties": {
          "id": {"type": "string"},
          "critical": {"type": "boolean"},
          "verdict": {"type": "string", "enum": ["pass", "fail"]},
          "note": {"type": "string"}
        }
      }
    },
    "overall_score": {"type": "integer", "minimum": 0, "maximum": 100},
    "pass": {"type": "boolean"},
    "rationale": {"type": "string"}
  }
}
JSON
}

append_file_block() {
  local file="\$1"
  printf '\\n### %s\\n\\n' "\$file"
  printf '~~~\\n'
  sed -n '1,220p' "\$file"
  printf '\\n~~~\\n'
}

write_judge_prompt() {
  cd /workspace/runs/fixture
  {
    printf '%s\n\n' '# And Scene Tier-2 Eval Judge'
    printf '%s\n' 'Grade the produced artifact against the OpenSpec scenarios first. Use the reference implementation only as a tiebreak for scenarios you cannot confidently call from the produced artifact alone. Do not penalize valid divergence from the reference.'
    printf '%s\n\n' 'Return JSON matching /artifacts/tier2-schema.json. Mark critical=true for verification/render scenarios and core skill-behavior scenarios. pass must be true only when every critical scenario passes.'
    printf 'Fixture commit: %s\n' "\$FIXTURE_COMMIT"
    printf 'Produced commit: %s\n' "\$FINAL_COMMIT"
    printf 'Reference commit: %s\n' "\$REFERENCE_COMMIT"
    printf 'Tier-1 result path: /artifacts/tier1-result.json\n\n'

    printf '%s\n' '## Spec Files'
    while IFS= read -r file; do
      append_file_block "\$file"
    done < <(find "openspec/changes/\$CHANGE_NAME/specs" -type f -name '*.md' | sort)

    printf '\\n%s\\n' '## Implementation Diff'
    printf '~~~diff\\n'
    sed -n '1,2000p' /artifacts/implementation.diff
    printf '\\n~~~\\n'

    printf '\\n%s\\n' '## Produced Source Tree'
    find . \\
      -path './.git' -prune -o \\
      -path './node_modules' -prune -o \\
      -path './dist' -prune -o \\
      -path './.agent-runner' -prune -o \\
      -path './validator_logs' -prune -o \\
      -type f \\
      ! -name 'package-lock.json' \\
      ! -name '*.png' \\
      ! -name '*.jpg' \\
      ! -name '*.jpeg' \\
      ! -name '*.webp' \\
      -size -200k \\
      -print | sort | while IFS= read -r file; do
        append_file_block "\$file"
      done

    printf '\\n%s\\n' '## Build And Verify Logs'
    for file in /artifacts/logs/fixture-npm-ci.log /artifacts/logs/fixture-build.log /artifacts/logs/fixture-verify.log; do
      if [ -f "\$file" ]; then
        append_file_block "\$file"
      fi
    done

    printf '\\n%s\\n' '## Screenshots'
    find /artifacts/screenshots -type f | sort || true

    if [ -d /workspace/runs/reference ]; then
      printf '\\n%s\\n' '## Reference Source Tree For Tiebreak Only'
      cd /workspace/runs/reference
      find . \\
        -path './.git' -prune -o \\
        -path './node_modules' -prune -o \\
        -path './dist' -prune -o \\
        -type f \\
        ! -name 'package-lock.json' \\
        ! -name '*.png' \\
        ! -name '*.jpg' \\
        ! -name '*.jpeg' \\
        ! -name '*.webp' \\
        -size -200k \\
        -print | sort | while IFS= read -r file; do
          append_file_block "\$file"
        done
    fi
  } > /artifacts/tier2-judge-prompt.md
}

run_tier2_judge() {
  write_judge_schema
  write_judge_prompt
  if [ -z "\$JUDGE_COMMAND" ]; then
    jq -n \\
      --arg judge_model "\$JUDGE_MODEL" \\
      '{
        status: "not_configured",
        judge_model: \$judge_model,
        pass: false,
        overall_score: 0,
        rationale: "No --judge-command was provided. Judge prompt and schema were written for an external tier-2 run."
      }' > /artifacts/tier2-result.json
    TIER2_STATUS="not_configured"
    return 0
  fi

  set +e
  bash -lc "\$JUDGE_COMMAND" < /artifacts/tier2-judge-prompt.md > /artifacts/tier2-result.json 2> /artifacts/logs/tier2-judge.log
  JUDGE_EXIT_CODE=\$?
  set -e
  if [ "\$JUDGE_EXIT_CODE" -eq 0 ]; then
    TIER2_STATUS="completed"
  else
    TIER2_STATUS="failed"
  fi
  return 0
}

write_metadata() {
  jq -n \\
    --arg repo "\$REPO" \\
    --arg fixture_ref "\$FIXTURE_REF" \\
    --arg reference_ref "\$REFERENCE_REF" \\
    --arg change_name "\$CHANGE_NAME" \\
    --arg workflow "\$WORKFLOW_PATH" \\
    --arg agent "\$EVAL_AGENT" \\
    --arg model "\$EVAL_MODEL" \\
    --arg judge_model "\$JUDGE_MODEL" \\
    --arg judge_status "\$TIER2_STATUS" \\
    --arg started_at "\${STARTED_AT}" \\
    --arg ended_at "\$(date -u +"%Y-%m-%dT%H:%M:%SZ")" \\
    --arg agent_runner_commit "\${AGENT_RUNNER_SOURCE_COMMIT:-}" \\
    --arg agent_runner_dirty "\${AGENT_RUNNER_SOURCE_DIRTY:-}" \\
    --arg fixture_commit "\$FIXTURE_COMMIT" \\
    --arg reference_commit "\$REFERENCE_COMMIT" \\
    --arg final_commit "\$FINAL_COMMIT" \\
    --arg diff_hash "\$DIFF_HASH" \\
    --arg run_session_dir "\$RUN_SESSION_DIR" \\
    --arg node_version "\$(node --version 2>/dev/null || true)" \\
    --arg npm_version "\$(npm --version 2>/dev/null || true)" \\
    --arg playwright_browsers_path "\${PLAYWRIGHT_BROWSERS_PATH:-}" \\
    --argjson agent_runner_exit_code "\$AGENT_RUNNER_EXIT_CODE" \\
    --argjson npm_ci_exit_code "\$NPM_CI_EXIT_CODE" \\
    --argjson build_exit_code "\$BUILD_EXIT_CODE" \\
    --argjson verify_exit_code "\$VERIFY_EXIT_CODE" \\
    --argjson judge_exit_code "\$JUDGE_EXIT_CODE" \\
    --argjson exit_code "\${EXIT_CODE:-0}" \\
    '{
      repo: \$repo,
      fixture_ref: \$fixture_ref,
      reference_ref: \$reference_ref,
      change_name: \$change_name,
      workflow: \$workflow,
      agent: \$agent,
      model: \$model,
      judge_model: \$judge_model,
      judge_status: \$judge_status,
      started_at: \$started_at,
      ended_at: \$ended_at,
      agent_runner_commit: \$agent_runner_commit,
      agent_runner_dirty: (\$agent_runner_dirty == "true"),
      fixture_commit: \$fixture_commit,
      reference_commit: \$reference_commit,
      final_commit: \$final_commit,
      diff_hash: \$diff_hash,
      run_session_dir: \$run_session_dir,
      cli_versions: {
        node: \$node_version,
        npm: \$npm_version
      },
      playwright_browsers_path: \$playwright_browsers_path,
      exit_codes: {
        agent_runner: \$agent_runner_exit_code,
        npm_ci: \$npm_ci_exit_code,
        build: \$build_exit_code,
        verify: \$verify_exit_code,
        judge: \$judge_exit_code,
        harness: \$exit_code
      }
    }' > /artifacts/metadata.json
}

STARTED_AT="\$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
EXIT_CODE=0
trap 'EXIT_CODE=\$?; write_metadata; exit \$EXIT_CODE' EXIT

configure_github_https_auth

git clone "\$REPO" /workspace/runs/fixture 2>&1 | tee /artifacts/logs/fixture-clone.log
cd /workspace/runs/fixture
git fetch origin 2>&1 | tee -a /artifacts/logs/fixture-clone.log
git checkout "\$FIXTURE_REF" 2>&1 | tee -a /artifacts/logs/fixture-clone.log
FIXTURE_COMMIT="\$(git rev-parse HEAD)"
write_eval_config

set +e
run_logged agent-runner env AGENT_RUNNER_NO_TUI=1 agent-runner run "\$WORKFLOW_PATH" $CHANGE_PARAM_Q
AGENT_RUNNER_EXIT_CODE=\$?
set -e
capture_run_state

set +e
run_logged fixture-npm-ci npm ci
NPM_CI_EXIT_CODE=\$?
run_logged fixture-build npm run build
BUILD_EXIT_CODE=\$?
run_logged fixture-verify npm run verify
VERIFY_EXIT_CODE=\$?
set -e
capture_screenshots
write_diff
write_tier1_result

git clone "\$REPO" /workspace/runs/reference 2>&1 | tee /artifacts/logs/reference-clone.log
cd /workspace/runs/reference
git fetch origin 2>&1 | tee -a /artifacts/logs/reference-clone.log
git checkout "\$REFERENCE_REF" 2>&1 | tee -a /artifacts/logs/reference-clone.log
REFERENCE_COMMIT="\$(git rev-parse HEAD)"

run_tier2_judge

if [ "\$AGENT_RUNNER_EXIT_CODE" -ne 0 ] || [ "\$NPM_CI_EXIT_CODE" -ne 0 ] || [ "\$BUILD_EXIT_CODE" -ne 0 ] || [ "\$VERIFY_EXIT_CODE" -ne 0 ] || [ "\$JUDGE_EXIT_CODE" -ne 0 ]; then
  exit 1
fi
AGENT
)

sandbox_args=(--artifact-dir "$ARTIFACT_DIR")
if [[ "$DRY_RUN" == 1 ]]; then
  sandbox_args=(--dry-run "${sandbox_args[@]}")
fi
sandbox_args+=("${ENV_ARGS[@]+"${ENV_ARGS[@]}"}")
sandbox_args+=("${ENV_FILE_ARGS[@]+"${ENV_FILE_ARGS[@]}"}")
sandbox_args+=("${AUTH_ARGS[@]+"${AUTH_ARGS[@]}"}")

if [[ "$PROOF_BROWSER" == 1 ]]; then
  exec "$SANDBOX_RUNNER" "${sandbox_args[@]}" -- "$proof_script"
fi

exec "$SANDBOX_RUNNER" "${sandbox_args[@]}" -- "$agent_script"
