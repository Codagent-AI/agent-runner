---
title: CLI Reference
group: Reference
order: 6
description: Commands and flags generated from current agent-runner help output.
---

# CLI Reference

<!-- Generated from 'agent-runner --help'. Update when flags change. -->

Agent Runner exposes a flag-first CLI plus lightweight `run` and `debug` command forms. This reference is based on current `agent-runner --help`, `agent-runner run --help`, and `agent-runner debug --help` output.

## Usage

```text
agent-runner [flags] [workflow [params...]]
```

With no arguments, Agent Runner opens the TUI. With a workflow argument, it
resolves and runs the latest definition for that version-free logical name.
Fresh runs reject workflow filenames, explicit version suffixes, and filesystem
paths.

## Global Flags

| Flag | Value | Description |
| --- | --- | --- |
| `-C` | `<dir>` | Change to `directory` before doing anything. |
| `-inspect` | `<run-id>` | Launch the run view TUI for a specific run. |
| `-list` | none | Launch the run list TUI. |
| `-resume` | optional `session-id` | Resume an interrupted workflow; launches TUI if no session ID is given. |
| `-reset-onboarding` | none | Clear onboarding settings, project `.validator/`, and saved onboarding runs before launching. |
| `-onboarding-from` | `<step-id>` | Start the built-in onboarding workflow from a top-level step. |
| `-validate` | none | Validate a logical workflow or exact versioned workflow file without executing. |
| `-v` | none | Print version and exit. |
| `-version` | none | Print version and exit. |

## Run Command

```text
agent-runner run <workflow> [--until <step-id>] [--param key=value] [key=value ...]
```

`agent-runner run` is a command-form alias for starting a workflow. `--param key=value` and `--param=key=value` are normalized into workflow parameters before execution.

The `<workflow>` argument is a version-free logical name such as `deploy`,
`team/deploy`, or `openspec:plan-change`. It is not a filename or path.

`--until <step-id>` stops successfully after the named top-level step is reached. The target step is inclusive: it runs before the workflow stops. If runtime conditions skip the target step, the workflow still stops at that position. The step ID is validated before execution begins, and nested loop or sub-workflow step IDs cannot be targeted.

Examples:

```bash
agent-runner run openspec:plan-change --param change_name=my-change
agent-runner run spec-driven:change --param change_name=my-change
agent-runner run spec-driven:change --param change_name=my-change --until accept
```

## Debug Command

`agent-runner debug` prints read-only debugging information. Exactly one debug flag must be supplied.

| Flag | Value | Description |
| --- | --- | --- |
| `--state` | `<run-id>` | Print run state JSON. |
| `--state-dir` | `<session-dir>` | Print run state JSON from a session directory. |
| `--audit-summary` | `<run-id>` | Print a redacted audit summary. |
| `--audit-summary-dir` | `<session-dir>` | Print a redacted audit summary from a session directory. |
| `--show-workflow` | `<workflow-ref>` | Print workflow YAML. |

Examples:

```bash
run_id="replace-with-run-id"
session_dir="/path/to/session-dir"

agent-runner debug --state "$run_id"
agent-runner debug --audit-summary "$run_id"
agent-runner debug --state-dir "$session_dir"
agent-runner debug --audit-summary-dir "$session_dir"
agent-runner debug --show-workflow openspec:plan-change
agent-runner debug --show-workflow builtin:openspec/plan-change-v1.0.yaml
```

`--show-workflow` is read-only. A logical namespaced reference shows the latest
embedded version; an exact `builtin:<namespace>/<file>-v<major>.<minor>.yaml`
reference or exact on-disk versioned path shows that specific definition.

## Validate

`-validate` validates workflow loading and parameters without executing the workflow.

```bash
agent-runner -validate openspec:plan-change change_name=my-change
```

`-validate` accepts workflow parameters only as `key=value`.

The workflow argument may be a version-free logical name, which resolves to the
latest version, or an existing exact versioned `.yaml`/`.yml` path. Exact paths
are for validation and inspection; they do not create a historical-version
launch channel.

## Resume

`-resume` without an ID opens the run list. `-resume <run-id>` resumes an
unfinished run from the exact workflow file recorded in its state. If the run
is already complete, the same command opens its read-only inspection view
instead of executing it again.
