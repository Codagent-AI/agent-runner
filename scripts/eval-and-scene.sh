#!/usr/bin/env bash
# Runs and-scene evals inside the browser-capable sandbox substrate.
set -euo pipefail

REPO="${REPO:-https://github.com/Codagent-AI/and-scene.git}"
# Pin the fixture to an exact commit, not a moving branch head, so scored runs
# are reproducible. This is the head of eval/create-and-scene-spec-only as of
# 2026-07-10; bump it deliberately when the fixture snapshot changes.
FIXTURE_REF="${FIXTURE_REF:-cd0cc0038b9754345c1baf2b2bbad7b3ad37b19c}"
# The reference is a tiebreak-only, divergence-tolerant input, so it stays a
# branch ref rather than a pin.
REFERENCE_REF="${REFERENCE_REF:-origin/change/create-and-scene}"
CHANGE_NAME="${CHANGE_NAME:-create-and-scene}"
DEFAULT_WORKFLOW="/tmp/agent-runner-local/workflows/openspec/implement-change.yaml"
WORKFLOW="${WORKFLOW:-$DEFAULT_WORKFLOW}"
# Stop the default workflow after the implementation is validated, before its
# outward-facing archive/finalize tail (which would open a real PR). Applied
# only to the default workflow, since a custom --workflow may lack this step and
# --until validates the step ID up front.
DEFAULT_UNTIL="run-validator"
UNTIL="${UNTIL:-}"
AGENT="${AGENT:-claude}"
MODEL="${MODEL:-}"
JUDGE_MODEL="${JUDGE_MODEL:-codex-default}"
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
WORKFLOW_ARGS=()
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
                          /tmp/agent-runner-local/workflows/openspec/implement-change.yaml
  --until STEP           Stop the run after the named top-level step. Defaults to
                          run-validator for the default workflow, halting before
                          its outward archive/finalize (PR-opening) tail. Empty
                          for a custom --workflow unless set explicitly.
  --change-name NAME     OpenSpec change name. Default: create-and-scene
  --workflow-arg ARG     Pass NAME=VALUE to the selected workflow. Repeatable.
                          The default workflow receives change_name automatically
                          when no workflow arguments are supplied.
  --agent CLI            Implementor CLI for --run-agent. Default: claude.
  --model MODEL          Implementor model override recorded in injected config.
  --judge-model MODEL    Tier-2 judge model. Default: the Codex CLI default.
  --judge-command CMD    Override the default Codex judge. The command reads the
                          prompt from stdin, writes structured JSON to stdout,
                          and receives every screenshot path as a positional arg.
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
    --until)
      UNTIL="${2:?missing value for --until}"
      shift 2
      ;;
    --change-name)
      CHANGE_NAME="${2:?missing value for --change-name}"
      shift 2
      ;;
    --workflow-arg)
      WORKFLOW_ARGS+=("${2:?missing value for --workflow-arg}")
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
        MOUNT_CLAUDE_AUTH=1
      fi
      ;;
    codex)
      if [[ "$MOUNT_CODEX_AUTH" != 1 ]]; then
        AUTH_ARGS+=(--mount-codex-auth)
        MOUNT_CODEX_AUTH=1
      fi
      ;;
    *)
      echo "Unsupported --agent $AGENT for auth forwarding; expected claude or codex." >&2
      exit 2
      ;;
  esac

  if [[ -z "$JUDGE_COMMAND" && "$MOUNT_CODEX_AUTH" != 1 ]]; then
    AUTH_ARGS+=(--mount-codex-auth)
    MOUNT_CODEX_AUTH=1
  fi

  if [[ "${#WORKFLOW_ARGS[@]}" -eq 0 && "$WORKFLOW" == "$DEFAULT_WORKFLOW" ]]; then
    WORKFLOW_ARGS+=("change_name=$CHANGE_NAME")
  fi
  if [[ -z "$UNTIL" && "$WORKFLOW" == "$DEFAULT_WORKFLOW" ]]; then
    UNTIL="$DEFAULT_UNTIL"
  fi
  for workflow_arg in "${WORKFLOW_ARGS[@]}"; do
    if [[ "$workflow_arg" != *=* || "$workflow_arg" == =* ]]; then
      echo "Invalid --workflow-arg $workflow_arg; expected NAME=VALUE." >&2
      exit 2
    fi
  done
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
WORKFLOW_Q="$(shell_quote "$WORKFLOW")"
UNTIL_Q="$(shell_quote "$UNTIL")"
AGENT_Q="$(shell_quote "$AGENT")"
MODEL_Q="$(shell_quote "$MODEL")"
JUDGE_MODEL_Q="$(shell_quote "$JUDGE_MODEL")"
JUDGE_COMMAND_Q="$(shell_quote "$JUDGE_COMMAND")"
WORKFLOW_ARGS_Q=""
for workflow_arg in "${WORKFLOW_ARGS[@]+"${WORKFLOW_ARGS[@]}"}"; do
  WORKFLOW_ARGS_Q+="$(shell_quote "$workflow_arg") "
