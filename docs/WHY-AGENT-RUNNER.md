# Why Agent Runner Exists

There are many YAML-based workflow definition languages and orchestration engines. This document explains why agent-runner is a separate tool rather than a configuration layer on top of an existing one.

## The Short Answer

Existing workflow engines fall into two categories: **cloud/server platforms** that can't run local CLI commands, and **CLI task runners** that can't express the control flow agent-runner needs. No existing tool combines local CLI execution, agent session management, loop-until with multi-step bodies, and mid-pipeline output capture.

## The Landscape

### Cloud and Server-Based Orchestrators

These systems have rich workflow definition languages with loops, conditionals, sub-workflows, and output capture. They are also inaccessible for agent-runner's use case.

| System | Why it doesn't work |
|---|---|
| **AWS Step Functions** | Runs Lambda functions and AWS services, not local CLI commands. Requires AWS infrastructure. |
| **Argo Workflows** | Runs containers on Kubernetes. Steps are Docker images, not host shell commands. |
| **Kestra** | Requires a JVM server process with an H2 database. Even "local mode" starts a web UI. Shell tasks run on the server, not the developer's machine. |
| **Tekton** | Kubernetes-native. Steps are containers. No local execution model. |
| **Azure Logic Apps** | Azure cloud service. No local execution. |
| **Netflix Conductor** | Server with worker polling architecture. Steps are remote task workers. |
| **Temporal** | Server + SDK architecture. Workflows are code (Go/Python), not YAML. Workers poll a server. |
| **Prefect / Airflow** | Python server platforms. Workflows are Python code, not YAML. Designed for data pipelines, not CLI orchestration. |

These tools have excellent YAML/JSON schemas for defining workflows. But their runtimes execute work in containers, cloud functions, or server-managed workers. Agent Runner needs to spawn `claude --resume <session-id> -p "fix these failures"` as a local process with access to the developer's filesystem, git repos, and terminal. That's fundamentally incompatible with container or cloud execution models.

### CLI Task Runners

These tools run locally and execute shell commands. They lack the workflow primitives agent-runner requires.

#### Taskfile (go-task)

The closest existing tool. Taskfile is a YAML-based CLI task runner with `for:` loops over globs and shell-output variables.

**What works:**
- Run arbitrary shell commands, including `claude -p "prompt"`
- Iterate over files with `for: { sources: ["tasks/*.md"] }`
- Capture shell output into variables with `sh:` in `vars:`
- Interactive TTY passthrough with `interactive: true`

**Where it breaks down:**

1. **No loop-until.** Taskfile has `retry:` which retries a single command on failure. It cannot retry a *sequence* of steps (run validator → fix → re-run validator). You must encode multi-step retry logic in a shell script inside a `cmd:` block, at which point the YAML is just a wrapper around bash.

2. **No mid-pipeline output capture.** `vars.sh:` resolves once when the task starts, before any commands run. You cannot run command A, capture its stdout, and inject it into command B's arguments within the same task. Workaround: temp files, which pushes state management into shell land.

3. **No conditional branching.** `if:` on a command skips or runs that one command. There is no if/else that routes to different task paths or step sequences.

4. **No session management.** No concept of tracking session IDs between steps, resuming a previous session, or choosing between interactive and headless execution modes at the workflow level.

The honest version of agent-runner's implement-verify workflow in Taskfile collapses to a single `cmd:` block containing a bash script:

```yaml
tasks:
  implement-and-verify:
    cmds:
      - |
        SESSION_ID=$(claude -p "Implement: $(cat {{.TASK_FILE}})" \
          --output-format json | jq -r '.session_id')
        for attempt in 1 2 3; do
          VALIDATOR_OUT=$(agent-validator run 2>&1) && break
          claude --resume "$SESSION_ID" -p "Fix these: $VALIDATOR_OUT"
        done
```

The Taskfile adds nothing here. The workflow logic is in bash.

#### Just

A Rust-based command runner. Uses its own `justfile` format, not YAML. Recipes are shell commands. No declarative loops, conditionals, or output capture at the workflow level. Essentially a better Makefile. Not applicable.

