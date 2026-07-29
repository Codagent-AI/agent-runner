# headless-progress-indication Specification

## Purpose
Document legacy headless stdout progress indicators that were retired in favor of live run TUI rendering.

## Requirements

### Requirement: Headless prompt display

Agent Runner SHALL NOT print a headless agent step's resolved prompt to stdout. The `live-run-view` TUI SHALL show the prompt inline in the step's detail pane, and the `step_start` audit event SHALL retain it for durable inspection.

#### Scenario: Headless prompt appears outside stdout
- **WHEN** a headless agent step starts in the live-run TUI
- **THEN** its resolved prompt is absent from stdout, visible in the detail pane, and present in the `step_start` audit event

### Requirement: Headless spinner animation

Agent Runner SHALL NOT draw a headless progress spinner on stdout. The `live-run-view` TUI SHALL indicate an in-progress headless agent step through its running status glyph and real-time detail-pane output.

#### Scenario: Headless progress uses TUI indicators
- **WHEN** a headless agent step is running
- **THEN** stdout contains no legacy spinner animation and the TUI shows the running status glyph and streamed output
