---
name: run-audit
description: Audit a live or historical Agent Runner execution for orchestration defects using its process, audit, state, output, metrics, workflow, validator, and Git evidence. Use when asked to inspect a run for known or unexpected bugs, monitor a suspicious run as it progresses, or repair Runner-owned failures in an explicitly designated development checkout; do not use for ordinary code review.
---

# Run Audit

Investigate the execution as a system, not just the reported symptom. The
result should distinguish confirmed product defects, likely defects, expected
workflow failures, external-tool failures, and unexplained evidence gaps.

## Scope and safety

- Default to a read-only audit. Do not kill, resume, unlock, rebuild, edit run
  state, or change either the execution worktree or its artifacts merely to
  test a theory.
- A live run owns its worktree, run directory, lock, child processes, and
  validator state. Treat all of them as concurrently changing evidence.
- Preserve unrelated and in-progress changes. Read the applicable `AGENTS.md`
  files before investigating source or making an authorized repair.
- Repair only when the user explicitly requests audit-and-repair. Put a
  Runner-owned fix in the exact development checkout the user designated, not
  in the active execution worktree. Do not fix Agent Validator or another
  sibling repository unless that repository is separately in scope.
- A code fix does not update an already-running binary. Do not rebuild,
  restart, or resume the run unless the user separately authorizes that action.

## Establish the actual execution

Do not infer runtime provenance from the current shell or source tree.

1. Identify the run ID, orchestration working directory, run directory, lock
   PID, runner PID, child process tree, process group, TTY, and current state.
   When a runner is live, `lsof -p <pid>` is often the most reliable way to
   locate its `cwd`, open `audit.log`, and executable (`txt`).
2. Record the exact executable loaded by the process. `command -v
   agent-runner` describes a future launch and is not proof of what the current
   process is running.
3. Record the execution worktree's branch, HEAD, and tracked/untracked status
   without modifying it. Record the designated development checkout
   independently.
4. Read `state.json`, the lock, the latest `run_start`, and the tail of
   `audit.log`. Confirm that the state path, current nested frame, process CWD,
   and audit path all describe the same run.
5. Capture the audit line count and artifact mtimes at the start and end of the
   investigation. State which observations may have changed while the audit
   was in progress.

Built-in workflows are embedded in the loaded Agent Runner binary. A workflow
file changed in the execution branch may have no effect on the live process.
Treat the `step_start` command/prompt in the audit as the authoritative record
of what executed, and use workflow hashes plus binary provenance when comparing
that behavior with source.

## Reconstruct the lifecycle

Parse every audit line as timestamp, optional prefix, event type, and JSON.
Report malformed records and unknown event types before interpreting the run.

- Segment the log at every `run_start`. Analyze start/end balance within a
  resume segment before aggregating the whole file. A killed invocation can
  leave open frames that must not be mistaken for the current invocation.
- Use append order as the primary sequence. Timestamps are supporting evidence:
  producers may use different precision, so a later line can have an earlier
  timestamp within the same second.
- Pair `step_start`/`step_end`, `sub_workflow_start`/`sub_workflow_end`, and
  `iteration_start`/`iteration_end`. In the current live segment, an unmatched
  active chain is expected; unmatched frames in closed or superseded segments
  need explanation.
- Check outcome propagation through iteration, loop, sub-workflow, and run
  boundaries. Flag impossible transitions, duplicate terminal events, skipped
  required work, and suspicious 0 ms failures.
- At every resume boundary, compare the prior terminal or abandoned frame with
  the restored state. Verify the loop index, retry position, captured values,
  session bindings, completed siblings, selected repository, and deepest child
  step.
- Compare the latest active audit chain with `state.json` and the live process
  tree. None is sufficient alone.

Ordinary check or review failures inside a configured retry loop are expected
workflow activity. A run-level failure, an immediately re-exhausted retry,
state/process disagreement, repeated failure after a claimed repair, or a
failure caused by an omitted orchestration input is unexpected.

## Cross-check evidence layers

Follow contradictions between layers; they often reveal defects that no single
artifact exposes.

### Workflow and prompts

- Compare analogous iterations and steps. Check resolved params, glob matches,
  loop counts, working directories, repository selection, commands, prompts,
  model/profile choices, and session strategies.
- Look for stale or repeated intake handoffs, the wrong task file, leaked
  captured variables, unexpected new sessions, missing resume IDs, and prompt
  content that grows across tasks.
- When a workflow hash changes, identify which segment uses each hash and which
  historical events the run view will project or ignore.

### Processes, signals, and locks

- Relate runner, agent, validator, and helper PIDs to their PPID and process
  group. Check for an orphan child, a live runner with no viable child, a stale
  lock, multiple runners writing the same run, or a child repeatedly dying
  immediately after launch.
- Distinguish a graceful stop (`run_end`, terminal step outcome, flushed state)
  from disappearance with no closing events. Inspect shell/TTY state when
  behavior changes after an interrupt.

### Git and repository routing

