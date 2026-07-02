# Task: Guided workflow and validator sub-workflows

## Goal

Create the `guided-workflow.yaml` (Phase 3) and `validator.yaml` (Phases 4+5) onboarding sub-workflows. These walk the user through planning a real task, getting tutored, implementing it autonomously, then setting up Agent Validator and running a real validation feedback loop.

## Background

### Existing patterns

The existing `workflows/onboarding/step-types-demo.yaml` is the model for onboarding sub-workflows. Study its structure: `mode: ui` steps with title/body/actions, interactive/autonomous agent steps with `session: new`, shell steps with `capture`, and `skip_if` gating. Follow the same conventions.

Named sessions are declared at the workflow level via `sessions:` blocks. Named session maps are shared by reference across sibling sub-workflows (`internal/model/context.go:322-325`), so a named session created in one sub-workflow is visible to later sibling sub-workflows under the same parent.

### guided-workflow.yaml (Phase 3)

Declares three named sessions:
- `planning-session` (agent: planner)
- `tutor-session` (agent: planner)
- `impl-session` (agent: implementor)

Steps in order:

1. **intro-ui** — `mode: ui`. Explains what Phase 3 does: the user will plan a real task, get guidance from a tutorial agent, and watch it get implemented autonomously.
2. **capture-cwd** — `command: pwd`, capture → `cwd`.
3. **check-existing-project** — shell step that captures whether the directory is empty, outside Git, or an existing Git project.
4. **existing-project-required** — `mode: ui`. Shows an exit screen when the directory is empty or not inside Git.
5. **require-existing-project** — `command: test "{{project_status}}" = "ok"`. Stops the workflow when the project guard fails.
6. **confirm-cwd** — `mode: ui`. Body shows `{{cwd}}` and notes the user can press Esc to abort if this is the wrong directory. Single Continue action.
7. **check-git-clean** — `command: git status --porcelain 2>&1; true`. Always succeeds (the `; true` ensures exit 0). Capture → `git_status`.
8. **warn-dirty** — `mode: ui`. `skip_if: 'sh: [ -z "{{git_status}}" ]'`. Warns the directory may not be in a clean state. Single Continue action.
9. **create-plan-dir** — `command: mktemp -d`, capture → `plan_dir`. Creates a temp directory outside the project for plan artifacts.
10. **explain-plan** — `mode: ui`. Explains the interactive planning step.
11. **plan** — `session: planning-session`, `mode: interactive`. Prompt instructs the agent to use `codagent:simple-plan` and write all artifacts to `{{plan_dir}}`. Suggests small-task examples (fix a typo, add a log line, rename a variable) but does not enforce scope. Tells the agent: "First, ask the user what the change is about. DO NOT attempt to guess."
12. **locate-task** — `session: planning-session`, `mode: autonomous`. Prompt: emit the path to the produced task file on a single line with no other text. Capture → `task_file`.
13. **validate-plan** — `command: test -f "{{task_file}}"`. Fails and stops the workflow if the task file does not exist.
14. **explain-tutor** — `mode: ui`. Explains the tutorial agent: a separate session that will review the plan and answer questions.
15. **tutor** — `session: tutor-session`, `mode: interactive`. Prompt includes `{{task_file}}` so the tutor can read and reference the plan. Tells the tutor to provide guidance contextualized to the plan (e.g., "I see you made a plan to X") and preview that an autonomous implementor will execute it next. References bundled docs at `{{session_dir}}/bundled/onboarding/docs/` for general AR Q&A.
16. **explain-impl** — `mode: ui`. Explains autonomous implementation: an implementor agent will now execute the plan autonomously.
17. **implement** — `session: impl-session`, `mode: autonomous`. Prompt references `{{task_file}}` and instructs the agent to use `codagent:implement-with-tdd`. The prompt tells the agent to stage implementation changes with `git add` and NOT commit changes.
18. **summary** — `mode: ui`. Body instructs the user to review with `git diff` if desired, but not to commit yet because the next onboarding phase configures Agent Validator and validates the uncommitted changes.

### validator.yaml (Phases 4+5)

Declares three named sessions:
- `validator-setup-session` (agent: planner)
- `tutor-session` (agent: planner)
- `impl-session` (agent: implementor) — this merges with guided-workflow's declaration because it has the same name and agent. `MergeSessionDecls` handles this automatically.

Steps in order:

1. **intro-ui** — `mode: ui`. Explains what Agent Validator is and why validation matters.
2. **stash-guided-changes** — shell step that stashes guided workflow changes so setup runs against the project baseline.
3. **init** — `command: '"$AGENT_RUNNER_EXECUTABLE" internal validator-init'`, `mode: interactive`. Creates validator configuration using the embedded Agent Runner helper.
4. **setup** — `session: validator-setup-session`, `mode: interactive`. Prompt instructs the agent to use `agent-validator:validator-setup` to configure checks and reviews.
5. **restore-guided-changes** — shell step that restores the guided workflow changes after setup.
6. **explain-validation** — `mode: ui`. Explains that onboarding will intentionally introduce a small defect, then run validation.
7. **break-it** — `session: tutor-session`, `mode: autonomous`. Introduces a small scoped defect in the guided-workflow changes so validation has something realistic to catch.
8. **prepare-fix-context** — `session: impl-session`, `mode: autonomous`. Brief prompt: "Briefly acknowledge you are ready to fix any validation failures found next. Output a single line with no other text: Ready". Purpose: make `impl-session` the most recent session in this workflow's context so `run-validator.yaml`'s `session: inherit` finds it when crossing the sub-workflow boundary.
9. **run-validator** — `workflow: ../core/run-validator.yaml`, `continue_on_failure: true`. This is the existing retry loop (max 3 iterations). Do NOT duplicate or modify `run-validator.yaml`. Reference it as-is.
10. **review-validator-status** — `session: tutor-session`, `mode: interactive`. Uses the validator help skill and runtime artifacts to explain what happened, including whether the intentional defect was caught and fixed.
11. **summary-ui** — `mode: ui`. Explains what happened during validation and the feedback-loop concept: how iterating between validation and fixes creates reliability.

### Key files

- `workflows/onboarding/step-types-demo.yaml` — reference for conventions, step structure, prompt style
- `workflows/onboarding/onboarding.yaml` — current top-level chain (do NOT modify in this task)
- `workflows/core/run-validator.yaml` — the existing validator retry loop referenced as a sub-workflow
- `workflows/embed_test.go` — add resolution tests for new sub-workflows
- `workflows/onboarding_step_types_demo_test.go` — reference for workflow shape test patterns

### Bundled docs

Agent steps that need docs reference `{{session_dir}}/bundled/onboarding/docs/`. The source files live under `workflows/onboarding/docs/` and are automatically embedded and materialized at runtime. Do not duplicate docs elsewhere.

### Testing

Add tests following the patterns in `workflows/embed_test.go` and `workflows/onboarding_step_types_demo_test.go`:

- **Embedding resolution**: verify `Resolve("onboarding:guided-workflow")` and `Resolve("onboarding:validator")` return the correct builtin refs.
- **Workflow shape tests**: verify step IDs are in the expected order, verify step types (UI vs interactive vs autonomous vs shell), verify session declarations, verify the `skip_if` on the warn-dirty step, verify `impl-session` is declared in both workflows.

## Spec

### Requirement: Guided workflow step sequence

The `guided-workflow` onboarding sub-workflow SHALL execute steps in this order: intro UI → capture cwd → existing-project guard → confirm cwd → capture git status → warn if dirty → create plan directory → explain planning → interactive planning → autonomous task-file capture → validate task file → explain tutor → interactive tutor → explain implementation → autonomous implementation → summary UI. Each agent step (planning, tutor, implementation) SHALL be preceded by an informational `mode: ui` screen explaining what the next step does.

#### Scenario: Guided workflow starts with intro
- **WHEN** `guided-workflow` starts
- **THEN** the first step is an intro UI screen explaining what Phase 3 does

#### Scenario: Agent steps are preceded by explanation screens
- **WHEN** each agent step (planning, tutor, implementation) is about to run
- **THEN** it is preceded by an informational `mode: ui` screen

### Requirement: Directory confirmation

The workflow SHALL capture the current working directory via a shell step and display it in a `mode: ui` confirmation screen. The screen SHALL show the captured directory path and have a single Continue action. The user can press Esc to abort the workflow if the directory is wrong.

#### Scenario: Confirmation screen displays working directory
- **WHEN** the directory confirmation screen renders
- **THEN** it displays the captured working directory path

#### Scenario: Continue proceeds to git check
- **WHEN** the user selects Continue on the confirmation screen
- **THEN** the workflow proceeds to the git-cleanliness check

### Requirement: Soft git-cleanliness guard

