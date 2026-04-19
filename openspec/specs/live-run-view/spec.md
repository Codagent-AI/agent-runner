## ADDED Requirements

### Requirement: Workflow invocation launches the run-view TUI

When agent-runner is invoked to run a workflow — either a fresh invocation or `--resume <session-id>` — the run-view TUI SHALL launch as the foreground display immediately and remain the sole interface for the run's duration. No streaming console output SHALL precede TUI initialization.

#### Scenario: Fresh workflow launches TUI
- **WHEN** agent-runner is invoked with a workflow name or path
- **THEN** the run-view TUI takes over the terminal with the step list populated from the workflow file, all rows in `pending`, before any step dispatches

#### Scenario: --resume launches TUI
- **WHEN** agent-runner is invoked with `--resume <session-id>` for an inactive run
- **THEN** the run-view TUI takes over with the step tree hydrated from audit.log and execution resumes from the recorded latest-step pointer

### Requirement: TTY required for TUI-launching invocations

Any agent-runner invocation that launches the TUI — workflow execution, `--resume`, `--list`, `--inspect` — SHALL require stdout to be an interactive terminal. If stdout is not a TTY, agent-runner SHALL print an error message to stderr identifying the TTY requirement and exit with a non-zero status without launching the TUI or executing the workflow. Non-TUI invocations (`--validate`, `--version`, `-v`) SHALL remain usable without a TTY.

#### Scenario: Piped stdout rejected for workflow run
- **WHEN** agent-runner is invoked to run a workflow with its stdout piped to another process
- **THEN** it prints a clear error to stderr and exits non-zero; no workflow steps dispatch

#### Scenario: Redirected stdout rejected for workflow run
- **WHEN** agent-runner is invoked to run a workflow with stdout redirected to a file
- **THEN** it prints a clear error to stderr and exits non-zero

#### Scenario: Interactive terminal proceeds
- **WHEN** agent-runner is invoked from an interactive terminal
- **THEN** the TUI launches and the workflow executes normally

#### Scenario: --validate does not require TTY
- **WHEN** `agent-runner --validate <workflow>` is invoked without a TTY (piped, redirected, or in CI)
- **THEN** validation runs and agent-runner exits with the validation outcome; the TTY check does not fire

#### Scenario: --version does not require TTY
- **WHEN** `agent-runner --version` (or `-v`) is invoked without a TTY
- **THEN** the version string is printed and agent-runner exits zero; the TTY check does not fire

### Requirement: TUI stays open after workflow completion

When the workflow reaches a terminal state (success or failure), the run-view TUI SHALL remain active. Exit SHALL require explicit user input (`q`, `Ctrl+C`, or Escape at the top level). Once in this post-completion state, the run view SHALL behave identically to a run opened via `--inspect` — the user can navigate the step list, drill in and out, scroll output, trigger the resume action on agent steps, and invoke the legend overlay.

#### Scenario: Successful completion keeps TUI open
- **WHEN** the last step in the workflow completes successfully
- **THEN** the TUI remains open with the breadcrumb status showing `completed`

#### Scenario: Failure keeps TUI open
- **WHEN** a step fails and the workflow halts
- **THEN** the TUI remains open with the breadcrumb status showing `failed`

#### Scenario: Post-completion navigation matches inspect mode
- **WHEN** the workflow has finished and the user navigates the step list, drills into sub-workflows or iterations, or scrolls the detail pane
- **THEN** the behavior is identical to a run opened via `--inspect` (per the `view-run` capability)

#### Scenario: Resume action available after completion
- **WHEN** the workflow has finished and the user triggers the resume action on an agent step with a known session ID
- **THEN** the TUI exits and agent-runner is relaunched with `--resume <session-id>`, exactly as the `view-run` capability's resume behavior specifies

### Requirement: Real-time step output

Shell and headless agent step stdout and stderr SHALL render in the detail pane as output is produced, not only after the step completes. Output is delivered to the TUI via an in-process channel (bubbletea `p.Send`) as bytes are produced by the subprocess, with ANSI escape sequences stripped before rendering. The same bytes (raw, unstripped) SHALL also be written to `<sessionDir>/output/<step-prefix>.out` and `<step-prefix>.err` for durable post-run inspection regardless of audit-log truncation, where `<step-prefix>` is the audit log event's `prefix` field with `/` replaced by `__` and `:` replaced by `_`. First-byte latency target: visible in the detail pane within 100 ms of being produced. Interactive agent steps are excluded — the agent owns the terminal directly during such steps, so agent-runner neither streams nor persists the agent's output itself.

