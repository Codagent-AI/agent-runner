## ADDED Requirements

### Requirement: Dispatcher resumes incomplete prior onboarding runs

When the dispatcher condition fires and a prior `onboarding:welcome` run exists in a non-terminal state (i.e., the workflow was started but did not reach success, dismiss, or fatal failure), the dispatcher SHALL resume that run via the standard resume machinery rather than starting a new run. Only when there is no incomplete prior run SHALL the dispatcher start a fresh `onboarding:welcome`.

The dispatcher SHALL detect incomplete runs by inspecting the standard run-state location for runs whose workflow id is `onboarding:welcome` and whose state is not terminal. If multiple incomplete runs exist (an unexpected state), the dispatcher SHALL resume the most recent and SHALL NOT start a new run.

#### Scenario: Resume after Ctrl-C mid-flow
- **WHEN** the user starts onboarding via the dispatcher, advances past the welcome screen and the adapter-detection step, presses Ctrl-C to exit, and then re-runs `agent-runner` on a TTY with onboarding settings still unset
- **THEN** the dispatcher SHALL fire and SHALL resume the existing run at the next pending step rather than re-rendering the welcome screen

#### Scenario: No incomplete run starts a fresh one
- **WHEN** the dispatcher fires and there is no `onboarding:welcome` run in a non-terminal state
- **THEN** the dispatcher SHALL start a fresh `onboarding:welcome` run

#### Scenario: Multiple incomplete runs resume the most recent
- **WHEN** the dispatcher fires and two `onboarding:welcome` runs exist in non-terminal states
- **THEN** the dispatcher SHALL resume the most recent run and SHALL NOT start a new run

### Requirement: Post-onboarding handoff to the home screen

When a dispatcher-launched `onboarding:welcome` run reaches a terminal state — `continue` → setup success, `dismiss`, `not_now`, cancellation, or failure — the runner SHALL transition to its normal entry point for the bare `agent-runner` invocation: the list-runs ("home") TUI. The runner SHALL NOT leave the user on the post-completion run view of the onboarding run.

This handoff applies only to the dispatcher-launched path. Direct invocation via `agent-runner run onboarding:welcome` SHALL retain its current post-run behavior (the user remains on the run-view per the standard `view-run` rules) so that scripted or explicit invocations are not surprised by an entry-point switch.

#### Scenario: Dismiss returns to home
- **WHEN** the dispatcher launches onboarding and the user selects `dismiss` on the welcome screen
- **THEN** after the dismissal timestamp is written, the runner SHALL transition to the list-runs TUI

#### Scenario: Not-now returns to home
- **WHEN** the dispatcher launches onboarding and the user selects `not_now` on the welcome screen
- **THEN** the runner SHALL transition to the list-runs TUI without writing settings

#### Scenario: Successful setup returns to home
- **WHEN** the dispatcher launches onboarding, the user selects `continue`, and setup completes successfully
- **THEN** after `completed_at` is written, the runner SHALL transition to the list-runs TUI

#### Scenario: Setup cancellation returns to home
- **WHEN** the dispatcher launches onboarding, the user selects `continue`, and the user cancels at the editor's confirmation screen
- **THEN** the runner SHALL transition to the list-runs TUI without writing settings (dispatcher will fire again on next entry per existing rules)

#### Scenario: Direct invocation does not switch entry points
- **WHEN** the user runs `agent-runner run onboarding:welcome` directly and the workflow reaches a terminal state
- **THEN** the runner SHALL behave per the existing `view-run` rules (remain on the run view) and SHALL NOT auto-transition to the list-runs TUI
