# Decisions

## propose

| Decision | Alternatives considered | Decision-bearing |
| --- | --- | --- |
| Verdict: go with caveats. The four enhancements are additive to the existing attempt-scoped agent-call handler and unblock `call_agent` as the orchestration path for PR #209. Caveat: the delta specs build on the unarchived `async-mcp` contract. | No-go (keep CLI-native sub-agents); defer until parallel calls are designed. | Yes |
| Deadline is an optional per-call `timeout` field on `call_agent` (and follow-ups); omission keeps today's unbounded behavior. | Profile-level `max_duration` (persisted config change, applies to all callers); Runner-wide default ceiling (changes existing behavior); both. The issue offers either as an example. | Yes |
| A timed-out call ends with a distinct `timed_out` error code rather than reusing `call_canceled`. | Reuse `call_canceled` with a reason message. | Yes |
| Follow-up is a third mutually exclusive target, `follow_up: <call_id>`, with a new `call_id` per follow-up. | A `resume: true` flag next to `agent`; reusing the original `call_id`. | Yes |
| Follow-ups are limited to terminal `agent:`-target calls (or follow-ups) in the same parent attempt; named-session calls are rejected because `session:` already resumes them. `cli` override is rejected; `model`, `workdir`, `timeout` are allowed. | Allow named-session call IDs; allow cross-attempt follow-ups via persisted call records. | Yes |
| Follow-up sessions are not added to the run's named-session map. | Auto-register them as named sessions. | No |
| Structured result adds a `details` object (exit status, duration, session descriptor, commits) on terminal success and failure; `result.response` text is unchanged. | Replace the response with a new shape (breaking); success-only details. | Yes |
| The raw native CLI session ID stays out of the MCP result; the `call_id` is the lead's resumable handle. This reads the issue's "the child session" as a session descriptor, preserving the existing contract that keeps raw session IDs, usage, and cost out of tool results. | Return the native session ID. | Yes |
| Commits are the `start..end` range of `HEAD` in the call's workdir, capped with a truncation flag. | Diff stat; all reflog movement; no commits. | No |
| Progress heartbeats fetch a bounded activity snapshot from the Runner over the control channel; adapters optionally summarize their stream, falling back to time since last output. | Stream raw child output in progress messages; keep bridge-local state. | No |
| Out of scope: archiving `async-mcp`, updating the sibling `codagent:call-agent` skill, and switching `orchestrate-implementation` to `call_agent`. The new fields are self-describing in the MCP schema, so no out-of-repo change is required. | Bundle the skill update (requires sibling-repo change). | Yes |

## proposal-review