#### Scenario: Long-running shell step output streams
- **WHEN** a shell step is executing and producing stdout
- **THEN** its detail pane reflects newly produced bytes without waiting for the step to finish

#### Scenario: Headless agent output streams
- **WHEN** a headless agent step is executing and its CLI is producing output
- **THEN** its detail pane reflects newly produced bytes without waiting for the step to finish

#### Scenario: ANSI sequences are stripped in the detail pane
- **WHEN** a shell step emits ANSI color or cursor-positioning sequences (e.g., `ls --color`, `git diff`)
- **THEN** the detail pane renders the text without those sequences and without visual corruption of the surrounding TUI layout

#### Scenario: Output persists past step completion
- **WHEN** a shell or headless agent step has completed
- **THEN** its full stdout and stderr (including any ANSI sequences, untruncated) are readable from `<sessionDir>/output/<step-prefix>.out` and `.err` in the session directory

#### Scenario: Post-completion detail pane reads from output files
- **WHEN** the workflow has finished and the user selects a shell or headless step (either in the same live-run TUI session or after re-opening via `--inspect` / list Enter)
- **THEN** the detail pane loads that step's output from the persisted output files, showing full content (subject only to the 2000-line / 256 KB tail-render threshold)

#### Scenario: Interactive agent step has no output files
- **WHEN** an interactive agent step runs and exits
- **THEN** no `<sessionDir>/output/<step-prefix>.out` or `.err` files are created for it; the detail pane shows the agent's profile, CLI, model, and session metadata as specified by the `view-run` detail-pane requirement

### Requirement: Interactive agent steps suspend the TUI

When the workflow dispatches an interactive agent step, the run-view TUI SHALL suspend, releasing the terminal so the agent process has full control. When the agent process exits, the TUI SHALL re-enter automatically without user input, regardless of the agent's exit status.

#### Scenario: Interactive step takes over terminal
- **WHEN** an interactive agent step starts
- **THEN** the run-view TUI suspends and the agent process owns the terminal

#### Scenario: Agent exits successfully and returns to TUI
- **WHEN** the interactive agent process exits with a successful outcome (continue-trigger received)
- **THEN** the run-view TUI re-enters automatically, the step's row reflects `success`, and workflow execution continues

#### Scenario: Agent exits abnormally and returns to TUI
- **WHEN** the interactive agent process exits without a continue-trigger (the session was abandoned or the CLI returned non-zero)
- **THEN** the run-view TUI re-enters automatically and the step's row reflects the recorded outcome (`aborted` or `failed`, per the existing interactive-agent behavior defined in the agent-runner engine)

### Requirement: Cursor auto-follows the active step

While the workflow is running and the user has not manually navigated away, the step-list cursor and drill depth SHALL auto-follow the currently active step — moving to peer steps at the same level, drilling into newly entered sub-workflows and loop iterations, and drilling out when execution leaves them.

Manual cursor movement, drill-in, or drill-out SHALL pause auto-follow. A dedicated keyboard action SHALL jump the cursor to the active step and re-engage auto-follow.

#### Scenario: Active step advances to peer
- **WHEN** the cursor is auto-following and execution moves to the next peer step
- **THEN** the cursor moves to the new active step and its detail pane is shown

#### Scenario: Active step enters a sub-workflow
- **WHEN** the cursor is auto-following and execution enters a sub-workflow
- **THEN** the view drills into the sub-workflow and the cursor lands on the new active child step

#### Scenario: Active step enters a loop iteration
- **WHEN** the cursor is auto-following and execution enters a new loop iteration
- **THEN** the view drills into the iteration and the cursor lands on the new active child step

#### Scenario: Active step leaves a sub-workflow
- **WHEN** the cursor is auto-following and execution returns from a sub-workflow or iteration to a higher level
- **THEN** the view drills out to the level of the new active step

#### Scenario: Manual navigation pauses auto-follow
- **WHEN** the user moves the cursor, drills in, or drills out manually
- **THEN** auto-follow is paused and the cursor stays where the user placed it regardless of execution progress

