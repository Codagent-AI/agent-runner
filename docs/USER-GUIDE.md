# Agent Runner User Guide

Agent Runner runs YAML workflows that coordinate agent CLIs, shell commands, scripts, UI prompts, loops, and sub-workflows. It persists run state outside the agent so workflows can be inspected and resumed.

## Getting Started

### Prerequisites

- At least one supported agent CLI installed and authenticated: `claude`, `codex`, `copilot`, `cursor`, or `opencode`
- Optional: `openspec`, if using the `openspec:*` built-in workflows
- Optional: `agent-validator`, if using the built-in validation workflows and not installing Agent Runner through Homebrew

### Install

Homebrew is the preferred install path on macOS and Linux:

```bash
brew tap Codagent-AI/tap
brew install --cask agent-runner
```

The Homebrew cask installs Agent Runner and its helper CLIs, including
`agent-plugin` and `agent-validator`.

On Linux without Homebrew, download the latest release tarball for your
architecture and install the helper CLIs separately:

```bash
curl -LO https://github.com/Codagent-AI/agent-runner/releases/latest/download/agent-runner_linux_amd64.tar.gz
tar xzf agent-runner_linux_amd64.tar.gz
sudo mv agent-runner /usr/local/bin/
npm install -g agent-validator @codagent-ai/agent-plugin
```

For local development and source builds, see [development.md](development.md).

## Running Agent Runner

```bash
agent-runner
```

With no arguments, Agent Runner opens the TUI. From there you can browse workflow definitions, start a run, inspect previous runs, or resume an interrupted run.

Common commands:

```bash
agent-runner -list
agent-runner -inspect <run-id>
agent-runner -resume
agent-runner -resume <run-id>
agent-runner -validate openspec:plan-change change_name=my-change
agent-runner openspec:plan-change my-change
agent-runner -C /path/to/project spec-driven:change
agent-runner -version
```

`-validate` accepts workflow parameters only as `key=value`. Normal runs accept positional parameters, `key=value` parameters, or a mix of both.

## Workflow Discovery

Workflow names resolve in this order:

1. `.agent-runner/workflows/<name>.yaml` or `.yml` in the current project
2. `~/.agent-runner/workflows/<name>.yaml` or `.yml`
3. Built-ins using `<namespace>:<name>`

Examples:

```bash
agent-runner deploy
agent-runner team/deploy prod
agent-runner core:run-validator
agent-runner openspec:plan-change my-change
agent-runner spec-driven:simple-change
```

Built-in namespaces currently include `core`, `openspec`, `spec-driven`, and `onboarding`.

## Basic Workflow

```yaml
name: hello
description: "A simple two-step workflow"

steps:
  - id: greet
    agent: planner
    prompt: "Say hello and list the files in the current directory."

  - id: summarize
    session: resume
    prompt: "Summarize what you found."
```

The first agent step defaults to `session: new`, so it must specify an `agent` profile. Later agent steps default to `session: resume` and continue the most recent session unless you set a different session.

Run it:

```bash
agent-runner hello
```

## Parameters

Workflow parameters are declared in `params:` and referenced as `{{name}}`.

```yaml
name: review-pr

params:
  - name: pr_number
    required: true

steps:
  - id: fetch
    command: gh pr checkout {{pr_number}}

  - id: review
    agent: planner
    prompt: "Review PR {{pr_number}}."
```

Run with positional or keyed arguments:

```bash
agent-runner review-pr 42
agent-runner review-pr pr_number=42
```

`required` defaults to `true`. Sub-workflows can also use `default` values for omitted parameters.

## Built-In Variables

Every step can reference:

| Variable | Value |
| --- | --- |
| `{{session_dir}}` | Absolute path to the current run directory, such as `~/.agent-runner/projects/<encoded-cwd>/runs/<run-id>`. |
| `{{step_id}}` | Current step ID. |

Workflow parameters and captured variables shadow built-ins with the same name.

## Agent Profiles

Agent steps use named profiles. Profiles define the default mode, CLI, model, effort, and optional system prompt.

Built-in defaults include:

```yaml
profiles:
  default:
    agents:
      interactive_base:
        default_mode: interactive
        cli: claude
        model: opus
        effort: high
      autonomous_base:
        default_mode: autonomous
        cli: claude
        model: opus
        effort: high
      planner:
        extends: interactive_base
      implementor:
        extends: autonomous_base
      summarizer:
        default_mode: autonomous
        cli: claude
        model: haiku
        effort: low
```

Config is layered in this order:

