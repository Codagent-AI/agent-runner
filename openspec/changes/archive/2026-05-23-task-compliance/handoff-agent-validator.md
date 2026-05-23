# Handoff: agent-validator side of task-compliance install-time fix

## Objective

Make it possible for `agent-validator init` to scaffold opt-in built-in reviews (specifically `task-compliance`) into `.validator/config.yml` when an orchestrator like agent-runner requests it. The goal is that after `agent-validator init` runs with the appropriate flag, the resulting `config.yml` contains a `task-compliance` review entry with `enabled: false`, so that a runtime `--enable-review task-compliance --context-file <task_file>` actually activates the review.

The companion change on the agent-runner side will wire `--enable-review task-compliance --context-file {{task_file}}` into `workflows/core/run-validator.yaml` and pass the new init flag from `cmd/agent-runner/internal_cmd.go`'s `validatorInitArgs`. That side is tracked in agent-runner issue #34 and the `task-compliance` change directory in `/Users/paul/codagent/agent-runner` (branch `task-compliance`).

## Current State

### The problem and how it surfaced

agent-runner issue #34 reports that the `task-compliance` built-in review never runs during agent-runner's implementation workflows. Investigation in agent-runner found two compounding gaps:

- **Runtime (Problem 2):** `workflows/core/run-validator.yaml` invokes `agent-validator run --report` with no opt-in flags. Even if the validator config had a `task-compliance` entry, the workflow wouldn't activate it.
- **Install time (Problem 1, what this handoff addresses):** Per `docs/user-guide.md` in agent-validator, `--enable-review <name>` only flips `enabled: false → active` for a review that is already defined in the config. Opt-in built-ins like `task-compliance` and `test-integrity` need a config entry before the flag does anything. Today, nothing in agent-validator's `init` command or `validator-setup` skill writes that entry, so the workflow's flag would be silently ignored.

### Regression context (b9131a6)

`git log` on agent-runner shows that both pieces previously existed for the agent-runner repo itself:

- Commit `a4f9151` (Apr 14) added a `task-compliance` entry with `enabled: false` to agent-runner's own `.validator/config.yml`.
- Commit `b87ebd7` (Apr 20) scaffolded `workflows/core/run-validator.yaml` with `agent-validator run --report --enable-review task-compliance` (but never with `--context-file`).
- Commit `b9131a6` (Apr 25) — "fix: correct live run follow and workflow skips" — removed **both** the config entry and the `--enable-review task-compliance` flag, in what looks like collateral damage inside an otherwise unrelated change. The commit message does not mention task-compliance.

The original wiring worked for the agent-runner repo's own development but was never a real solution for end users: `--context-file` was always missing, and nothing scaffolded the config entry into projects that consume agent-runner's workflows.

### Why the fix lives in agent-validator `init`

