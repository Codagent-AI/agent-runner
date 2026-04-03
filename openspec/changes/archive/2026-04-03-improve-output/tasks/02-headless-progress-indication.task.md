# Task: Headless progress indication

## Goal

Show the resolved prompt and a spinner animation during headless agent step execution, so operators can see what the agent was asked and know it's still running.

## Background

Headless agent steps currently print `mode: headless` and then go silent until completion. The operator has no visibility into what the agent was asked or whether the step is still running (vs. hung). Two changes address this: printing the resolved prompt and showing a spinner.

**Prompt display** — After the existing `mode: headless` line is printed (in `logStepMode()`), print the full resolved prompt text indented with two spaces per line. This happens only for headless steps — interactive steps already show the prompt in the interactive session. The resolved prompt is available in the `executeAgentStep()` function after `buildPrompt()` returns.

**Spinner** — Use the `ora` library (add as a new dependency) to show an ASCII spinner with the text `agent running...` while the headless subprocess is active. The spinner starts before `Bun.spawn()` returns and stops after the process exits. If the user presses ctrl-c, the spinner must be stopped before the subprocess is killed. The spinner lifecycle lives entirely within `runHeadlessWithSigint()` in `src/executors/agent.ts`.

**Key files:**
- `src/executors/agent.ts` — all changes go here:
  - `executeAgentStep()` — add prompt printing after `logStepMode()` for headless steps. The resolved `prompt` variable is already available at that point (line ~77-78).
  - `runHeadlessWithSigint()` — add `ora` spinner start/stop around the subprocess lifecycle. Currently this function creates a SIGINT handler that calls `proc.kill()` — update it to also call `spinner.stop()` before killing.
- `package.json` — add `ora` as a dependency

**Constraints:**
- `ora` should work under Bun (the project runtime). If it doesn't, `nanospinner` or a hand-rolled `setInterval` + `process.stdout.write` fallback is ~20 lines.
- Only headless steps get the prompt display and spinner — interactive steps are unchanged.
- The prompt display uses simple 2-space indentation: `prompt.split('\n').map(l => '  ' + l).join('\n')`.
- The spinner text is `agent running...` — no dynamic content.
- The spinner line must be cleared (not left behind) when the step completes or is interrupted.

## Spec

### Requirement: Headless prompt display

When a headless agent step starts, baton SHALL print the full resolved prompt text to stdout, indented with two spaces per line. The prompt is printed after the step heading and mode label, before the agent subprocess starts.

#### Scenario: Prompt displayed for headless step
- **WHEN** a headless agent step starts with a resolved prompt
- **THEN** the prompt text is printed to stdout, indented with two spaces per line

#### Scenario: Prompt not displayed for interactive step
- **WHEN** an interactive agent step starts
- **THEN** the prompt text is NOT printed (the user sees it in the interactive session)

#### Scenario: Long prompt displayed in full
- **WHEN** a headless agent step has a multi-line resolved prompt
- **THEN** the full prompt is printed without truncation

### Requirement: Headless spinner animation

While a headless agent step is running, baton SHALL display an ASCII spinner animation with the label `agent running...` to indicate the step is in progress. The spinner is cleared when the step completes or is interrupted.

#### Scenario: Spinner shown during headless execution
- **WHEN** a headless agent step is running
- **THEN** an ASCII spinner with label `agent running...` is displayed on the current terminal line

#### Scenario: Spinner cleared on step completion
- **WHEN** a headless agent step finishes (success or failure)
- **THEN** the spinner is stopped and the line is cleared before subsequent output

#### Scenario: Spinner cleared on ctrl-c
- **WHEN** the user presses ctrl-c during a headless agent step with an active spinner
- **THEN** the spinner is stopped and cleared before baton exits

#### Scenario: Spinner not shown for interactive steps
- **WHEN** an interactive agent step is running
- **THEN** no spinner is displayed

## Done When

All seven scenarios above are covered by tests and passing. Headless agent steps display the resolved prompt (indented) and show an `ora` spinner during execution. Interactive steps are unchanged. The spinner is cleanly stopped on both normal completion and ctrl-c interruption.