1. Built-in defaults
2. Global config: `~/.agent-runner/config.yaml`
3. Project config: `.agent-runner/config.yaml`

Project config wins over global config. Project config may set `active_profile`; global config may not.

Agent step-level `mode`, `cli`, and `model` override the resolved profile for that step.

## Agent Steps

```yaml
- id: plan
  agent: planner
  prompt: "Plan the change."

- id: implement
  agent: implementor
  session: new
  mode: autonomous
  prompt: "Implement the plan."

- id: review
  session: resume
  mode: autonomous
  prompt: "Review what you just changed."
```

Supported CLI adapters are `claude`, `codex`, `copilot`, `cursor`, and `opencode`.

Interactive steps run inside a PTY. The workflow advances when the user types `/next`, presses Ctrl-], or when the agent emits the continuation marker injected by Agent Runner. Workflow prompts may describe this as "complete the step" or "signal continuation": the agent should answer the user's question, then use its injected continuation-marker instruction to end the step. If the CLI exits without any continuation trigger, the step is treated as aborted so you can resume the workflow later.

Autonomous steps run without user interaction. Depending on `~/.agent-runner/settings.yaml`, autonomous steps may run in headless mode or in an interactive backend with autonomy instructions. Capturing an autonomous agent step forces headless execution so stdout can be captured reliably.

## Sessions

Agent steps support these session strategies:

| Session | Meaning |
| --- | --- |
| `new` | Start a fresh session using the step's `agent` profile. |
| `resume` | Resume the most recent session in the current workflow context. |
| `inherit` | In a sub-workflow, resume the parent workflow's most recent session. |
| named session | Resume or create a declared session such as `lead-agent`. |

Named sessions are declared at the workflow top level:

```yaml
sessions:
  - name: lead-agent
    agent: planner
  - name: reviewer-agent
    agent: planner

steps:
  - id: draft
    session: lead-agent
    prompt: "Draft the proposal."

  - id: review
    session: reviewer-agent
    mode: autonomous
    prompt: "Review the proposal."
```

Named session names cannot be `new`, `resume`, or `inherit`.

## Shell Steps

```yaml
- id: validate
  command: agent-validator run --report
  capture: validator_output
  capture_stderr: true
  continue_on_failure: true
```

Shell commands are interpolated with shell-safe quoting and run through `/bin/sh`. Non-zero exit codes fail the step unless `continue_on_failure: true` is set.

Shell steps may set `mode: interactive` to run in a PTY. Interactive shell steps cannot use `capture`.

## Script Steps

Script steps run static workflow-local or bundled scripts.

```yaml
- id: detect
  script: detect-options.sh
  script_inputs:
    cwd: "{{session_dir}}"
  capture: options
  capture_format: json
```

`script` must be a static relative path and cannot use interpolation or path traversal. `script_inputs` are passed to the script as JSON on stdin. `capture_format` may be `text` or `json`; JSON captures must be either an array of strings or an object whose values are strings.

## UI Steps

UI steps render inside the live run TUI.

```yaml
- id: choose-cli
  mode: ui
  title: "Choose CLI"
  body: "Select the CLI for this run."
  inputs:
    - kind: single_select
      id: cli
      prompt: "CLI"
      options: ["claude", "codex"]
      default: "claude"
  actions:
    - label: "Continue"
      outcome: continue
  capture: setup_inputs
  outcome_capture: setup_action
```

`capture` stores UI inputs as a map. `outcome_capture` stores the selected action outcome as a string. UI steps require a TTY.

## Loops

Counted loop:

```yaml
- id: retry
  loop:
    max: 3
    as_index: attempt
  steps:
    - id: validate
      command: agent-validator run
      continue_on_failure: true
      break_if: success

    - id: fix
      session: inherit
      mode: autonomous
      prompt: "Fix validator failures."
      skip_if: previous_success
```

For-each loop:

```yaml
- id: per-task
  loop:
    over: "tasks/*.md"
    as: task_file
    as_index: i
    require_matches: true
  steps:
    - id: implement
      agent: implementor
      session: new
      mode: autonomous
      prompt: "Implement {{task_file}}."
```

`break_if: success` or `break_if: failure` exits the enclosing loop. A loop with a break condition fails if all iterations are exhausted without a break. A loop without any break condition succeeds after all iterations complete.

## Sub-Workflows

```yaml
- id: implement-task
  workflow: ../core/implement-task.yaml
  params:
    task_file: "{{task_file}}"
```

