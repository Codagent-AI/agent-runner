## Why

Agent Runner currently infers access to its `call_agent` tool from a literal substring in an agent
step's prompt. That couples capability provisioning to prose, makes reviews and validation indirect,
and forces workflows to duplicate safety and reporting instructions that belong in a reusable skill.

## What Changes

- **BREAKING**: Add an explicit agent-step `tools` sequence and make `tools: [call_agent]` the only
  way a workflow can provision the Runner-owned agent-call integration. Prompt text no longer changes
  tool availability.
- Accept `call_agent` as the only initial Runner-owned tool; reject scalar, unknown, duplicate, and
  non-agent declarations. Omitted and empty lists on agent steps provision no Runner-owned tools.
- Drive control-channel setup, call eligibility, and process-local CLI adapter integration only from
  the validated tool declaration. Called children remain unable to receive `call_agent`.
- Migrate the call-capable OpenSpec v2 proposal review, approach review, task review, acceptance
  preparation, and targeted re-acceptance steps to declare `tools: [call_agent]`.
- Add a reusable `codagent:call-agent` skill to the sibling Agent Skills repository. The skill owns
  standalone child prompting, safe single-target invocation, honest failure handling, independent
  verification, and faithful reporting of both child findings and the lead's assessment.
- Replace duplicated generic call-operation prose in the migrated workflows with the skill invocation
  while preserving each step's review scope, approval gates, fix behavior, and call budget.
- Update Agent Runner's agent-call documentation, Agent Skills reference/workflow documentation, and
  the current agent-call behavioral specifications.
- After source validation, refresh the local Codex and Claude Codagent plugin installations so newly
  started sessions can discover `codagent:call-agent`.

## Capabilities

### New Capabilities

- `call-agent-skill`: Reusable orchestration rules for invoking one Runner-owned child at a time and
  handling its result without hiding failures or disagreements.

### Modified Capabilities

- `step-model`: Explicit static Runner-owned tool declarations and validation on agent steps.
- `agent-calls`: Availability and eligibility are based on declared tools instead of prompt content.
- `cli-adapter`: Process-local agent-call integration is provisioned from validated tool metadata.
- `step-control-channel`: Autonomous control context is created and delivered only for declared
  Runner-owned tools.
- `builtin-workflows`: Call-capable OpenSpec v2 steps declare the tool and delegate generic operation
  rules to `codagent:call-agent`.

## Out of Scope

- Runner-owned tools other than `call_agent`.
- Dynamic or interpolated tool declarations.
- Recursive agent calls or provisioning `call_agent` to called children.
- Compatibility fallback to prompt-substring detection.
- Changes to agent-call execution, session, concurrency, cancellation, metrics, or UI behavior beyond
  eligibility wording.
- Changes to the existing `openspec/changes/call-agent/` proposal, design, or task artifacts.
- Full Agent Validator execution, commits, releases, or version bumps.

## Impact

- Agent Runner model, YAML loading/validation, agent execution, control setup, CLI adapter input, and
  agent-call eligibility code under `internal/model/`, `internal/loader/`, `internal/exec/`,
  `internal/runner/`, `internal/control/`, and `internal/cli/`.
- Built-in OpenSpec workflows under `workflows/openspec/`.
- Agent Runner docs and current agent-call behavioral specs.
- The sibling Agent Skills repository at `/Users/paul/codagent/agent-skills`, including
  `skills/call-agent/SKILL.md` and skill reference/workflow documentation.
- Local Codex and Claude Codagent plugin installations, refreshed only after both repositories'
  source changes have passed their required checks.