The workflow SHALL capture the output of `git status --porcelain` in a shell step. A `mode: ui` warning screen SHALL render only when the captured output is non-empty or the shell step failed (not a git repo). The warning SHALL use a single message for both dirty-tree and non-git cases. The warning screen SHALL have a single Continue action; the user can press Esc to abort.

#### Scenario: Clean git repo skips warning
- **WHEN** `git status --porcelain` output is empty
- **THEN** the warning screen is skipped

#### Scenario: Dirty tree shows warning
- **WHEN** `git status --porcelain` output is non-empty
- **THEN** the warning screen renders

#### Scenario: Non-git directory shows warning
- **WHEN** the git status command fails
- **THEN** the warning screen renders

#### Scenario: Continue on warning proceeds to planning
- **WHEN** the user selects Continue on the warning screen
- **THEN** the workflow proceeds to the planning step

### Requirement: Planning step uses simple-plan skill

The planning step SHALL use the `planner` agent in a named `planning-session`, interactive mode. The prompt SHALL instruct the agent to use `codagent:simple-plan` and write plan artifacts to a location outside the project directory. The prompt SHALL suggest small-task examples (fix a typo, add a log line, rename a variable) but SHALL NOT enforce a hard constraint on task scope.

#### Scenario: Planning runs interactively with planner
- **WHEN** the planning step starts
- **THEN** it runs as an interactive session using the `planner` agent in the `planning-session`

#### Scenario: Planner suggests but does not enforce scope
- **WHEN** the planner discusses the task with the user
- **THEN** it suggests small-task examples without refusing larger tasks

#### Scenario: Plan artifacts written outside project
- **WHEN** the planner writes plan artifacts
- **THEN** they are written to a location outside the project directory

### Requirement: Plan location capture and validation

After the interactive planning step, an autonomous step SHALL resume `planning-session` and emit the path to the produced task file on a single line, captured into `task_file`. A subsequent shell step SHALL validate that the file at `{{task_file}}` exists. If the task file does not exist, the validation step SHALL fail and the workflow SHALL stop.

#### Scenario: Capture step emits task file path
- **WHEN** planning completes and the capture step resumes the planning session
- **THEN** it emits the task file path as a single-line capture into `task_file`

#### Scenario: Valid task file proceeds to tutor
- **WHEN** the task file exists at the captured path
- **THEN** the workflow proceeds to the tutor step

#### Scenario: Missing task file stops workflow
- **WHEN** the task file does not exist at the captured path
- **THEN** the validation step fails and the workflow stops

### Requirement: Tutorial step provides contextualized guidance

The tutor step SHALL run as an interactive step in a separate named `tutor-session` using the `planner` agent. The prompt SHALL include `{{task_file}}` so the tutor can read and reference the plan. The tutor SHALL provide guidance contextualized to the plan outcome and explain what the upcoming implementation step will do. The tutor SHALL have access to bundled Agent Runner documentation. The tutor SHALL support general Agent Runner Q&A beyond the plan.

#### Scenario: Tutor runs in separate session
- **WHEN** the tutor step starts
- **THEN** it runs in a `tutor-session` separate from the `planning-session`

#### Scenario: Tutor prompt includes task file
- **WHEN** the tutor's prompt is constructed
- **THEN** it includes the `{{task_file}}` path

#### Scenario: Tutor answers general AR questions
- **WHEN** the user asks general Agent Runner questions during the tutor step
- **THEN** the tutor can answer using bundled documentation

### Requirement: Implementation step executes the task

The implementation step SHALL run as an autonomous step using the `implementor` agent in a named `impl-session`. The prompt SHALL reference `{{task_file}}` and instruct the agent to use `codagent:implement-with-tdd`. The implementation step SHALL NOT commit changes automatically; changes SHALL remain uncommitted.

#### Scenario: Implementation runs autonomously with implementor
- **WHEN** the implementation step starts
- **THEN** it runs autonomously with the `implementor` agent in the `impl-session`

#### Scenario: Implementor receives task file path
- **WHEN** the implementor receives its prompt
- **THEN** the prompt contains the `{{task_file}}` path

#### Scenario: Changes remain uncommitted
- **WHEN** the implementation step completes
- **THEN** changes remain uncommitted in the working tree

### Requirement: Summary screen directs user to review

The summary UI screen SHALL instruct the user to run `git diff` to review the changes if desired, but not to commit yet because the next phase configures Agent Validator and validates the uncommitted changes. The summary SHALL NOT show a list of changed files or run post-implementation checks.

