---
title: External User Mode
group: Guides
order: 6
description: Run interactive Claude and Codex steps without a terminal using a file-based responder.
---

# External User Mode

Install and authenticate Claude or Codex first. An external program can then supply user turns for interactive steps. Create an empty exchange directory, then run:

```bash
agent-runner run openspec:change --external-user /absolute/exchange --external-user-timeout 10m --until define
agent-runner --resume <run-id> --until define
```

`--external-user` implies no TUI. The directory must already exist, be writable, and contain no `*.request.json` or `*.reply.json` files when starting a fresh run. Its absolute path and timeout are saved in run state. Resume reuses those settings and rejects either external-user flag. Omit the timeout to wait indefinitely; interrupts still stop the wait. `agent-runner --help` lists both flags as a capability probe.

Interactive steps run as successive headless turns on one session. The first turn receives the usual step instructions and completion command. Later replies are sent verbatim, including text beginning with a dash. AskUserQuestion is disabled; the agent asks in plain text. These steps omit the autonomy preamble and use the user's autonomous permission setting. The caller's sandbox provides the isolation boundary. Declared `call_agent` tools are pre-authorized on every turn.

Only Claude and Codex interactive leads are supported. Autonomous steps run headlessly regardless of the configured backend. Interactive shell and UI steps fail immediately.

## Exchange contract

A turn that ends successfully without control-channel completion writes:

```text
<step-key>-<attempt>-<turn>.request.json
```

The step key uses `[A-Za-z0-9._-]`. Path components are encoded separately and joined with dots; authored dots, underscores, and unsafe characters become `_hex_` escapes. Loop iterations append `_i_<iteration>` to their component, and repair attempts append `_r_<attempt>`. Sub-workflow and group step IDs are included. Attempt numbers follow the audit log; turn numbers start at 1 in each attempt. A request contains:

```json
{
  "schema_version": 1,
  "run_id": "run-id",
  "step": "define.proposal",
  "step_id": "proposal",
  "attempt": 1,
  "turn": 1,
  "cli": "claude",
  "session_id": "session-id",
  "agent_message": "What should this change do?",
  "empty_turn": false
}
```

`agent_message` contains all lead assistant text in order, excluding native subagent text. Empty turns still produce requests with `empty_turn: true`.

Write the reply to the same stem with `.reply.json`:

```json
{"schema_version": 1, "text": "Build a slide tool"}
```

Text must be nonempty. To abort:

```json
{"schema_version": 1, "action": "abort", "reason": "turn cap reached"}
```

Both sides must write to a temporary file in the same directory and rename it into place. Runner request permissions are 0644; responders should use permissions readable across their bind mount, such as 0644. A pre-existing reply for a new request fails the step. Replies must be regular files inside the exchange directory; Runner refuses to follow a reply symlink that resolves outside it. Replies cannot complete steps: the agent must invoke `agent-runner step complete`, and Runner waits for acknowledgement and durable turn evidence.

Malformed replies, aborts, failed CLI processes, and timeouts fail the step. Replace a malformed reply before resuming, because resume reads the same pending file again. Runner never supplies a default answer.

## Resume and evidence

The authoritative replay record is `<run-directory>/external-user/exchanges.jsonl`. Requests are recorded and fsynced before publication; valid replies are recorded before use. A pending request is restored if missing, then awaited under its original identity. A reply whose turn was interrupted is replayed verbatim. If the completion executable changed, Runner appends the updated instruction and audits that refresh. An abort is terminal for its exchange; a later resume uses the ordinary resume prompt. Completed steps are skipped. New exchanges after resume use the new attempt number.

The exchange directory contains transport files only. The run's `audit.log` records request and reply identity, replay, and failures. Usage in `run-metrics.json` aggregates turn reports, preserving partial coverage when usage is missing and attributing cumulative counters against prior session reports.

`--until` is inclusive and applies only to the current invocation, including resume. An invalid or already-passed resume target fails before dispatch and leaves state unchanged. A successful cap exits 0 and prints `stopped after step "<id>" (--until).`. State records the reached top-level step in `currentStep`, with `currentStep.completed: true`; the run's `completed` remains false (and may be omitted from JSON) while later steps remain. The audit log includes that step's successful `step_end` and a `run_end` with `outcome: "success"` and `completed: false`. An interruption has no successful capped-stop message; its active step remains incomplete (or the previously completed step is still the resume position if interrupted between steps). Reaching the final step marks the run completed.

Raw CLI stdout and stderr use the existing `<run-directory>/output/<sanitized-audit-prefix>.out` and `.err` files, which hold the latest turn. Reusing a prefix archives the previous files as `.bak-*`, retaining up to eight archives per stream. Each external-user turn is also kept in full as `<sanitized-audit-prefix>.attempt-<attempt>.turn-<turn>.out` and `.err`, so every turn's raw stream-json or JSONL output survives long conversations. If a per-turn copy cannot be created, Runner continues the turn and reports a warning on stderr and in `output/persistence-warnings.log`. Resumed steps start a new attempt number, so earlier attempts' turn files are not overwritten.

`run-metrics.json` lists every agent invocation, including each `call_agent` child, with its `cli` and `session_id`. Use it as the manifest when collecting native CLI session files for a transcript audit. The exchange record retains relayed assistant text and replies, but does not contain tool transcripts.