#### Scenario: Jump-to-live re-engages auto-follow
- **WHEN** the user presses the jump-to-live key (`l`) with auto-follow paused
- **THEN** the view navigates to the currently active step (drilling in/out as needed) and auto-follow resumes

#### Scenario: Failure jumps cursor to the failed step
- **WHEN** the workflow reaches a terminal failed state
- **THEN** the cursor drills to the failed step (including into sub-workflows or loop iterations as needed) and its detail pane is shown, regardless of where auto-follow last placed the cursor

### Requirement: Detail-pane tail-follow

While output is streaming into the selected step's detail pane, the viewport SHALL remain pinned at the tail (newest content visible) unless the user has manually scrolled up. Scrolling up (via `k` or mouse wheel) SHALL pause tail-follow. Pressing `t` SHALL jump the viewport to the tail and re-engage tail-follow. `End` and uppercase `G` SHALL NOT be bound to this action (or to anything else).

#### Scenario: Streaming output auto-tails
- **WHEN** new bytes arrive for the currently selected step and tail-follow is engaged
- **THEN** the detail pane viewport stays at the bottom, showing the newest content

#### Scenario: User scroll pauses tail-follow
- **WHEN** the user scrolls the detail pane up (via `k` or mouse-wheel-up)
- **THEN** tail-follow is paused; subsequent output does not move the viewport

#### Scenario: t re-engages tail-follow
- **WHEN** the user presses `t` with tail-follow paused
- **THEN** the viewport jumps to the bottom of the output and tail-follow resumes

#### Scenario: End and G are not bound
- **WHEN** the user presses `End` or uppercase `G`
- **THEN** nothing happens (neither key is bound to tail-follow or any other action)

### Requirement: Quit during live run requires confirmation

While the workflow is running, pressing `q`, `Ctrl+C`, or Escape at the top level SHALL prompt the user for confirmation before quitting. The confirmation prompt SHALL explicitly state that the active subprocess will be orphaned (continue running in the background) if the user proceeds. Confirming SHALL exit the TUI without killing the active subprocess. Declining SHALL dismiss the prompt and leave the workflow running. After the workflow has finished, `q`, `Ctrl+C`, and Escape-at-top-level SHALL exit immediately without confirmation. The confirmation does not fire while the TUI is suspended for an interactive agent step, because during that window keystrokes are received by the agent, not the TUI.

#### Scenario: Confirmation requested on q mid-run
- **WHEN** the user presses `q` while the workflow is running and the TUI is not suspended for an interactive step
- **THEN** a confirmation prompt is displayed stating that the active subprocess will be orphaned on confirm; the workflow continues while the prompt is open

#### Scenario: Confirmation requested on Ctrl+C mid-run
- **WHEN** the user presses `Ctrl+C` while the workflow is running and the TUI is not suspended for an interactive step
- **THEN** the same confirmation prompt as for `q` is displayed; the workflow continues while the prompt is open

#### Scenario: Confirmation requested on Escape at top level mid-run
- **WHEN** the user presses Escape at the top level of the run view while the workflow is running
- **THEN** the same confirmation prompt as for `q` is displayed; the workflow continues while the prompt is open

#### Scenario: Escape inside a drilled-in view drills out, not quit
- **WHEN** the user presses Escape while drilled into a sub-workflow, loop, or iteration (not at the top level) and the workflow is running
- **THEN** the view drills out one level as specified by the `view-run` capability; no confirmation is displayed

#### Scenario: Confirmation accepted exits TUI and orphans subprocess
- **WHEN** the user confirms quit
- **THEN** the TUI exits; any currently-running subprocess (shell command or headless agent) is not killed and continues executing in the background

#### Scenario: Confirmation declined keeps running
- **WHEN** the user declines quit
- **THEN** the prompt is dismissed and the workflow continues uninterrupted

#### Scenario: Keystrokes during interactive step reach the agent
- **WHEN** the user presses `q` or `Ctrl+C` while the TUI is suspended for an interactive agent step
- **THEN** the keystroke is delivered to the agent process (not the TUI); no confirmation is displayed by agent-runner

#### Scenario: No confirmation after completion
- **WHEN** the workflow has finished and the user presses `q`, `Ctrl+C`, or Escape at the top level
- **THEN** the TUI exits immediately
