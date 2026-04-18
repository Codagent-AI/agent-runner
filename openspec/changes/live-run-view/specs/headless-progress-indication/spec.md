## REMOVED Requirements

### Requirement: Headless prompt display
**Reason**: The `live-run-view` TUI shows the resolved prompt inline in the detail pane of a headless agent step; it is no longer printed to stdout.
**Migration**: None for end users — the prompt is visible in the TUI. Tooling that parsed the indented stdout prompt must read the prompt from `audit.log` `step_start` events.

### Requirement: Headless spinner animation
**Reason**: The `live-run-view` TUI indicates in-progress headless agent steps via the step-list status indicator (blinking running glyph) and real-time output streaming in the detail pane; a stdout spinner is no longer drawn.
**Migration**: None for end users — progress is visible in the TUI.
