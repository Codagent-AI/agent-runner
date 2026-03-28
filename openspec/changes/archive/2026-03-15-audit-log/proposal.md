## Why

When a workflow fails, there's no record of what happened. The state file only tracks *where* execution stopped (for resume), not *what* each step did, what it received, or why it failed. Troubleshooting requires re-running with manual observation. We need a structured audit log that captures the full execution history for post-failure review and general visibility.

## What Changes

- Add a structured audit log that emits paired start/end events for every lifecycle point: run, step, loop iteration, sub-workflow
- Each log entry captures the full context snapshot (params, captured variables), step-specific details (commands, prompts, session info), timing, and outcome
- Capture stderr from shell steps (currently inherited to terminal and lost) by piping and teeing it, storing it in the log
- Correlation via nesting path so entries can be filtered by run, iteration, or sub-workflow scope

## Capabilities

### New Capabilities

- `audit-log-entries`: Defines the structure and content of audit log entries — event types, fields, context snapshots, step-type-specific data, and correlation model
- `audit-log-lifecycle`: Defines when and how log entries are emitted during workflow execution — paired start/end events at run, step, loop iteration, and sub-workflow boundaries
- `stderr-capture`: Captures stderr from shell steps by piping and teeing (similar to existing stdout capture), storing it in the audit log
- `audit-log-storage`: Defines where and how audit logs are persisted — file format, naming, location, and persistence policy

### Modified Capabilities

- `output-capture`: Shell executor changes to also pipe stderr (currently only stdout is piped when capture is set)

## Impact

- **`src/runner.ts`**: Emit run-level and step-level audit events, pass audit logger through execution
- **`src/executors/shell.ts`**: Pipe stderr (in addition to existing stdout piping), emit step-specific audit data
- **`src/executors/agent.ts`**: Emit agent-specific audit data (prompt, session strategy, resolved/discovered session IDs, model, enrichment)
- **`src/executors/loop.ts`**: Emit iteration-level audit events
- **`src/executors/sub-workflow.ts`**: Emit sub-workflow audit events with resolved params
- **`src/context.ts`**: May need to carry audit logger reference
- New `src/audit.ts` (or similar): Audit log writer, entry formatting, path encoding
