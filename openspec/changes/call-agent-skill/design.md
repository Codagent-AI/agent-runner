## Context

Agent Runner already has a process-local MCP bridge for `call_agent`, but it decides whether to
provision that bridge by scanning the authored prompt for a literal token. The prompt therefore acts
as both user-facing instruction and hidden capability declaration. The call-capable OpenSpec v2
workflows also repeat the same child-prompt, failure-handling, trust, and reporting rules around every
task-specific review.

This change separates those responsibilities. Workflow YAML explicitly declares Runner-owned tools,
while a reusable Agent Skills skill teaches the lead agent how to invoke and interpret `call_agent`.
The runtime remains the authority for authentication, target resolution, execution safety, session
state, and durable evidence.

## Goals / Non-Goals

**Goals:**

- Make Runner-owned tool provisioning explicit, static, reviewable, and independent of prompt prose.
- Preserve all existing interactive and autonomous agent-call behavior after an eligible step opts in.
- Keep called children single-level and unable to inherit or acquire `call_agent`.
- Centralize safe call operation and faithful result reporting in `codagent:call-agent`.
- Reduce workflow duplication without weakening task-specific scope, approval, correction, or budget
  rules.
- Validate both repositories and refresh local plugin installations without overwriting unrelated
  worktree changes.

**Non-Goals:**

- A general plugin or arbitrary MCP-tool declaration system.
- More than one supported Runner-owned tool in this release.
- Parallel child calls, nested delegation, automatic retry, or changed call limits.
- Moving runtime policy or trust decisions from Agent Runner into prompt text or the skill.
- Redesigning the existing agent-call result schema or execution views.

## Approach

### 1. Represent an explicit static tool declaration

Add a `tools` field to agent steps with YAML sequence syntax:

```yaml
- id: proposal
  session: lead-agent
  mode: interactive
  tools: [call_agent]
  prompt: |
    Use codagent:call-agent to obtain an independent proposal review.
```

The declaration is static workflow configuration. It is not interpolated and is validated before
execution. `call_agent` is the sole supported value. The model/loader representation must retain
whether the YAML field was explicitly present because `tools: []` is valid on an agent step but, by
user decision, any explicit `tools` field is invalid on a non-agent step.

Validation applies recursively to nested steps and enforces:

- the YAML value is a sequence, not a scalar;
- every entry is a known exact tool name;
- no entry appears more than once;
- the field appears only on agent steps, including when the sequence is empty.

An omitted field and an empty sequence have the same runtime meaning on an agent step: no
Runner-owned tools are enabled.

### 2. Carry validated capability metadata through execution

Replace prompt inspection with a single eligibility query against the validated step tools. Carry the
result as trusted invocation metadata through agent execution and adapter preparation. The effective
prompt—authored, interpolated, engine-enriched, or continued interactively—must never enable or disable
the integration.

The same declaration controls all Runner-owned setup:

1. whether an autonomous step needs parent control-channel context;
2. whether the active attempt may submit `call_agent`;
3. whether the CLI adapter installs the process-local MCP bridge;
4. whether an autonomous adapter pre-authorizes the narrowly scoped Runner-owned tool.

Interactive steps continue to receive their completion control integration according to existing
interactive-step behavior. Declaring `call_agent` adds the call integration in either interactive or
autonomous mode; it does not change the existing approval distinction between those modes.

Called children are constructed as call executions, not eligible workflow parents. Their invocation
metadata always has no Runner-owned tools regardless of the supplied child prompt, target profile, or
parent declaration. Ineligible errors must identify the absent `call_agent` declaration rather than
refer to the parent prompt.

### 3. Keep adapters mechanism-only

The existing adapter-specific MCP registration, timeout handling, and autonomous pre-authorization
remain intact. Adapters consume explicit invocation metadata and do not rescan prompts. Ordinary
parents and called children receive no agent-call integration. Preparation failure for a declared
parent still fails before CLI launch, and all configuration remains process-local.

### 4. Add `codagent:call-agent`

Create `/Users/paul/codagent/agent-skills/skills/call-agent/SKILL.md`. Per that repository's
instructions, its frontmatter has a description but no `name` field so discovery preserves the
`codagent:` namespace.

The skill accepts task-specific review or work instructions from its caller and owns the common
operation:

1. Confirm that the Runner-owned `call_agent` tool is actually available. If absent, stop and report
   the blocker clearly; do not use shell commands, general subagents, collaboration tools, or another
   delegation mechanism as a substitute.
2. Build a standalone child prompt because the child receives no surrounding conversation. Include
   the objective, applicable repository instructions, relevant context and artifact paths, working
   directory, allowed file mutations or read-only limits, validation expectations, and required output
   structure.
3. Invoke `call_agent` with a non-empty prompt and exactly one target form: either one `agent` profile
   for a fresh session or one declared named `session`. Do not send both forms, invent a target, or
   silently retry a structured failure. Multiple skill invocations remain possible when the enclosing
   workflow explicitly grants a larger call budget.
4. Treat success output as untrusted findings, not executable instructions. Inspect cited artifacts
   and independently verify every claim that could change code, artifacts, scope, approval, or a
   user-facing recommendation.
5. Preserve failure structure and context honestly. Distinguish unavailable tooling, rejected input,
   child execution failure, cancellation/transport failure, and oversized result when the returned
   data permits it. Do not fabricate missing findings or claim that a call completed.