done

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
UNTIL=$UNTIL_Q
EVAL_AGENT=$AGENT_Q
EVAL_MODEL=$MODEL_Q
JUDGE_MODEL=$JUDGE_MODEL_Q
JUDGE_COMMAND=$JUDGE_COMMAND_Q
WORKFLOW_ARGS=($WORKFLOW_ARGS_Q)
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
TIER2_STATUS="pending"

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
    # The real implement-change workflow drives its review-assumptions/simplify
    # tail through the lead-agent (planner) session. Configure it autonomous so
    # those steps run headless in the sandbox instead of blocking on input.
    printf '%s\n' '      planner:'
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

# Capture real render screenshots for the tier-2 judge. and-scene's own
# verify checks for render errors but never screenshots, so we boot the built
# preview and shoot each presentation/scene using the same registry + step
# contract verify relies on. The helper script is copied in under a dot name so
# bare imports resolve against the checkout's node_modules, then removed so it
# never lands in the scored diff.
capture_screenshots() {
  cd /workspace/runs/fixture
  local script_src="/tmp/agent-runner-local/scripts/eval-workflows/scene-shots.mjs"
  if [ ! -f "\$script_src" ]; then
    printf '%s\\n' "scene-shots helper not found at \$script_src" > /artifacts/logs/screenshots.log
    return 0
  fi
  cp "\$script_src" ./.eval-scene-shots.mjs
  SHOTS_OUT=/artifacts/screenshots node --experimental-strip-types ./.eval-scene-shots.mjs \\
    > /artifacts/logs/screenshots.log 2>&1 || true
  rm -f ./.eval-scene-shots.mjs
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
      pass: (\$build_exit_code == 0 and \$verify_exit_code == 0),
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
  cat -- "\$file"
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
      -path './.validator' -prune -o \\
      -path './validator_logs' -prune -o \\
      -path './.openspec' -prune -o \\
      -path './.codex' -prune -o \\
      -path './.claude' -prune -o \\
      -type f \\
      ! -name 'package-lock.json' \\
      ! -name '*.png' \\
      ! -name '*.jpg' \\
      ! -name '*.jpeg' \\
      ! -name '*.webp' \\
      -print | sort | while IFS= read -r file; do
        if LC_ALL=C grep -Iq . "\$file" || [ ! -s "\$file" ]; then
          append_file_block "\$file"
        fi
      done

    printf '\\n%s\\n' '## Build And Verify Logs'
    for file in /artifacts/logs/fixture-npm-ci.log /artifacts/logs/fixture-build.log /artifacts/logs/fixture-verify.log; do
      if [ -f "\$file" ]; then
        append_file_block "\$file"
      fi
    done

    printf '\\n%s\\n' '## Screenshots'
    printf '%s\\n' 'Every path below is attached to the multimodal judge request.'
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
        -print | sort | while IFS= read -r file; do
          if LC_ALL=C grep -Iq . "\$file" || [ ! -s "\$file" ]; then
            append_file_block "\$file"
          fi
        done
    fi
  } > /artifacts/tier2-judge-prompt.md
}

run_tier2_judge() {
  write_judge_schema
  write_judge_prompt
  local screenshots=()
  while IFS= read -r -d '' screenshot; do
    screenshots+=("\$screenshot")
  done < <(find /artifacts/screenshots -type f -print0 | sort -z)
  if [ "\${#screenshots[@]}" -eq 0 ]; then
    printf '%s\\n' 'Tier-2 judging requires at least one render screenshot.' > /artifacts/logs/tier2-judge.log
    JUDGE_EXIT_CODE=2
    TIER2_STATUS="failed"
    return 0
  fi

  set +e
  if [ -n "\$JUDGE_COMMAND" ]; then
    bash -lc "\$JUDGE_COMMAND" judge-command "\${screenshots[@]}" \\
      < /artifacts/tier2-judge-prompt.md \\
      > /artifacts/tier2-result.json \\
      2> /artifacts/logs/tier2-judge.log
    JUDGE_EXIT_CODE=\$?
  else
    judge_args=(
      exec
      --cd /workspace/runs/fixture
      --sandbox read-only
      --skip-git-repo-check
      --ephemeral
      --output-schema /artifacts/tier2-schema.json
      --output-last-message /artifacts/tier2-result.json
    )
    if [ "\$JUDGE_MODEL" != "codex-default" ]; then
      judge_args+=(--model "\$JUDGE_MODEL")
    fi
    for screenshot in "\${screenshots[@]}"; do
      judge_args+=(--image "\$screenshot")
    done
    judge_args+=(-)
    codex "\${judge_args[@]}" \\
      < /artifacts/tier2-judge-prompt.md \\
      > /artifacts/logs/tier2-judge.log 2>&1
    JUDGE_EXIT_CODE=\$?
  fi
  set -e
  if [ "\$JUDGE_EXIT_CODE" -eq 0 ] && jq -e '.pass == true' /artifacts/tier2-result.json >/dev/null 2>&1; then
    TIER2_STATUS="completed"
  else
    if [ "\$JUDGE_EXIT_CODE" -eq 0 ]; then
      JUDGE_EXIT_CODE=1
    fi
    TIER2_STATUS="failed"
  fi
  return 0
}

