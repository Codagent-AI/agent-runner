---
title: Troubleshooting
group: Reference
order: 9
description: Common Agent Runner failures and recovery commands.
---

# Troubleshooting

Most Agent Runner failures are workflow resolution, parameter, session, or continuation issues. The sections below list the common causes and the command or YAML change that fixes each one.

## Missing Required Parameter

Pass the parameter positionally or as `key=value`.

```bash
agent-runner review-pr 42
agent-runner review-pr pr_number=42
```

## Unknown Workflow

Use a version-free project/user logical name, a path-like logical name under
`.agent-runner/workflows`, or a built-in name such as `core:run-validator`.

```bash
agent-runner core:run-validator
agent-runner openspec:plan-change my-change
```

Do not launch `deploy-v1.0`, `deploy-v1.0.yaml`, or a filesystem path. Author
the file as `deploy-v1.0.yaml`, keep its YAML name as `deploy`, and launch it as
`agent-runner deploy`.

## Workflow Filename Must Be Versioned

Rename authored workflow files to
`<logical-name>-v<major>.<minor>.yaml` (or `.yml`) while keeping the YAML
`name:` version-free. Update every parent workflow to pin the exact versioned
child reference.

An unfinished legacy on-disk run whose state records `deploy.yaml` cannot
silently move to `deploy-v1.0.yaml`; start a new run after the rename. If the
state records an unversioned `builtin:` reference, restart with the current
binary or finish the old run with the older binary. Embedded files cannot be
renamed by the user.

## Recorded Workflow Version Is Missing

Resume intentionally does not replace a missing recorded version with a newer
sibling. Restore the exact recorded file to its original path, or inspect the
run without resuming it. Completed runs can still open from saved state and
audit evidence.

Workflow versions are not immutable archives. Editing a versioned file in
place is supported, and resume warns on a hash change but continues. Fresh runs
always select the latest logical version, so rolling new runs back requires
publishing a numerically newer definition containing the rollback—not launching
an older filename directly.

## Unknown CLI Adapter

Valid step-level `cli` values are:

| CLI |
| --- |
| `claude` |
| `codex` |
| `copilot` |
| `cursor` |
| `opencode` |

## Agent Step Requires Agent

Fresh sessions need an agent profile. Add `agent: planner`, `agent: implementor`, or another configured profile, or use `session: resume`, `session: inherit`, or a declared named session when that is what you intend.

```yaml
- id: plan
  agent: planner
  prompt: "Plan the change."
```

## Interactive Step Will Not Advance

The workflow advances when the current interactive step sends a completion event:

| Method | Action |
| --- | --- |
| Ask the agent | Tell the agent to continue to the next workflow step. It should run the completion client from its injected instructions. |
| Native completion command | Type `/agent-runner:next` in Claude, Copilot, or Cursor. In Codex, invoke `$agent-runner-next`. |

There is no Agent Runner continuation overlay or global keyboard shortcut. If the agent does not respond, quit the CLI and resume the run. Exiting before completion is accepted records the step as aborted.

## Debug A Run

From an inactive run detail view, press `d` to launch the built-in debug workflow for that run.

You can also launch the workflow directly:

```bash
run_id="replace-with-run-id"
session_dir="/path/to/session-dir"

agent-runner run core:debug
agent-runner run core:debug failed_run_id="$run_id"
agent-runner run core:debug failed_session_dir="$session_dir"
```

Read-only debug inspection commands are available for state, audit summaries, and embedded workflow YAML:

```bash
run_id="replace-with-run-id"
session_dir="/path/to/session-dir"
workflow_ref="openspec:plan-change"

agent-runner debug --state "$run_id"
agent-runner debug --audit-summary "$run_id"
agent-runner debug --state-dir "$session_dir"
agent-runner debug --audit-summary-dir "$session_dir"
agent-runner debug --show-workflow "$workflow_ref"
```

## Resume A Run

Use the TUI:

```bash
agent-runner -resume
```

Or resume a specific run ID from the current project:

```bash
run_id="replace-with-run-id"
agent-runner -resume "$run_id"
```

If that run is complete, the command opens the same read-only view as
`agent-runner -inspect "$run_id"`.