Considered alternatives (full list in agent-runner's session for this branch):

- Make `task-compliance` a default review in `validator-setup` or `init` — **rejected**. task-compliance is meaningful only when something orchestrates the choice of task file. Standalone agent-validator users have no orchestrator and would get a useless review.
- Have agent-runner edit `.validator/config.yml` directly after `agent-validator init` — **rejected**. Couples agent-runner to agent-validator's YAML schema; brittle.
- Route around the config entry (different agent-validator command that runs a built-in without a config entry) — **considered**. Would avoid this whole problem but bypasses the validator's "this review is configured for this entry point" model. Not pursued in this round.
- Extend `agent-validator init` so the orchestrator can request opt-in built-ins be scaffolded — **chosen**. agent-validator stays the owner of its config format; agent-runner only passes a flag at init time.

## Key Decisions

- **`task-compliance` stays opt-in.** The entry written must be `enabled: false`. Activation remains via the runtime `--enable-review task-compliance --context-file <path>` flags. Rationale: only the orchestrator knows which file is the active task, so the review must not run by default.
- **The fix lives in `agent-validator init`, not in `validator-setup` or as a runtime injection.** Init is the natural scaffolding point and is already invoked by agent-runner's onboarding via `cmd/agent-runner/internal_cmd.go`'s `runValidatorInit`.
- **The runtime flags on the agent-runner side (`--enable-review task-compliance --context-file {{task_file}}`) are the activation mechanism.** This handoff is about ensuring the config entry exists so those flags have something to flip on. Do not change activation semantics in agent-validator.

## Open Questions

These need answers before implementation.

### 1. Flag shape on `agent-validator init`

What should the new option look like? Three candidates discussed in the agent-runner session:

- **`--enable-builtin <name>` (repeatable).** Generic; mirrors agent-validator's own `--enable-review` naming; future-proofs for `test-integrity` and any other opt-in built-in. *Currently the leaning recommendation.*
- **`--orchestrator <name>` (e.g., `agent-runner`).** Semantic; says "I'm an orchestrator, scaffold the bits I need." Couples agent-validator to knowing about orchestrators by name.
- **`--with-task-compliance`.** Narrowest; one boolean per built-in. Easy now, awkward to extend.

### 2. Migration for existing `.validator/` directories

`src/commands/init.ts:148–166` (`existingConfigDir`) skips the entire scaffolding step when `.validator/` already exists. So passing the new flag in an existing project silently does nothing. Options:

- **Accept the gap.** Document that existing projects need to add the entry manually or by re-initializing. Minimum new surface area.
- **Add an idempotent companion command** in agent-validator, e.g. `agent-validator config add-review <name> --builtin <name> --enabled <bool>` (exact name TBD). agent-runner's `validator-init` can call it after `init` regardless of whether the scaffold ran. Helps anyone who configures the validator outside the agent-runner flow.
- **Make `init` patch existing configs when the flag is set.** Read the existing `config.yml`, ensure the entry is present in the entry point's `reviews:` list, write it back. Heavier lift; YAML-rewrite risk.

The companion-command option is the leaning recommendation because it cleanly handles the migration case without re-init.

### 3. Generalize now or just `task-compliance`?

agent-validator docs list two opt-in built-ins today: `task-compliance` and `test-integrity`. Options:

- **Generalize.** Flag accepts any built-in name; `selectReviewConfig` / `writeConfigYml` handle the list. agent-runner uses only `task-compliance` initially but the door is open.
- **Hardcode task-compliance.** Implement just for this one. Revisit when test-integrity becomes a need.

The leaning recommendation is generalize, because both candidate flag shapes (#1) point toward a list-of-names model and the cost is small.

## Next Steps

1. Decide the three open questions above (flag shape, migration approach, generalization scope). The agent-runner side of this work is paused until #1 and #2 are settled, since they shape the interface agent-runner has to call.
2. Implement in `agent-validator`:
   - **`src/commands/init.ts`** — register the new option(s) on the `init` command; thread it through `runInit` → `selectReviewsAndConfirmInstall` → `scaffoldValidatorDir` → `writeConfigYml`. Handle the existing-`.validator/` branch per the chosen migration approach.
   - **`src/commands/init-reviews.ts`** — extend `ReviewConfig` / `selectReviewConfig` (or add a sibling helper) to accept the set of opt-in built-ins to scaffold. Each opt-in built-in should produce a `ReviewEntry`-shaped record with `enabled: false`. Note: the current `ReviewEntry` type does not have an `enabled` field — it will need one (or a parallel type) so `writeConfigYml` can emit `enabled: false`.
   - **`src/commands/init-config-helpers.ts`** — update `writeConfigYml` to emit `enabled: false` on opt-in entries and include the explanatory comment shown below.
   - If pursuing the companion command, add a new `src/commands/config.ts` (or similar) and register it in `src/index.ts` (or wherever commands are registered).
3. Add tests covering: fresh init with the flag writes the entry; fresh init without the flag does not write the entry; (if implemented) the companion command is idempotent and additive.
4. Cut an agent-validator release containing the change.
5. Update agent-runner's `cmd/agent-runner/internal_cmd.go` `validatorInitArgs` to pass the new flag and document the minimum required agent-validator version. (This step happens in the agent-runner repo on the `task-compliance` branch.)

## Exact entry shape to write

The entry must match what previously lived in agent-runner's own `.validator/config.yml` (commit `a4f9151`, removed in `b9131a6`):

```yaml
entry_points:
  - path: "."
    reviews:
      # ... existing reviews ...
      - task-compliance:
          builtin: task-compliance
          enabled: false # Opt-in: activate with `agent-validator run --enable-review task-compliance --context-file <task>`
```

The comment is load-bearing for users discovering the entry by hand and should be preserved. The `model:` field that originally accompanied this entry (`model: claude-sonnet-4.6`) is **not** required — leave model selection to the orchestrator's defaults unless there's a clear reason to pin it.

## What agent-runner needs from this change

So the agent-validator interface can be designed against the consumer:

- **A flag on `agent-validator init`** that, when passed by agent-runner's `runValidatorInit` (`cmd/agent-runner/internal_cmd.go:138`), causes a fresh init to include a `task-compliance` opt-in entry. The shape of that flag is open question #1.
- **A migration path for projects that already ran `agent-validator init` before this change shipped.** This is open question #2. Without it, only brand-new agent-runner projects benefit; existing users who already initialized their validator config will silently keep getting no task-compliance review.
- **Stable, documented behavior on conflict.** If the entry already exists with different settings (e.g., user enabled it manually), the new mechanism should not clobber the user's value. Idempotent-and-additive semantics.
- **A version signal.** agent-runner will want to require a minimum agent-validator version so it knows the flag is available. Either bump the agent-validator version on release and have agent-runner document the minimum, or expose the supported flags in `agent-validator --version` / a capabilities command.

## Relevant Files

In `/Users/paul/codagent/agent-validator`:

- `src/commands/init.ts` — registers the `init` command (line ~47); the scaffolding flow runs through `runInit` → `selectReviewsAndConfirmInstall` → `scaffoldValidatorDir`. Existing-dir handling at lines 148–166.
- `src/commands/init-reviews.ts` — `ReviewEntry`, `ReviewConfig`, `selectReviewConfig`. Today only emits `code-quality + security-and-errors`, `all-reviewers`, or fallback `all-reviewers`. No `enabled` field on `ReviewEntry`.
- `src/commands/init-config-helpers.ts` — `writeConfigYml` writes the YAML based on a `ReviewConfig`. Needs to emit `enabled: false` and the explanatory comment for opt-in entries.
- `src/built-in-reviews/task-compliance.md` — the prompt itself; no changes needed here, included for reference.
- `docs/user-guide.md` and `docs/config-reference.md` — document `--enable-review` requiring a pre-configured entry. Should be updated to mention the new init flag and (if added) the companion command.

In `/Users/paul/codagent/agent-runner` (consumer side, paused on this handoff):

- agent-runner branch: `task-compliance`
- agent-runner change dir: `openspec/changes/task-compliance/` (this handoff lives here as `handoff-agent-validator.md`)
- `cmd/agent-runner/internal_cmd.go` — `runValidatorInit` at line 138 calls `agent-validator init`; `validatorInitArgs` at line 133 returns `["init"]`. Both need updating after the agent-validator change lands.
- `workflows/core/run-validator.yaml` — will gain the runtime `--enable-review task-compliance --context-file {{task_file}}` wiring (Problem 2; not in scope for this handoff).
- `workflows/core/implement-task.yaml` — will propagate its `task_file` param into the `run-validator` sub-workflow call.
- `.validator/config.yml` in this repo — will get the `task-compliance` entry restored as part of the agent-runner PR (Problem 1 for this repo only; the install-time fix for other projects is what this handoff is about).
