# live-run-view Specification (delta)

## MODIFIED Requirements

### Requirement: TUI stays open after workflow completion

When the workflow reaches a terminal state (success or failure), the run-view TUI SHALL remain active. Exit SHALL require explicit user input (`q`, `Ctrl+C`, or Escape at the top level).

On a successful (`completed`) terminal state, the TUI SHALL display the run summary screen as the post-completion view, per the `run-complete-screen` capability; the `s` key toggles to the detailed run view. On a `failed` terminal state, the TUI SHALL show the detailed view with the cursor on the failed step, and the summary is available via `s`.

Once in the post-completion detailed view, the run view SHALL behave identically to a run opened via `--inspect` — the user can navigate the step list, drill in and out, scroll output, trigger the resume action on agent steps, and invoke the legend overlay.

#### Scenario: Successful completion shows summary screen
- **WHEN** the last step in the workflow completes successfully
- **THEN** the TUI remains open displaying the run summary screen, with the breadcrumb status showing `completed`

#### Scenario: Failure keeps TUI open in detailed view
- **WHEN** a step fails and the workflow halts
- **THEN** the TUI remains open in the detailed view with the breadcrumb status showing `failed`; the summary is not auto-shown

#### Scenario: Post-completion navigation matches inspect mode
- **WHEN** the workflow has finished, the user is in the detailed view (via `s` from the summary, or after a failure), and the user navigates the step list, drills into sub-workflows or iterations, or scrolls the detail pane
- **THEN** the behavior is identical to a run opened via `--inspect` (per the `view-run` capability)

#### Scenario: Resume action available after completion
- **WHEN** the workflow has finished and the user triggers the resume action on an agent step with a known session ID from the detailed view
- **THEN** the TUI exits and agent-runner is relaunched with `--resume <session-id>`, exactly as the `view-run` capability's resume behavior specifies

## ADDED Requirements

### Requirement: Metrics update during an active run

While a workflow run is executing, the live detailed view SHALL show each completed step's token usage and reported cost in its detail block as soon as the step completes, per the `view-run` capability's detail-pane contract. Metrics for the run so far SHALL also be available mid-run through the summary toggle (per `run-complete-screen`).

#### Scenario: Completed step shows metrics mid-run
- **WHEN** an agent step completes while later steps are still running
- **THEN** that step's detail block shows its token usage and reported cost without waiting for the run to finish
