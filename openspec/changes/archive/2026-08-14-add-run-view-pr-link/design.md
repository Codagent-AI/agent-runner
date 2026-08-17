# Design

Only the non-obvious decisions are recorded here. Everything else follows the existing run-view and runner patterns.

## Context

Agent Runner has no run-level notion of a pull request. `RunState` carries the workflow file, params, profile set, and intake provenance; `runs.RunInfo` adds display metadata. Neither has a branch, remote, or PR field. The run view builds its entire model from two sources: `state.json`, read once in `runview.New()`, and the audit stream, which it replays in full for historical runs and tails live via `FileTailer`.

The URL itself is not hard to obtain. `workflows/core/implement-change-v1.0.yaml:116` already runs `gh pr list --head "$branch" --state open --json number,url,state,isDraft,baseRefName,headRefOid` inside `verify-draft-pr` and parses several fields out of it, discarding `url`. That step runs only after the agent has confirmed the PR is open, is a draft, and matches local `HEAD`, so its URL is already validated.

## Decision: reserved capture name over a new step field or control-channel command

Three mechanisms could carry the URL from a workflow to the runner.

A **new step field** (`records_pull_request: true`, or a typed `capture_as:`) is the most self-documenting, but it pulls the loader, `model.Step`, workflow validation, and the `step-model` capability into a change that is otherwise a display feature. The field would exist to mark exactly one variable.

A **control-channel command** (`agent-runner run set-pr-url <url>`) would let the agent report the PR it just created. It is the largest change — new message type, auth path, handler, audit event — and it makes recording depend on an agent remembering to call it, where a shell step is deterministic.

The **reserved capture name** reuses the capture mechanism unchanged (`internal/exec/shell.go:159`), needs no new YAML surface, and keeps the special-casing to one helper. `capture: pr_url` reads honestly in the workflow file: it is a capture, and it is the PR URL.

The cost is a naming convention, which is the pattern the `ChangeName` TODO in `internal/runs/runs.go:39` objects to. The objection there is specifically about *sniffing*: `runs.go` inspects a param the workflow never agreed to expose, so a workflow that happens to take a `change_name` param gets its value repurposed. This is the inverse — `pr_url` is documented as reserved, a workflow opts in by capturing it, and the recording happens at one known point rather than being guessed at read time. If a second reserved name ever appears, that is the signal to promote the mechanism to a real step field.

**Consequence:** a workflow that captures `pr_url` for an unrelated purpose gets a PR segment. Acceptable for a reserved, documented name.

## Decision: the audit event is the only source the run view reads

The obvious move is a `RunState.PullRequestURL` field, matching how `profileSet` reaches the breadcrumb. It does not survive contact with the live view.

`runview.New()` reads `state.json` exactly once. A state field would therefore show up for historical runs but not appear mid-run, and fixing that means re-reading `state.json` on the refresh tick — a second, differently-shaped update path alongside the audit tail that already drives every other live update.

A `pull_request_recorded` audit event has no such split. The run view replays the whole audit log for a historical run and tails the same log live, so one parser in `internal/runview/audit.go` serves both modes with identical behavior, and `RawEvent.Data` is already a generic `map[string]any`.

So `RunState.PullRequestURL` is deliberately **not** added. Nothing would read it: the run list is out of scope, and the run view is better served by the event. The value is still durable in `state.json` as an ordinary entry in `CapturedVariables`, which is what makes it survive resume. If the run list later wants a PR column, adding the state field then is a small change with a real consumer.

## Decision: emit at the capture site, not at the state-write boundary

The tempting approach is to check for the reserved name once, where `internal/runner/runner.go` builds `RunState` from the accumulated `ctx.CapturedVariables` (runner.go:807 and runner.go:903). One place, every step type, no executor edits.

**It does not work, and the reason is the whole point of this section.** Captured variables do not flow child-to-parent across a sub-workflow boundary. `internal/model/context.go:382` gives a child context a *copy* of the parent's map, and when the child completes, `internal/exec/subworkflow.go:245` copies its captures into a `NestedStepState` that is attached as `parent.LastSubWorkflowChild` — persisted state, not the parent's live context. `restorePersistedSessions` (subworkflow.go:288) moves captures the other way, restoring them *into* a context on resume; it is not a merge. Only loops merge upward, via `mergeIterationCaptures` (`internal/exec/loop.go:259`).

That asymmetry is fatal to the root-boundary approach here, because the sub-workflow case is the only case that matters: `core/implement-change-v1.0.yaml` is composed as a sub-workflow by every PR-producing lineage. A root-context check would work when tested standalone and silently find nothing in real use.

So the event is emitted **where the capture is bound**, through a small shared helper called from the capture sites (`shell.go:159`, `agent.go:224`, `script.go:62`, `ui.go:40`). Recording only has to reach the audit stream, and the audit logger is reachable from the execution context at any depth — so nesting stops being a consideration rather than being compensated for. The helper is also the natural single place to apply the normalization rules below.

This leaves `pr_url` an entirely ordinary captured variable. Nothing is promoted, no run attribute is derived, and the only special behavior attached to the name is "also emit an audit event when it is bound".

Duplicate suppression is per emitting context: re-binding the same value emits nothing. A resumed session restores `pr_url` from state and may emit once more. That is left alone rather than persisting a "last emitted" marker, because the consumer contract is *the most recent event wins* — a repeated identical event changes nothing the run view displays, and the marker would be extra state serving no reader. The spec is written to permit it.

## Decision: normalize the value, and ignore captures that cannot be a URL

Shell capture keeps stdout verbatim — `captureShellOutput` (`internal/exec/shell.go:151`) assigns `result.Stdout` with no trimming, so an `echo`-produced value carries a trailing newline. Embedded in an OSC 8 target, that newline breaks the hyperlink. The helper therefore trims surrounding whitespace before comparing, recording, or emitting.

Captures are not always strings: `script.go` can bind a list or map, and `ui.go` binds a map of inputs. A non-string capture kind bound to the reserved name is ignored — no event, no failure. Recording is an observation of the run and must never be able to fail a step.

The event payload is `{"url": "<trimmed-url>"}`, named explicitly so runner and run-view tests can assert one schema.

## Decision: OSC 8 with the existing sanitizer

The hyperlink is `ESC ] 8 ; ; <url> ESC \ <label> ESC ] 8 ; ; ESC \`.

`tuistyle.ansiEscapeRe` (`internal/tuistyle/format.go:257`) already matches both OSC terminator forms, including `\x1b\][^\x1b]*\x1b\\`. A URL contains no `ESC`, so `tuistyle.Sanitize` strips both the opening and closing sequences and leaves the label. `renderChrome()` measures with `runewidth.StringWidth(tuistyle.Sanitize(crumb))`, so logo and rule placement stay correct with no change to the measurement code. This is worth a test rather than an assumption: it is the failure mode that would silently misalign the whole chrome line.

## Risks and trade-offs

- **A stale URL.** The recorded URL is whatever was true when captured. A PR closed and reopened as a new one after the recording step shows the old link. Accepted: the alternative is resolving at view time, which is wrong for historical runs by construction.
- **Breadcrumb crowding.** The chrome line already carries name, version, profile, elapsed or start time, and status. `· PR #62` adds roughly eight columns. `RenderChromeWithLogo` already drops the logo when the left content does not fit, so a narrow terminal degrades by hiding the logo rather than corrupting the line. No narrow-terminal suppression logic is specified; if it proves cramped in practice, that is a follow-up.
- **A URL the label cannot parse.** Falls back to the bare `PR` label, still linked.
