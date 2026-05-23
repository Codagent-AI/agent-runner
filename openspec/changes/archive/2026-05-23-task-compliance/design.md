## Context

agent-runner's implementation workflows are supposed to activate agent-validator's built-in `task-compliance` review when validating each task. Today they don't:

- `workflows/core/run-validator.yaml` runs `agent-validator run --report` with no opt-in flags.
- This repo's `.validator/config.yml` does not declare a `task-compliance` entry, so `agent-validator run --enable-review task-compliance` would be silently ignored even if the flag were passed (per agent-validator's `dynamic-review-control` semantics: the flag flips an existing entry from `enabled: false` to active; it does not inject reviews).

`git log` shows this used to be partially wired up and was removed in commit `b9131a6` ("fix: correct live run follow and workflow skips") as collateral damage in an otherwise unrelated change. That commit message does not mention task-compliance. The original wiring was also incomplete — `--context-file <task_file>` was never present, so the review would have run with an empty `{{CONTEXT}}` substitution.

The upstream agent-validator change (`fc01607`, archived as `init-task-compliance`) added `agent-validator init --enable-builtin <names...>` to scaffold opt-in built-ins like `task-compliance` into a fresh `.validator/config.yml`. This change is the consumer side: wire that flag in at install time and wire the runtime `--enable-review` + `--context-file` flags through the workflow params.

## Goals / Non-Goals

**Goals:**

- End-to-end activation of `task-compliance` for any project that goes through agent-runner's onboarding from a clean slate.
- No regressions for the five existing callers of `core/run-validator` that don't have a task file (`spec-driven/implement-change`, `openspec/implement-change`, `spec-driven/simple-change`, `openspec/simple-change`, `onboarding/validator`).
- Restore the entry to this repo's `.validator/config.yml` so the review actually runs when developers iterate on agent-runner itself.

**Non-Goals:**

- Migration for projects whose `.validator/` was scaffolded by an older agent-validator. agent-validator's new init prints a "paste this YAML" warning when the dir already exists; we accept that gap. (Rejected: building a companion `agent-validator config add-review` command, or having agent-runner edit the YAML directly.)
- Wiring agent-validator's other opt-in built-in (`test-integrity`). The plumbing is general enough to support it, but no workflow needs it today.
- Enforcing a minimum agent-validator version in agent-runner code. If an older binary is on PATH, `agent-validator init --enable-builtin task-compliance` fails with an unknown-option error from agent-validator itself — clear enough.
- Changing how the existing fix-violations retry loop handles review failures. Task-compliance findings are surfaced as review violations and flow through the existing `validator_output` → `fix-violations` step unchanged.

## Approach

### Runtime flag wiring ("Option α")

In planning, three approaches were considered for how to make the validator command conditional on `task_file`:

- **α — single command with inline shell `if`** *(chosen)*. The command in `run-validator.yaml`'s `run-validator` step becomes a multi-line shell block that issues `agent-validator run --report --enable-review task-compliance --context-file "{{task_file}}"` when the param is non-empty and `agent-validator run --report` otherwise. One step, one capture (`validator_output`), the existing `fix-violations` step is untouched.
- **β — two mutually-skipped steps**, each with its own `skip_if: 'sh: …'`. Literal commands but two `break_if: success` conditions, two `validator_output` writes (only one ever runs per iteration), and more clutter.
- **γ — separate sub-workflow** `run-validator-with-task.yaml` that hardcodes the flags, called only by `implement-task`. Zero blast radius on the original file but duplicates the entire ~40-line retry/fix-loop scaffolding.

