## Why

Agent configuration (mode, CLI backend, model) is scattered across individual workflow step definitions with no reuse or consistency guarantees. Changing the model for all headless steps means editing every workflow file. A centralized profile system lets us define agent configurations once and reference them by name, with inheritance to reduce duplication as the number of profiles grows.

## What Changes

- **New `.agent-runner/config.yaml` config file** with named agent profiles. Each profile specifies: mode (interactive/headless), cli (claude/codex), model, effort level (low/medium/high), and system prompt.
- **Profile inheritance** — a profile can `extends` another, inheriting all fields and overriding selectively.
- **Pre-generated default profiles**: `interactive_base`, `headless_base`, `planner`, `implementor`.
- **BREAKING**: The `mode` attribute on workflow steps is replaced by an `agent` attribute that references a profile name. The profile determines the execution mode.
- **All existing workflow files updated** to use `agent:` instead of `mode:` on every agent step.
- **CLI adapters extended** to accept and pass through the effort level parameter.

## Capabilities

### New Capabilities
- `agent-profiles`: Profile schema definition (name, mode, cli, model, effort, system_prompt), inheritance via `extends`, resolution from `.agent-runner/config.yaml`, default profile generation, and step-level `agent` attribute referencing a profile by name.

### Modified Capabilities
- `step-model`: The step `mode` field is removed. Replaced by `agent` field that references a named profile. The profile determines mode, cli, and model. Per-step `model`/`cli` override behavior to be decided in design.
- `cli-adapter`: Adapter arg construction extended to accept and pass through an effort level parameter to the underlying CLI.
- `workflow-execution`: Agent step dispatch resolves the named profile before building CLI args. Mode (interactive vs headless) is determined by the resolved profile, not the step definition.

## Out of Scope

- **Runtime profile switching** — profiles are static config, not selectable at workflow invocation time.
- **Profile validation beyond schema** — no checking that a profile's model is compatible with its CLI.
- **Environment-specific profiles** — no dev/staging/prod profile sets.
- **Validator config unification** — `.validator/config.yml` has its own CLI adapter config; unifying it with agent profiles is a separate concern.

## Impact

- **Workflow files**: All 5 workflow YAML files updated (plan-change, implement-task, implement-change, run-validator, smoke-test). Every agent step (~15 steps total) changes from `mode: interactive|headless` to `agent: <profile_name>`.
- **Step model** (`internal/model/step.go`): `Mode` field replaced or supplemented by `Agent` field. Validation logic updated.
- **CLI adapter** (`internal/cli/`): `BuildArgsInput` struct extended with effort field. Both Claude and Codex adapters updated to emit effort flags.
- **Agent executor** (`internal/exec/agent.go`): Profile resolution added before CLI dispatch. Mode determined from resolved profile.
- **New config loader**: Parses `.agent-runner/config.yaml`, validates profile schema and inheritance, resolves `extends` chains.
- **No external API changes** — this is internal runner configuration only.
