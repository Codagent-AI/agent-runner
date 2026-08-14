## Why

Most Agent Runner change workflows end by opening a pull request, but the run view never mentions it. After a run finishes, the user has to leave the TUI and find the PR by hand — `gh pr view --web`, or the branch name and a browser. The run view already knows more about the run than any other surface, and the PR is the single most useful thing to reach from it.

Nothing in a run's state, audit stream, or `RunInfo` records a pull request today. The information exists during the run: `core/implement-change-v1.0.yaml` already queries `gh pr list --head "$branch" --json number,url,...` to verify the draft PR, and then discards the URL. This change keeps it.

## What Changes

### Recording

- Reserve the captured-variable name `pr_url`. When any step binds it to a non-empty string, a `pull_request_recorded` audit event carrying the URL is emitted at the point of capture.
- Emitting at the capture site is what makes nesting a non-issue. Captured variables do **not** propagate child-to-parent across a sub-workflow boundary, so a check at the root state-write boundary would miss `core/implement-change-v1.0.yaml` running under `openspec/change-v2.0.yaml` — which is the only case that matters. See `design.md`.
- The value is trimmed before use, since shell capture keeps stdout verbatim and a trailing newline would break the hyperlink. Non-string capture kinds bound to the reserved name are ignored.
- Recording never fails a step. It is an observation of the run, not an outcome. This does not relax any existing validation: `verify-draft-pr` keeps its current failure semantics, and recording is added to it without weakening the checks it already performs.

### Display

- When a run has a recorded pull request, the run-view breadcrumb shows a dim `· PR #62` segment after the run status, in the same style and with the same separator as the existing recorded-version and profile-set segments.
- The segment is an OSC 8 terminal hyperlink to the full URL, so it is clickable in iTerm2, Ghostty, WezTerm, and VS Code. Terminals without OSC 8 support show the plain `PR #62` text.
- The segment appears in every entry mode that displays a run — live, `--inspect`, and from the run list — and appears mid-run as soon as the URL is recorded, without re-entering the view. Because the metrics summary screen renders the same chrome, it gets the segment too.

### Built-in workflows

- `core/implement-change-v1.0.yaml`: the existing `verify-draft-pr` step emits the already-fetched URL and captures it. Its existing guards are unchanged — it still fails unless exactly one open, draft, HEAD-matching pull request exists. Recording rides along on a check that has already passed.
- `core/finalize-pr-v1.0.yaml`: a new best-effort step after `push-pr` records the URL, since this workflow can open the PR when run standalone. This is the step the failure-tolerance rule applies to: unavailable or unauthenticated `gh`, or no open PR, records nothing and does not fail the run.

These two files are composed by every PR-producing lineage (`openspec/change-v1.0` and `v2.0`, `spec-driven/change-v1.0` and `v2.0`, and both `implement-change-v2.0` variants), so no top-level change workflow needs editing.

## Capabilities

### New Capabilities

- `run-pull-request-link`: recording a run's pull request URL from a reserved capture, and rendering it as a clickable breadcrumb segment in the run view.

### Modified Capabilities

None. The new breadcrumb segment composes with the existing recorded-version and profile-set segments rather than changing them, in the same way `live-run-view`'s "Breadcrumb shows a non-default profile set" requirement was added alongside the version label.

## Out of Scope

- The run list (`--list`, `--resume`). Showing a PR marker per row would require reading each run's audit stream on every list render.
- A first-class `RunState.PullRequestURL` field. See `design.md`: the audit stream serves live and historical runs through one code path, and nothing would read the state field.
- Forge-agnostic support. GitLab merge requests and other hosts still render and link correctly, but only GitHub `/pull/<n>` URLs get a numbered label.
- Any keybinding that opens the URL in a browser. OSC 8 covers the terminals in use.
- Detecting a PR that a workflow opened without recording it, or one opened outside a run.

## Impact

- `internal/exec/`: a shared helper invoked from the capture-binding sites in `shell.go`, `agent.go`, `script.go`, and `ui.go` that trims the value and emits the new audit event for the reserved name.
- `internal/audit/`: one new `EventType`.
- `internal/runview/`: parse the new event while building the tree; render the segment in `renderBreadcrumb()`.
- `workflows/core/implement-change-v1.0.yaml`, `workflows/core/finalize-pr-v1.0.yaml`: record the URL.
- `openspec/specs/run-pull-request-link/`: new capability.
- No state-file schema change, so existing runs stay readable and older runs simply show no segment.