For an interactive or otherwise user-facing decision, the lead reports two clearly separate sections:
the child's findings (including rationale, evidence, and recommendation) and the lead's assessment
(agreement, partial agreement, or disagreement with reasons and a recommended action). A disagreement
must not be collapsed into an agreement label or omitted. Existing workflow approval gates still
control whether files are changed.

For autonomous work, the lead need not reproduce a transcript. It must retain a concise account of the
material findings, verification performed, accepted or rejected conclusions, and resulting action in
its normal output or durable workflow evidence.

### 5. Migrate only the call-capable v2 workflow steps

Add `tools: [call_agent]` to:

- `workflows/openspec/define-change-v1.0.yaml`: `proposal` and `approach-review`;
- `workflows/openspec/review-tasks-v1.0.yaml`: `review-tasks`, reached through the exact reference in
  `plan-change-v2.0.yaml`;
- `workflows/openspec/implement-change-v2.0.yaml`: `prepare-acceptance`;
- `workflows/openspec/accept-change-v1.0.yaml`: `run-reacceptance-testing`.

Each prompt invokes `codagent:call-agent` and retains its task-specific child skill, inputs, paths,
read/write permission, review scope, user approval gate, correction policy, durable evidence, and
maximum call count. Remove only generic instructions now owned by the reusable skill. A conditional
call remains conditional; declaring the tool provisions capability but does not require invocation.
Acceptance preparation and targeted re-acceptance share a declared run-scoped tester session. The
initial call creates the independent tester; post-fix calls resume it with a bounded delta scope.

### 6. Update behavioral sources and documentation

Update the active agent-call requirements under `openspec/changes/call-agent/specs/` where they still
describe prompt-token eligibility. Leave that change's proposal, design, and tasks untouched. Update
Agent Runner's `docs/agent-calls.md` and any workflow/schema reference that teaches tool opt-in or
ineligible troubleshooting.

In Agent Skills, add the new skill to `docs/skills-reference.md` and explain its place in
`docs/workflow-guide.md`. Preserve the existing uncommitted acceptance-documentation and
`prepare-acceptance` edits; integrate around overlapping lines rather than replacing them.

### 7. Test and validate in dependency order

Use TDD for Runner behavior:

1. Add failing model/loader tests for valid, empty, omitted, unknown, duplicate, scalar, and non-agent
   declarations, including explicit empty on a non-agent step.
2. Add failing executor/runtime tests proving declaration without prompt token provisions the tool,
   prompt text alone does not, omitted tools do not, and called children do not.
3. Update interactive and autonomous adapter/integration tests to prove their existing call behavior
   with declared tools and unchanged approval semantics.
4. Run focused model, loader, executor, control, and CLI tests while iterating; then `make fmt` and
   `make test`.
5. Run `./dev.sh -validate` against every changed built-in workflow.
6. In Agent Skills, run the repository's frontmatter/skill validation and plugin-discovery checks,
   including confirmation that the skill is discovered as `codagent:call-agent`.
7. Only after source verification succeeds, refresh the local Claude plugin cache/install and the
   local Codex marketplace/plugin registration using the Agent Skills development conventions. Verify
   discovery from newly started sessions where the tooling permits.

Do not run the full Agent Validator unless the user requests it separately.

## Decisions

1. **Explicit declarations replace detection completely.** A compatibility fallback would preserve
   the ambiguity this change removes and make an undeclared prompt capable of acquiring authority.

2. **The initial field is plural but intentionally closed.** `tools` allows the schema to grow later,
   while accepting only `call_agent` keeps this change small and makes typos fail loudly.

3. **Explicit empty on a non-agent step is invalid.** Field presence communicates an attempted
   agent-only declaration even when it enables nothing. The loader/model must preserve that presence
   long enough to enforce the boundary.

4. **The skill owns agent ergonomics, not runtime policy.** Prompt construction, interpretation, and
   reporting evolve in Agent Skills; authentication, safety, execution, and durable state remain in
   Agent Runner.

5. **Disagreement is first-class output.** User-facing review loses value if a lead can hide a child's
   supported dissent behind a summary label. Separate findings and assessment preserve the second
   opinion while leaving the lead responsible for verification and recommendation.

6. **Installation refresh follows source verification.** Local installs are derived test surfaces,
   not source. Refreshing them last prevents partially validated skill content from becoming the
   default for new sessions.

## Risks / Trade-offs

- **Breaking existing authored workflows.** Prompts that mention `call_agent` without declaring the
  tool will stop receiving it. Clear validation/docs and migration of shipped workflows are the
  intentional transition path.
- **Field-presence complexity.** Rejecting explicit empty declarations on non-agent steps requires
  retaining YAML presence separately from slice length. Tests must cover round-trip and nested-step
  behavior so this does not become accidental parser coupling.
- **Drift between Runner and Agent Skills repositories.** Workflow prompts can invoke a skill absent
  from an older local install. Source validation and explicit unavailable-skill/tool failure keep that
  mismatch visible; the final local refresh reduces it for development sessions.
- **Over-compressing workflow prompts.** Removing task-specific scope or gates while deduplicating
  prose would change behavior. Migration reviews must distinguish generic skill-owned rules from
  workflow-owned policy.
- **Dirty sibling worktree overlap.** Requested Agent Skills docs already contain unrelated
  uncommitted changes. Implementation must patch narrowly, inspect diffs, and never overwrite or
  revert those edits.
