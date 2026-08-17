# console-output-formatting Specification

## Purpose
Document legacy workflow stdout formatting that was retired when the live run TUI became the sole live workflow display.

## Requirements

### Requirement: Step separator lines

Agent Runner SHALL NOT print step separator lines to stdout while the `live-run-view` TUI is the live workflow display. The TUI SHALL render step boundaries through its step-list layout.

#### Scenario: Live workflow omits stdout separators
- **WHEN** a workflow runs in the live-run TUI
- **THEN** stdout contains no legacy step separator lines and the TUI step list communicates step boundaries

### Requirement: Breadcrumb step headings

Agent Runner SHALL NOT print breadcrumb step headings to stdout while the `live-run-view` TUI renders the full nesting path as a breadcrumb line and shows step identity in the step list. Audit events SHALL continue to carry nesting through the `prefix` field.

#### Scenario: Live workflow omits stdout breadcrumb headings
- **WHEN** a nested workflow step runs in the live-run TUI
- **THEN** stdout contains no legacy breadcrumb heading, while the TUI breadcrumb shows the nesting path and the audit event prefix retains that nesting