Sub-workflow paths resolve relative to the parent workflow. Built-in workflows can call scripts and child workflows bundled in the same namespace. Sub-workflows get their own execution context, receive only explicitly passed parameters plus defaults, and may use `session: inherit` to continue the parent session.

## Flow Control

`continue_on_failure: true` lets the workflow continue after a failed step.

`skip_if` supports:

```yaml
skip_if: previous_success
skip_if: 'sh: test "{{run_session_report}}" != "true"'
```

`previous_success` is not allowed on the first step in a scope. The `sh:` form is allowed on the first step and skips when the shell command exits 0.

`break_if` supports:

```yaml
break_if: success
break_if: failure
```

It is only valid inside a loop body.

## Capture And Interpolation

Captured values are available to later steps with `{{name}}`.

```yaml
- id: collect
  command: git status --short
  capture: status

- id: summarize
  agent: summarizer
  mode: autonomous
  prompt: |
    Summarize this status:
    {{status}}
```

Shell and autonomous agent captures are strings. Script JSON captures can produce lists or maps. UI captures produce maps. Whole-value interpolation preserves typed values where supported, such as using a captured list as UI select options.

## Built-In Workflows

Common built-ins:

| Workflow | Purpose |
| --- | --- |
| `openspec:plan-change` | Create and plan an OpenSpec change. |
| `openspec:implement-change` | Implement task files for an OpenSpec change. |
| `openspec:change` | Run OpenSpec planning and implementation. |
| `openspec:simple-change` | Inline OpenSpec planning and implementation for smaller changes. |
| `spec-driven:plan-change` | Plan without depending on OpenSpec. |
| `spec-driven:implement-change` | Implement discovered task files without OpenSpec. |
| `spec-driven:change` | Run spec-driven planning and implementation. |
| `spec-driven:simple-change` | Inline spec-driven planning and implementation. |
| `core:run-validator` | Run Agent Validator with a retry/fix loop. |
| `core:implement-task` | Implement one task file and run validation. |
| `core:finalize-pr` | Push/update PR, wait for CI, and fix failures. |

Use the TUI or `agent-runner -list` to browse the full embedded set.

## Engines

Engines are Go plugins registered in the binary. The engine interface supports workflow validation, deferred validation, prompt enrichment, and post-step validation.

```go
type Engine interface {
    ValidateWorkflow(workflow *model.Workflow, params map[string]string, workflowFile string) error
    NeedsDeferredValidation() bool
    EnrichPrompt(stepID string, params map[string]string, opts engine.EnrichOptions) string
    ValidateStep(stepID string, params map[string]string) (bool, error)
}
```

The built-in `openspec` engine is configured like this:

```yaml
engine:
  type: openspec
  change_param: change_name
```

It uses `openspec status --change <name> --json` and `openspec instructions <step> --change <name> --json` to validate artifact steps and enrich prompts.

## Run State And Audit Logs

Runs are stored under:

```text
~/.agent-runner/projects/<encoded-cwd>/runs/<run-id>/
```

Important files:

| File | Purpose |
| --- | --- |
| `state.json` | Resume state, current step, session IDs, params, captures, nested progress, and completion flag. |
| `audit.log` | JSONL event log for the run. |
| `output/` | Per-step output files used by the live run view and workflows. |
| `bundled/` | Materialized bundled scripts/assets for built-in workflow runs. |

Audit events include `run_start`, `run_end`, `step_start`, `step_end`, `iteration_start`, `iteration_end`, `sub_workflow_start`, `sub_workflow_end`, and `error`.

## Troubleshooting

### Missing required parameter

Pass the parameter positionally or as `key=value`.

### Unknown workflow

Use a project/user workflow name, a path-like name under `.agent-runner/workflows`, or a built-in name such as `core:run-validator`.

### Unknown CLI adapter

Valid step-level `cli` values are `claude`, `codex`, `copilot`, `cursor`, and `opencode`.

### Agent step requires "agent"

Fresh sessions need an agent profile. Add `agent: planner`, `agent: implementor`, or another configured profile, or use `session: resume`, `session: inherit`, or a declared named session when that is what you intend.

### Interactive step will not advance

The workflow advances when any continuation trigger is detected:

- The user types `/next` and presses Enter.
- The user presses Ctrl-].
- The agent emits the injected continuation marker after the user asks it to continue or the prompt tells it to complete the step.

If the CLI exits without one of these triggers, the step is aborted and the workflow can be resumed.

### Resume a run

Use the TUI:

```bash
agent-runner -resume
```

Or resume a specific run ID from the current project:

```bash
agent-runner -resume <run-id>
```