- Verify each executable step ran in its intended repository. For multi-repo
  runs, keep the orchestration directory, selected repository root, artifact
  paths, and state location distinct.
- Relate recorded start SHAs to generated and fixer commits. Confirm descendant
  relationships, the intended repository, clean-boundary checks, and whether a
  source-only workflow change was actually present in the loaded binary.
- Do not call a commit/revert sequence causal merely because it appears in
  history; compare the actual trees and event times.

### Validator

- Read the exact audited validator command, its CWD, context file, base ref,
  exit status, report, debug log when needed, and per-gate logs.
- Separate findings returned by Validator from failures of Validator itself
  (timeouts, adapter errors, oversized input, stale one-shot state, or missing
  telemetry).
- Compare review scope with the task's recorded start SHA. If source contains a
  scoping fix but the audited command omits it, investigate binary/workflow
  provenance before blaming Validator.
- Do not treat `no_changes`, preserved one-shot state, or a retry counter as
  self-explanatory. Check which baseline and prior state produced it.

### Persisted output

- Inventory `.out` and `.err` files, including size, birth/modify times, and
  non-empty stderr. Compare them with audit stdout/stderr and step attempts.
- Check whether repeated or resumed executions map to the same output basename.
  Output opened with truncation can erase the only transcript of an interrupted
  attempt; concurrent old and new writers can corrupt the same artifact.
- A successful step can still contain meaningful errors or warnings. Inspect
  them rather than filtering solely on outcome.

### Metrics and run view

- Inspect `run-metrics.json`, backup artifacts, schema, run/workflow identity,
  `history_complete`, sessions, attempts, totals, coverage, and nested metrics.
  Explain missing history or `nested-metrics-missing`; do not silently add
  backups together.
- Compare metrics attempts and active duration with audit evidence. Counter
  resets, duplicate cumulative usage, recovery backups, or attempt numbering
  that restarts after recovery deserve explicit classification.
- Compare the TUI projection with audit and state: active step, failure reason,
  loop current/total, elapsed time, retry position, selectable attempts, and
  resume availability. A correct run can still have a broken projection.

## Search beyond the known symptom

Do not stop after confirming the bug the user reported.

- Compare repeated instances of the same workflow step. Unexpected differences
  are often more informative than isolated error text.
- Search for success events containing errors, very short terminal failures,
  duplicated starts, missing ends, changed hashes, unexpectedly large prompts,
  overwritten artifacts, gaps in metrics, and state that advances without
  durable evidence.
- Check invariants implied by the workflow: guards precede mutations, task
  baselines remain stable through retries, required commits exist before index
  advancement, validation covers the intended diff, and completed work is not
  replayed.
- Only after an evidence anomaly is concrete, inspect relevant source, tests,
  blame, and changes since the last release. This keeps a large unreleased diff
  from turning into speculative review.
- Try to disprove each theory with an independent evidence layer. Mark it
  likely, not confirmed, when the contradiction cannot be resolved.

Useful project anchors include `docs/run-state-and-audit.md`,
`docs/usage-and-cost-tracking.md`, `internal/runner/`, `internal/exec/`,
`internal/liverun/`, `internal/runview/`, `internal/metrics/`, and `workflows/`.

## Monitor a live run

For a continuing audit, maintain a read-only watcher over newly appended audit
records and process liveness while periodically resampling state, Git HEAD, and
the lock.

- Report transitions at task starts, retries, fixer completions, resume
  boundaries, and run termination. Avoid dumping full prompts or validator
  payloads when a concise event summary is enough.
- Re-run the structural checks on each new resume segment and compare the new
  task with prior peer tasks. Continue looking for new classes of anomaly; do
  not reduce monitoring to matching previously seen error strings.
- A long agent turn with a live child and growing output is not a stall. A dead
  child, unchanged output, and a runner that never emits a terminal event is.
- If the runner exits, allow for final audit/state flushing, then classify the
  terminal evidence. Do not auto-resume or clear the lock.

In explicitly authorized audit-and-repair mode, diagnose an unexpected failure
first. If Agent Runner owns the cause, reproduce it with a failing test in the
designated dev checkout, implement the narrow fix, and run focused then broader
checks per the repository instructions. Keep monitoring the original run, but
do not inject the fix into it. Report whether exercising the fix will require a
new binary and a human-controlled restart or resume.

## Report

Lead with the current run status and the highest-impact findings. For each
finding include:

- classification: confirmed defect, likely defect, expected/external failure,
  already-fixed-but-not-exercised, or unresolved evidence gap;
- concrete evidence: timestamps, event paths, PIDs, SHAs, commands, artifact
  names, and relevant source locations;
- impact on this run and likely impact on other runs;
- likely owning component and whether the loaded runtime contains a fix;
- safe next action, clearly separating diagnosis, source repair, rebuild, and
  run mutation.

End with what was inspected, what remained live or uncertain, and any changes
made. Never imply that a source fix repaired an already-running process.