| Finding | Disposition | Decision | Alternatives considered | Decision-bearing |
| --- | --- | --- | --- | --- |
| PR-001: follow-up drops the source call's workdir (resume path defaults to the parent's directory; call records keep no resolved settings). | Applied | Call records retain resolved profile, CLI, effective model, and effective workdir. An omitted follow-up `workdir` inherits from the directly referenced call, so overrides carry down a chain. Explicit `workdir` keeps existing rules (relative to the parent's effective directory, contained in the worktree). A CLI that cannot resume the session fails the follow-up as a structured error and never starts a fresh child. | Require the lead to repeat `workdir` on every follow-up; resolve explicit overrides relative to the source call's workdir. | Yes |
| PR-002: follow-up `model` override conflicts with adapters that omit the model on resume (Claude, Copilot, Cursor, OpenCode; Codex applies it). | Applied | Follow-ups inherit the source call's model. An explicit, different model is accepted only when the adapter declares that it applies a model on resume; otherwise it is rejected before acceptance. `details` reports the effective model passed to the child. Universal resume-model support and named-session model handling are out of scope. | Reject all follow-up model overrides; add resume-model support to every adapter; accept and silently drop. | Yes |
| PR-003: `start..end` HEAD range cannot prove commits were made during the call; non-git workdirs are unaccounted for. | Applied | Narrow the contract to an observed `HEAD` delta: start/end `HEAD`, commits reachable from end but not start (capped), and an evidence state (captured, not a git repository, capture failed/timed out, non-linear history). Document it as an observation, not authorship. Git commands are time-bounded, and evidence failure never changes the call outcome. Authoritative attribution is out of scope. | Commit-trailer or hook-based provenance; drop commits from the result. | Yes |

## spec

| Decision | Alternatives considered | Decision-bearing |
| --- | --- | --- |
| `agent-calls` MODIFIED blocks are copied from the unarchived `async-mcp` delta where it modifies them (`Long-running MCP execution`) and from the main spec otherwise (`Invocation fields and valid forms`). `async-mcp` must be archived before this change. All other behavior is ADDED, which avoids conflicts with `async-mcp`. | Delta against the main spec only (would revert async behavior on archive); fold `async-mcp` into this change. | Yes |
| `timeout` is a Go-style duration string, 1s to 24h, timed from acceptance. Invalid values are rejected before acceptance with `invalid_request`. | Integer seconds; no upper bound; timed from CLI launch. | Yes |
| A timed-out call ends with status `failed` and error code `timed_out`. No new terminal status is added, so existing callers that check `succeeded/failed/canceled` keep working. Explicit cancel or any earlier terminal outcome wins over the deadline. | New `timed_out` status; `canceled` status. | Yes |
| A follow-up may reference a call with any terminal status, including failed, canceled, or timed out, provided its native session was discovered. Rejection codes: `unknown_call`, `call_in_progress`, `invalid_target` (named-session source or multiple targets), `not_resumable`, `self_session`, `invalid_model`, `invalid_request` (`cli`). | Succeeded-only sources; a single generic rejection code. | Yes |
| A follow-up's result target identifies it as a follow-up naming the referenced `call_id`; the run view labels it as a follow-up of that call; audit and metrics name the referenced call. Timeout and follow-up evidence are specified in `agent-calls`, not as `audit-log-entries` deltas. | Reuse the `agent` target kind; add an `audit-log-entries` delta. | No |
| `details` appears on every terminal response with a `call_id` (including oversized and launch failures), never on snapshots or pre-acceptance rejections. Duration runs from acceptance to the terminal outcome, in the same format as `elapsed`. The git delta caps commits at 50, newest first, with `truncated`; states are `captured`, `not_git`, `unavailable`, and `non_linear`. | Success-only details; launch-based duration; an uncapped list. | Yes |
| The activity summary is one line of at most 200 characters with the event kind, tool name, and age. It excludes message text, tool arguments, and output. Claude and Codex provide summaries; other adapters fall back to output recency. Snapshot failure degrades to a generic message. | Include tool arguments; summaries for every adapter in this change. | Yes |
| Resume model-override capability: Codex supported; Claude, Copilot, Cursor, and OpenCode unsupported, matching current argument construction. | Change adapters to pass the model on resume. | No |
| Whether the control-channel snapshot is keyed by request ID or `call_id` is deferred to design. | Fix it in the spec. | No |

## design

| Decision | Alternatives considered | Decision-bearing |
| --- | --- | --- |
| The deadline uses `context.WithCancelCause` with a sentinel cause; cancel, timeout, and teardown share one path, and the first cause wins. | A separate timed-out flag; `context.WithTimeout` per call. | No |
| Call records retain the resolved invocation (profile, adapter, CLI, model, workdir) and the native session ID (discovered, or pre-assigned once launched). Follow-ups copy from the record rather than re-resolving profiles. | Re-resolve the source profile at follow-up time. | No |
| Resume model capability is an optional adapter interface `cli.ResumeModelApplier` (Codex only), guarded by a registry-wide args test. | A hard-coded CLI list in exec. | No |
| Follow-ups check `cli.SessionStore.SessionExists` before launch and fail as accepted `not_resumable` without launching; they never fall back to a fresh session. The spec was updated with this pre-launch rule. | Let the CLI fail; the agent-step fresh-session fallback. | No |
| The activity snapshot is a new control message `agent_call_activity`, keyed by exactly one of start request ID or `call_id`. This resolves the deferred spec marker, and the spec was updated. The handler is an optional interface. The bridge fetches on each 15 s tick with a 2 s bound, for both `call_agent` and `get_agent_call`. | Key by `call_id` only (unavailable while blocked in `call_agent`); bridge-side state. | No |
| `Details` lives on `Response` (`exit`, `exit_code`, `duration`, `session`, `git`); git commands run after child exit, each bounded to 5 s, through an injectable runner. | `Details` inside `Result` (success-only); git capture concurrent with the child. | No |
| Activity summarizers are an optional `cli.HeadlessActivitySummarizer` on Claude and Codex, fed by a tee writer in `childStdoutCapture` with a 1 MiB line cap; summaries clamp to 200 runes. | Parse the bounded probe buffer; summarizers for every adapter. | No |
| Audit adds `timeout`, `follow_up_of`, `error_code`, `exit`, and `git_state`; metrics and the run view read them as optional. Follow-ups are labeled `call follow-up: <call_id>`. | Leave audit unchanged. | No |

## test-plan

| Decision | Alternatives considered | Decision-bearing |
| --- | --- | --- |
| Integration tests use fake CLI shell scripts, a real control socket, the real bridge, real `git`, and recorded real CLI JSONL fixtures. They run in the existing CI `test` job with second-scale timeouts and an injected 100 ms heartbeat; no new suite. | Real CLIs in CI (needs credentials); pure unit coverage of each layer. | No |
| Two real-agent E2E journeys (follow-up by call ID with a git commit; hung child rescued by `timed_out`) extend the existing `e2e_agents` build-tag harness, run locally with existing Claude credentials, not in CI. | A CI E2E with fakes only (duplicates the integration tests); no E2E. | No |
| Acceptance may use existing local CLI logins with a bounded number of short turns, temp repos only. It must not touch remotes, global CLI config, the live factory, or this checkout's history. Fakes may substitute for unavailable CLIs. | Fake CLIs only; unrestricted real-agent use. | No |
| No human-only testing. | An HT for TUI review (covered by the exploratory pass). | No |

## approach-review

| Finding | Disposition | Decision | Alternatives considered | Decision-bearing |
| --- | --- | --- | --- | --- |
| AR-001: completion, cancel, and timeout have no shared outcome arbitration; context causes cannot express a natural exit, and the exit-to-publication window can flip outcomes. | Applied | One synchronized settlement per call (`trySettle` under `h.mu`). It is triggered by an `OnExited` hook right after process wait (before usage, session, and git collection), the deadline timer, explicit cancel, teardown, or a pre-launch failure; the first wins and is frozen. The response, `details.exit`, `details.duration` (acceptance to settlement), audit, and metrics derive from it; evidence collection follows settlement and cannot change it. The spec now defines settlement and adds post-exit deadline/cancel scenarios; INT-001 adds deterministic race cases with injectable timer and pause hooks. | `WithCancelCause` only; treating result publication as the boundary. | Yes |
| AR-002: `details.session.model` would report an unapplied named-session model override as effective. | Applied | Report the model only when known: passed to a fresh session, passed on resume by an adapter that applies it, or inherited by a follow-up from a known source. Otherwise omit it. Named-session request handling is unchanged (still a non-goal). A spec scenario and an INT-003 case cover a Claude named-session resume with a differing model. | Report the requested model; reject named-session model overrides (behavior change, out of scope). | Yes |
| AR-003: E2E-001 cannot prove native session reuse (token is in the worktree, and `resumed` reflects intent). | Applied | The token stays only in the child's memory, and the commit is unrelated content. The oracle uses native evidence: the same `resolved_session_id`, both prompts in the same Claude transcript in the isolated `HOME`, and no new transcript during the follow-up. A fresh-call negative control must fail the oracle. | Keep the token-file check; rely on `details.session.resumed`. | No |
| AR-004: the output-recency fallback ignored stderr. | Applied | Recency is updated from both raw stdout and stderr (a recency-only stderr tee around any adapter wrapper, bytes unchanged); structured activity is parsed from stdout only; stderr content never appears in summaries. Spec wording and a scenario were updated; INT-002 adds a stderr-only child. | Narrow the spec to stdout-only recency. | No |

## tasks

| Decision | Alternatives considered | Decision-bearing |
| --- | --- | --- |
| `tasks.md` holds exactly one implementation task covering the whole change, grouped by area and following the format of the archived `feature-211` change, with source-file links and finish criteria (`make fmt`, `make test`, `make lint`). | Multiple tasks per area. | No |
| The task forbids archiving this change before `async-mcp`, and forbids editing the sibling Agent Skills repository. | Leave ordering implicit. | No |