α won on smallness of diff and singleness of capture. Multi-line shell in a `command:` block is already an established pattern in this codebase (see `onboarding/validator.yaml`'s `stash-guided-changes` and `restore-guided-changes` steps).

The conditional uses `if [ -n "{{task_file}}" ]`. agent-runner's interpolation substitutes the param value into the command string before sh runs, so the test is shell-side on the literal value (or empty string).

### Param propagation

Two new lines in `implement-task.yaml`:

```yaml
  - id: run-validator
    workflow: run-validator.yaml
    params:
      task_file: "{{task_file}}"
    continue_on_failure: true
```

Existing callers of `run-validator` (the five listed under Goals) are not modified. They omit `params:` entirely; the optional `task_file` defaults to empty; the workflow takes the no-task-compliance branch.

### Install-time wiring

`cmd/agent-runner/internal_cmd.go:133` currently is:

```go
func validatorInitArgs(projectConfigPath string) ([]string, error) {
	_ = projectConfigPath
	return []string{"init"}, nil
}
```

It changes to return `[]string{"init", "--enable-builtin", "task-compliance"}`. agent-validator's existing `--yes` and `--agents` flags continue to be passed (or not) as before — the new flag is additive.

The `_ = projectConfigPath` discard line is retained; the parameter is kept on the function signature in case a future change needs to consult the project config to decide which built-ins to enable.

`cmd/agent-runner/internal_cmd_test.go` already covers `validatorInitArgs`; the assertion updates to expect the new slice.

### This-repo config restoration

The previous entry in this repo's `.validator/config.yml` was alongside `security-and-errors` under the root entry point. The config has since been simplified to use `all-reviewers`. The restored entry goes under the existing root entry point, after `all-reviewers`:

```yaml
    reviews:
      - all-reviewers:
          builtin: all-reviewers
      - task-compliance:
          builtin: task-compliance
          enabled: false # Opt-in: activate with `agent-validator run --enable-review task-compliance --context-file <task>`
```

The comment is load-bearing for human discoverability and matches the form previously committed.

The `model: claude-sonnet-4.6` field that originally accompanied the entry is intentionally omitted; review model selection should flow from the project's CLI defaults rather than being pinned per-review.

### Docs

`docs/LOOPS-AND-SUBWORKFLOWS.md` previously showed `agent-validator run --enable-review task-compliance` in its run-validator snippet. `b9131a6` collapsed it to `agent-validator run --report`. Restore the snippet to match the conditional form that the workflow now uses (or note that the actual workflow uses a conditional, and show the activated form as an example).

## Decisions

- **Option α over β/γ.** Smallest diff, single capture path, shell-conditional pattern already idiomatic in this repo. β and γ both introduce more state to manage for a one-bit decision.
- **`task_file` is the param name, not `task_context` or generic `extra_args`.** The activation is specifically about pointing the review at a task; the name should say that. Generic flag-passing would invite misuse and obscure intent.
- **Restore this repo's `.validator/config.yml` entry rather than rely on someone re-running `agent-validator init`.** Re-init is a no-op when `.validator/` exists. Restoring the entry directly is the only path back to a working local validator in this repo.
- **No code-level version pin on agent-validator.** Agent-validator's own error message ("unknown option `--enable-builtin`") is already clear; adding a wrapper version check in agent-runner adds maintenance burden for a problem that only exists for a window of time after this lands.
- **Don't auto-fix existing `.validator/` directories in consumer projects.** Matches the upstream agent-validator decision ("accept the gap" with a warning) and avoids agent-runner reaching into another tool's config format.

## Risks / Trade-offs

- **Older agent-validator binaries on PATH cause onboarding to fail at the validator-init step.** Mitigation: agent-validator's error message is clear; release notes should call out the minimum version. The failure is at a step (validator-init) where the user is already paying attention.
- **The conditional `if` in `command:` is shell, not YAML.** A reader has to mentally execute the shell to see what command actually runs. Mitigated by precedent (other workflows already do this) and by the fact that the shell is two branches of one literal `agent-validator …` invocation.
- **Param plumbing in workflows is implicit.** `task_file` is referenced via `{{task_file}}` in two places (the workflow command and the sub-workflow call). A typo in either won't be caught until runtime. Mitigated by the existing workflow load-time validation of param references.
- **Existing-`.validator/` projects silently miss the entry after upgrading agent-runner.** They'll get the upstream agent-validator warning at their next `validator-init`, but until then, task-compliance won't activate even though the workflow now passes the flags. This is accepted per the "accept the gap" decision and documented in the agent-validator change.