#### Make / Ninja / Other Build Systems

Build systems execute dependency graphs, not sequential workflows. No loops, no conditionals, no session management. Wrong abstraction entirely.

### Agent Orchestration Frameworks

These frameworks orchestrate AI agents, but at the wrong abstraction level.

| System | Why it doesn't work |
|---|---|
| **LangGraph** | Code-defined (Python), not YAML. Cyclic state graphs are interesting but require a Python runtime and LangChain dependency. Designed for multi-agent conversation routing, not CLI process orchestration. |
| **CrewAI** | YAML configuration for agent roles and tasks, but no workflow-level control flow. Loops and conditionals are handled in Python code. |
| **AutoGen** | Multi-agent conversation framework. Agents converse until done. No declarative workflow steps, no shell command integration, no session management. |

These tools orchestrate *agent conversations*. Agent Runner orchestrates *CLI processes that happen to be agents*. The distinction matters: agent-runner treats `claude` as a CLI binary to spawn and manage, not as a library to call.

## What Makes Agent Runner Different

Five capabilities that no existing tool provides:

### 1. Agent Session Management

Agent Runner tracks session IDs across workflow steps. A step can declare `session: resume` to continue a previous step's conversational context, or `session: new` for a fresh start. This is a first-class workflow concept, not a hack layered on top.

```yaml
- id: implement
  mode: headless
  session: new
  prompt: "Implement this feature"

- id: fix
  mode: headless
  session: resume    # continues implement's session
  prompt: "Fix the test failures"
```

No workflow engine has this concept because no workflow engine was designed to orchestrate stateful conversational agents.

### 2. Mode Triality

Every step declares one of three execution modes:

- **interactive** — Agent runs with full terminal access. Human collaborates. Step advances when the user signals completion.
- **headless** — Agent runs non-interactively. Output streams to terminal. Step advances on exit.
- **shell** — No agent. Direct shell command execution.

These three modes coexist in a single workflow. CI systems have "manual approval gates" but not interactive agent sessions. Task runners have shell commands but not agent modes. No tool combines all three.

### 3. Session-Aware Loops

Inside a loop, `session: resume` chains naturally across iterations. The verify-fix loop creates a session chain where each fix attempt builds on the previous:

```
implement → fix₁ → fix₂ → fix₃
```

The agent accumulates context with each iteration. When the outer loop moves to the next task, `session: new` resets cleanly. No existing loop primitive understands sessions.

### 4. Prompt-Based Steps

Steps send natural language prompts to agents, positioned as the first thing the agent sees (highest-attention position). Prompts can invoke skills directly (`/codagent:propose`), include interpolated parameters, and receive engine-enriched context.

This is fundamentally different from "run a shell command." The prompt is a first-class workflow concept, not a string passed to `sh -c`.

### 5. Signal-Based Interactive Advancement

Interactive steps use a signal file mechanism: the agent writes `.agent-runner-signal` (via the `/continue` skill), agent-runner detects it and advances the workflow. This allows human-in-the-loop collaboration within a deterministic workflow — the human works with the agent for as long as needed, then signals "I'm done with this step."

No CI system or task runner has this interaction model.

## The Design Principle

Agent Runner's primitives for loops, conditionals, output capture, and sub-workflows are **not novel**. They're borrowed from well-established patterns:

- **Loop-until** from Azure Logic Apps, Netflix Conductor
- **For-each** from Kestra, CNCF Serverless Workflow
- **Output capture** from Argo Workflows
- **Sub-workflows** from Kestra, Serverless Workflow
- **Conditional execution** from Argo, GitHub Actions

What's novel is the **runtime layer beneath those primitives**: session management, mode triality, prompt delivery, and signal-based advancement. These capabilities require a purpose-built orchestrator. You can't bolt them onto Taskfile or squeeze them into Argo containers.

Agent Runner extends proven workflow patterns into agent orchestration territory. The syntax should feel familiar to anyone who's written a GitHub Actions workflow or a Kestra flow. The execution model is new.