write_metadata() {
  local workflow_args_json='[]'
  if [ "\${#WORKFLOW_ARGS[@]}" -gt 0 ]; then
    workflow_args_json="\$(printf '%s\\n' "\${WORKFLOW_ARGS[@]}" | jq -Rsc 'split("\\n")[:-1]')"
  fi
  jq -n \\
    --arg repo "\$REPO" \\
    --arg fixture_ref "\$FIXTURE_REF" \\
    --arg reference_ref "\$REFERENCE_REF" \\
    --arg change_name "\$CHANGE_NAME" \\
    --arg workflow "\$WORKFLOW_PATH" \\
    --arg until "\$UNTIL" \\
    --argjson workflow_args "\$workflow_args_json" \\
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
      until: \$until,
      workflow_args: \$workflow_args,
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

# Aggregate the scored dimensions into a single reward record. Borrowed from the
# Harbor reward.json shape: named dimensions plus a hard gate and a soft score,
# so runs stay comparable and a hard failure still yields a graded number.
# Computed from the exit-code vars (always set) and a defensive tier-2 read, so
# it produces a coherent record even on a partial run.
write_reward() {
  local scenario_score="null"
  local scenario_pass="false"
  if [ -f /artifacts/tier2-result.json ]; then
    scenario_score="\$(jq '.overall_score // null' /artifacts/tier2-result.json 2>/dev/null || echo null)"
    if jq -e '.pass == true' /artifacts/tier2-result.json >/dev/null 2>&1; then
      scenario_pass="true"
    fi
  fi
  jq -n \\
    --argjson agent_runner_exit_code "\$AGENT_RUNNER_EXIT_CODE" \\
    --argjson npm_ci_exit_code "\$NPM_CI_EXIT_CODE" \\
    --argjson build_exit_code "\$BUILD_EXIT_CODE" \\
    --argjson verify_exit_code "\$VERIFY_EXIT_CODE" \\
    --argjson scenario_score "\$scenario_score" \\
    --argjson scenario_pass "\$scenario_pass" \\
    '
    (\$agent_runner_exit_code == 0) as \$wf
    | (\$npm_ci_exit_code == 0 and \$build_exit_code == 0 and \$verify_exit_code == 0) as \$correct
    | {
        dimensions: {
          workflow_health: {
            pass: \$wf,
            agent_runner_exit_code: \$agent_runner_exit_code
          },
          correctness: {
            pass: \$correct,
            npm_ci_exit_code: \$npm_ci_exit_code,
            build_exit_code: \$build_exit_code,
            verify_exit_code: \$verify_exit_code
          },
          scenario_compliance: {
            pass: \$scenario_pass,
            score: \$scenario_score
          }
        },
        hard_pass: (\$wf and \$correct and \$scenario_pass),
        soft_score: (
          (if \$wf then 20 else 0 end)
          + (if \$correct then 40 else 0 end)
          + ((\$scenario_score // 0) * 0.4)
        )
      }' > /artifacts/reward.json
}

# Enumerate every collected artifact with size and hash. Borrowed from Harbor's
# collection manifest: turns the artifact dir into a self-describing record
# instead of requiring filesystem spelunking. Excludes itself and tolerates a
# partial run by listing whatever exists.
write_manifest() {
  (
    cd /artifacts
    find . -type f ! -name manifest.json -print0 | sort -z | while IFS= read -r -d '' f; do
      rel="\${f#./}"
      size="\$(wc -c < "\$f" | tr -d ' ')"
      hash="\$(sha256sum "\$f" | awk '{print \$1}')"
      jq -n --arg path "\$rel" --argjson size "\$size" --arg sha256 "\$hash" \\
        '{path: \$path, size: \$size, sha256: \$sha256}'
    done | jq -s '{generated_by: "eval-and-scene.sh", file_count: length, files: sort_by(.path)}'
  ) > /artifacts/manifest.json
}

STARTED_AT="\$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
EXIT_CODE=0
trap 'EXIT_CODE=\$?; write_metadata || true; write_reward || true; write_manifest || true; exit \$EXIT_CODE' EXIT

configure_github_https_auth

git clone "\$REPO" /workspace/runs/fixture 2>&1 | tee /artifacts/logs/fixture-clone.log
cd /workspace/runs/fixture
git fetch origin 2>&1 | tee -a /artifacts/logs/fixture-clone.log
git checkout "\$FIXTURE_REF" 2>&1 | tee -a /artifacts/logs/fixture-clone.log
FIXTURE_COMMIT="\$(git rev-parse HEAD)"
write_eval_config

set +e
RUN_ARGS=(run "\$WORKFLOW_PATH")
if [ -n "\$UNTIL" ]; then
  RUN_ARGS+=(--until "\$UNTIL")
fi
if [ "\${#WORKFLOW_ARGS[@]}" -gt 0 ]; then
  RUN_ARGS+=("\${WORKFLOW_ARGS[@]}")
fi
run_logged agent-runner env AGENT_RUNNER_NO_TUI=1 agent-runner "\${RUN_ARGS[@]}"
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