#### Scenario: Summary renders after implementation
- **WHEN** the implementation step completes successfully
- **THEN** the summary UI screen renders

#### Scenario: Summary instructs git diff
- **WHEN** the summary screen renders
- **THEN** it instructs the user to run `git diff` if desired and not commit yet

### Requirement: Guided workflow failure leaves onboarding incomplete

When the `guided-workflow` sub-workflow fails or is cancelled before completing, the top-level `onboarding:onboarding` workflow SHALL NOT write `settings.onboarding.completed_at`.

#### Scenario: Failure does not write completion
- **WHEN** the guided-workflow fails or is cancelled
- **THEN** `settings.onboarding.completed_at` is not written

### Requirement: Validator sub-workflow step sequence

The `validator` onboarding sub-workflow SHALL execute steps in this order: intro UI → stash guided changes → validator init shell step → interactive validator setup → restore guided changes → explain-validation UI → intentional break step → prepare fix context → validator retry loop → interactive validator review → summary UI.

#### Scenario: Validator workflow starts with intro
- **WHEN** the validator sub-workflow starts
- **THEN** the first step is an intro UI screen explaining what Agent Validator is and why validation matters

#### Scenario: Init runs before setup
- **WHEN** the intro UI completes
- **THEN** validator initialization runs before the interactive setup step

### Requirement: Validator initialization

A shell step SHALL run Agent Runner's internal validator initialization helper to create the `.validator` configuration directory before the setup agent runs. If the init step fails, the workflow SHALL stop.

#### Scenario: Init creates configuration
- **WHEN** the init step runs successfully
- **THEN** `.validator/config.yml` is created

#### Scenario: Init failure stops workflow
- **WHEN** validator initialization fails
- **THEN** the workflow stops

### Requirement: Validator setup via skill

An interactive step using the `planner` agent in a named `validator-setup-session` SHALL run `agent-validator:validator-setup` to configure checks and reviews. The skill handles check discovery, user confirmation, review gate configuration, and config validation.

#### Scenario: Setup runs interactively with planner
- **WHEN** the setup step starts
- **THEN** it runs interactively with the `planner` agent in the `validator-setup-session`

#### Scenario: Setup configures checks
- **WHEN** the setup step completes
- **THEN** at least one check is configured in `.validator/config.yml`

### Requirement: Validation run with retry loop

The validation step SHALL use a retry loop (max 3 iterations) matching the production `run-validator.yaml` pattern: run `agent-validator run --report`, capture output, continue on failure, break on success. On failure, an autonomous implementor step SHALL fix violations. The validator SHALL run on uncommitted changes from the guided-workflow phase; no manual commit step is required between phases.

#### Scenario: Validator passes on first run
- **WHEN** the validator passes on the first iteration
- **THEN** the loop exits after one iteration

#### Scenario: Validator fails and implementor fixes
- **WHEN** the validator fails on an iteration
- **THEN** the autonomous implementor attempts to fix violations

#### Scenario: Validator re-runs after fix
- **WHEN** the implementor fixes violations
- **THEN** the validator re-runs on the next iteration

#### Scenario: Retry limit exhausted
- **WHEN** 3 iterations are exhausted without the validator passing
- **THEN** the loop exits

#### Scenario: Validator runs on uncommitted changes
- **WHEN** the validation step runs
- **THEN** it operates on uncommitted changes from the guided-workflow phase without requiring a prior commit

### Requirement: Validator summary explains feedback loop

The summary UI screen SHALL explain what happened during the validation run and describe the feedback-loop concept: how iterating between validation and fixes creates reliability.

#### Scenario: Summary renders after validation
- **WHEN** the validation loop completes
- **THEN** the summary UI screen renders

#### Scenario: Summary explains feedback loop
- **WHEN** the summary screen renders
- **THEN** it explains the feedback-loop concept

## Done When

- `workflows/onboarding/guided-workflow.yaml` exists with all 18 steps in the correct order, correct session declarations, correct step types and modes, and complete prompts.
- `workflows/onboarding/validator.yaml` exists with all 11 steps, declares `impl-session` (same name+agent as guided-workflow for cross-workflow sharing), includes `prepare-fix-context` before calling `run-validator.yaml`, and references `../core/run-validator.yaml` as a sub-workflow.
- Embedding resolution tests pass for both new workflows.
- Workflow shape tests verify step IDs, step types, session declarations, and `skip_if` conditions.
- `run-validator.yaml` is NOT modified.
