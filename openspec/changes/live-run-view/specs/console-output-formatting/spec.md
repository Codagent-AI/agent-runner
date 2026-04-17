## REMOVED Requirements

### Requirement: Step separator lines
**Reason**: The `live-run-view` TUI is now the sole live workflow display; separator lines on stdout are no longer printed. The TUI renders step boundaries through its step-list layout.
**Migration**: None for end users — the behavior is replaced by the TUI. Tooling that parsed these separators from stdout must switch to tailing `audit.log` events.

### Requirement: Breadcrumb step headings
**Reason**: The `live-run-view` TUI renders the full nesting path as a breadcrumb line and shows step identity in the step list; breadcrumb headings on stdout are no longer printed.
**Migration**: None for end users. Tooling that parsed these headings from stdout must switch to tailing `audit.log` events (which carry nesting via the `prefix` field).
