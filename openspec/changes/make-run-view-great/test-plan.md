## Coverage Strategy

Specifications remain the source of unit-test requirements. Implementation-time
TDD shall cover the pure tree projection, layout calculations, audit ordinals,
detail-document construction, key-state transitions, and other isolated
behavior enumerated by the specifications and design. This plan records only
additional integration and agent-acceptance obligations.

The highest cross-component risks are:

- replaying real workflow, state, audit, metrics, and output artifacts into one
  coherent historical selected-step view;
- carrying live coordinator messages and subprocess output into the correct
  selected node without changing manual scope or stealing paused selection;
- preserving agent-call output filtering and attribution when a call is either
  selected or used as previous-execution context;
- keeping workflow-defined UI input alive while ordinary run-view navigation
  continues; and
- delivering the responsive visual hierarchy in a real terminal rather than
  only in string-level renderer tests.

Agent and agent-call coverage must be deterministic and zero-cost. Use fake
local CLIs, deterministic subprocesses, and recorded output. No obligation in
this plan authorizes network access, real model invocation, use of personal
credentials, or API cost.

## Integration Tests

### INT-001: Historical view projects durable run artifacts

- Covers: historical selected-step initialization; previous execution context;
  latest-attempt ordering and metadata; selected detail for shell, script,
  interactive agent, headless agent, agent call, and containers; persisted
  output filtering; large-output loading; metrics visibility; missing workflow
  recovery; recorded workflow-version presentation; nested-selection resume
  fallback; skipped-execution recap; and copy payload construction.
- Boundary: real workflow resolution, `state.json`, ordered `audit.log` replay,
  `run-metrics.json`, persisted agent-call output files, CLI output filters, and
  `runview.New(..., FromInspect)` through the selected-detail renderer.
- Setup: create an isolated temporary project and session directory containing
  a resolvable nested workflow plus real serialized state, audit, metrics, and
  output artifacts. Include:
  - completed shell and script leaves with distinct stdout and stderr;
  - a failed script whose recap must prefer stderr;
  - an interactive agent with no transcript;
  - a headless agent with filtered output;
  - a dynamic fake agent call with separately persisted raw output;
  - a logical step with more than one attempt;
  - a skipped leaf with a recorded triggering `skip_if`;
  - nested containers and more than five children;
  - an inline-selected row whose nearest workflow-bearing ancestor differs from
    the manual drill scope;
  - structured metrics; and
  - output exceeding the existing lazy-loading threshold.
  Add focused variants for a failed run and for recoverable audit history whose
  workflow file is unavailable.
- Action: construct the historical model through its production entry
  constructor, apply representative terminal sizes and navigation keys, render
  sanitized selected detail, invoke the semantic copy path, and load full
  output through the existing action.
- Assertions:
  - audit replay produces deterministic latest-attempt start order;
  - the previous rail identifies the latest earlier terminal leaf, excludes
    containers, includes calls, and uses the correct output source;
  - interactive execution never acquires a fabricated transcript;
  - selected call output comes from bounded persisted files and ordinary
    adapter filtering rather than an audit response field;
  - call count, target, raw evidence, filtering, and inactive direct resume
    remain available while session/workdir stay out of ordinary primary detail;
  - unavailable metrics retain `?` and their reason, absent cost is never
    `$0.00`, legacy metrics are omitted, and resumed/inherited agents use the
    session-originating model;
  - a skipped previous execution shows its `skip_if` reason and no output;
  - `r` uses the nearest workflow-bearing selected ancestor before root;
  - saved historical breadcrumbs retain their recorded version or
    `unversioned`;
  - the collapsed display and copied full input differ only as intended;
  - lazy output and full output use the same selected execution;
  - failed historical entry selects the failed leaf at root scope;
  - completed metrics entry retains the summary-first behavior; and
  - a missing workflow can be recovered from sufficient audit evidence without
    inventing pending structure.
- Constraints: all files and CLIs are local fixtures; output reads remain
  bounded; no system clipboard write is performed; no network, credentials, or
  model invocation is allowed.
- Execution: ordinary Go tests alongside `internal/runview`, included by
  `go test ./...` and CI.

### INT-002: Live coordinator messages and output preserve view ownership

- Covers: real-time output; active-leaf expansion and selection; independent
  active-step and detail-tail follow; live agent-call integration; ANSI
  sanitization with raw persistence; manual navigation without selection
  theft; output-path escaping and first-byte latency; and live UI request
  ownership.
- Boundary: a real `liverun.Coordinator`, its `TUIProcessRunner`, deterministic
  local subprocesses, output files, coordinator messages, and a live
  `runview.Model` connected through a forwarding terminal-program test
  implementation.
- Setup: create an isolated live session with a nested workflow tree and output
  directory. Use deterministic local subprocess fixtures that emit delayed
  stdout and stderr, split ANSI sequences, enough rows to scroll, and
  independently addressed parent and fake agent-call output. Connect
  coordinator `StepStateMsg`, `OutputChunkMsg`, `UIRequestMsg`, and completion
  messages to the live model through the same message boundary used by Bubble
  Tea.
- Action:
  - advance execution from a parent into nested leaves and a fake agent call;
  - stream output while the producing row is selected;
  - navigate to another row, scroll the old response upward, and stream more
    output;
  - exercise `t` and `l`;
  - issue a live UI request, navigate away from the UI leaf, return with `l`,
    and resolve the form; and
  - finish once successfully and once with a nested failure.
- Assertions:
  - active execution expands and selects the leaf without mutating manual drill
    scope;
  - parent and call output remain separate;
  - display output is sanitized while persisted output retains the original
    bytes;
  - split ANSI sequences are removed across chunk boundaries, raw files use the
    established escaped-prefix path, and first-byte display meets the 100 ms
    target under the deterministic fixture;
  - output continues accumulating while selection is elsewhere;
  - paused selection and scroll offset are not stolen;
  - `t` follows only the selected execution and continues tailing later bytes,
    while `l` returns to root active selection and resumes both follow modes;
  - navigating away leaves the UI request unresolved and stops form-key
    routing;
  - returning to the UI leaf restores form-key routing without auto-drilling;
    and
  - terminal failure selects the failed leaf at root scope.
- Constraints: use only local deterministic subprocesses and fake CLIs. Do not
  invoke a real agent, contact the network, or require user credentials.
- Execution: ordinary Go tests in the existing `internal/runview` and
  `internal/liverun` test phase, included by `go test ./...` and CI.

## End-to-End Tests

No new automated end-to-end obligation is warranted.

The delivered surface is a full-screen Bubble Tea terminal. A PTY's accumulated
ANSI byte stream contains obsolete frames and does not provide a stable
assertion of the final responsive layout. Adding a terminal-emulator dependency
or a new screen-reconstruction framework solely for this change would exceed
the design scope. The integration obligations exercise the real artifact,
coordinator, subprocess, filtering, and persistence boundaries. The
agent-acceptance flows exercise the built binary in an ANSI-capable terminal
and preserve final-frame screenshots as visual evidence.

This omission does not waive the existing repository-wide tests or existing
smoke and real-agent E2E suites. Those continue to run according to their
current triggers, but this change adds no model-backed E2E requirement.

## Agent Acceptance Tests

### AT-001: Inspect a mixed historical run

- Classification: Required.
- Covers: historical tree and detail presentation; selectable nested rows;
  optional manual drill scope; dynamic sidebar sizing;
  whitespace pane separation; per-type metadata; labeled rails; previous
  execution context; input expansion; agent-call detail; large-output action;
  metrics summary preservation; and responsive wrapping.
- Actor and surface: an agent acting as a user in the built
  `agent-runner --inspect <run-id>` terminal UI.
- Setup: build the branch binary. Under an isolated temporary home and project,
  prepare a deterministic completed session with the same representative data
  classes as INT-001, including a fake agent call, a previous successful
  script, a previous failed script, an interactive agent without transcript,
  long input, more than five nested children, and oversized selected output.
  Use no real agent or personal run data.
- Steps:
  1. Open the run and, when structured metrics open the summary first, press
     `s` to enter detail.
  2. Traverse top-level and inline nested real rows with Up and Down, including
     rows before and after an omission marker.
  3. Select containers and leaves of each representative type and inspect their
     labeled detail.
  4. Drill into a container and confirm every direct child is reachable, then
     drill out and confirm the breadcrumb scope returns.
  5. Expand and collapse a long current input with `i`.
  6. Load oversized output with `g`.
  7. Observe the same view at approximately 120x40 and 80x24 terminal sizes.
- Expected:
  - nested real rows are selectable and omission rows are never selected;
  - selected detail replaces rather than stacks cross-step blocks;
  - omission markers report exact earlier and later counts and are never
    selectable;
  - the sidebar shows complete names when space permits, may exceed half the
    terminal, truncates only names when constrained, follows the defined
    extremely narrow degradation order, and leaves at least a usable detail
    pane whenever physically possible;
  - panes are separated by whitespace with no full-height vertical rule;
  - rails are clearly labeled and visually grouped;
  - script recaps use the correct output tail, and the interactive-agent recap
    shows metadata without transcript;
  - primary metadata includes CLI and model where applicable but omits inactive
    diagnostic fields;
  - collapsed input occupies no more than three visual rows and expands inline;
  - loading full output preserves the selected execution; and
  - resize rewraps the previous excerpt to no more than two visual rows.
- Evidence: final-frame terminal screenshots at wide and narrow widths, plus
  focused screenshots showing the script recap, interactive-agent recap,
  omission rows, and expanded input.
- Effects and cleanup: creates only an isolated binary, temporary home, project,
  and run artifacts. Remove them after evidence is collected. Do not press
  `c`, so the user's clipboard is not modified.
- Permitted substitutes: an ANSI-capable PTY with reliable final-frame capture
  may substitute for an interactive terminal. Direct calls to `Model.View()`,
  dry runs, and static mockups are not substitutes.

### AT-002: Follow a live nested execution without auto-drilling

- Classification: Required.
- Covers: active-leaf inline expansion; no automatic drill-in; live output;
  paused manual exploration; in-scope active ancestry; dual-focus windowing;
  separate tail and active follow; live agent-call integration; manual drill
  behavior; and successful completion.
- Actor and surface: an agent acting as a user in the built live run-view TUI.
- Setup: under an isolated temporary home and project, run a local deterministic
  workflow containing nested sub-workflows, a loop with more than five
  iterations or children, delayed shell and script output, a long active
  response, and a zero-cost fake autonomous agent executable that makes a fake
  accepted agent call. Delays must be
  long enough for the stated interactions but bounded so the flow completes
  promptly.
- Steps:
  1. Observe execution enter nested work while active follow is engaged.
  2. Confirm the root breadcrumb remains unchanged while the active leaf becomes
     selected inline.
  3. Use Up or Down to select an earlier row, then use `k` to inspect earlier
     detail while the active execution continues.
  4. Wait for additional output and at least one execution transition.
  5. Press `t`, wait for additional bytes, and observe continuous tailing of the
     same selected execution.
  6. Press `l` and observe the current active leaf at root scope and its output
     tail.
  7. Manually drill into a container, confirm all direct children are
     available, then press `l` again.
  8. Allow the workflow to finish successfully and switch between summary and
     detail.
- Expected:
  - following never changes manual breadcrumb scope;
  - manual navigation pauses active follow and does not hide an in-scope active
    ancestry;
  - distant active and selected foci both remain visible with an exact
    `… N between` marker when needed;
  - the accepted call appears as a separate selectable active leaf, retains its
    parent `(N calls)` count, and never mixes parent and child output;
  - incoming output neither steals selection nor changes the chosen scroll
    position while follow is paused;
  - `t` changes only selected-detail tail following and continues following
    subsequent bytes for that selected execution;
  - `l` restores root scope, active-leaf selection, and both follow modes;
  - drilled scope exposes every direct child without the five-child inline
    window; and
  - successful completion retains the summary screen and detailed view.
- Evidence: terminal screenshots or final-frame captures showing nested active
  selection at root scope, paused exploration with the active path still
  visible, the result after `t`, the result after `l`, and the completion
  summary.
- Effects and cleanup: runs only local shell/script work and a fake CLI inside
  isolated temporary directories. Remove the generated run and fixture files.
  No network, credentials, or API cost is authorized.
- Permitted substitutes: the fake CLI may replay representative filtered agent
  output. A real model-backed run is not permitted or required.

### AT-003: Preserve failed-leaf selection across live and historical views

- Classification: Required.
- Covers: terminal failure behavior, root-scope failed ancestry, selected-step
  error detail, static interrupted indicators, and historical parity.
- Actor and surface: an agent acting as a user in the built live run view and
  subsequent `agent-runner --inspect <run-id>` view.
- Setup: run a deterministic local nested workflow whose leaf script emits
  stdout and stderr and then fails. Use an isolated temporary home and project.
- Steps:
  1. Navigate away from the active execution before the failing leaf exits.
  2. Allow the workflow to reach failed terminal state.
  3. Inspect the selected failed leaf, its ancestry, error metadata, and
     previous-execution recap.
  4. Exit, reopen the same run with `--inspect`, and inspect the initial detail.
- Expected:
  - failure returns to root manual scope and selects the deepest failed leaf
    without creating a drill scope;
  - the failed ancestry is expanded and the deepest leaf carries the failure
    indicator;
  - the failed script detail distinguishes stdout and stderr and shows exit and
    error context;
  - no active animation remains after the run is inactive; and
  - historical inspection opens with the same failed leaf and ancestry.
- Evidence: screenshots of the terminal failed state and the initial historical
  inspection state.
- Effects and cleanup: local failing subprocess and isolated temporary run
  artifacts only. Remove temporary files after evidence capture.
- Permitted substitutes: None.

### AT-004: Navigate while a live UI form remains pending

- Classification: Required.
- Covers: live UI inside selected detail, tree navigation during a pending UI
  step, Enter drill ownership, UI scroll, jump-to-live, form-key ownership, and
  recorded outcome.
- Actor and surface: an agent acting as a user in the built live run-view TUI.
- Setup: run an isolated deterministic workflow with a nested `mode: ui` step,
  overflowing body text, at least one input, multiple actions, and another
  selectable container in the same visible scope.
- Steps:
  1. Observe the active UI leaf under `Current form` with the tree and breadcrumb
     still visible.
  2. Use Up or Down to select another row.
  3. Drill into another non-UI container with Enter, confirm `d` does not drill,
     then drill out with Escape.
  4. Return to the UI leaf with `l`.
  5. Scroll its body with `j` and `k`.
  6. Operate the input and action with Left/Right, Tab, Shift-Tab, and Enter.
- Expected:
  - navigating or drilling away leaves the UI request unresolved;
  - ordinary selected detail replaces the form while another row is selected;
  - `l` returns to root scope and selects the UI leaf without resolving or
    drilling into it;
  - `j` and `k` scroll the selected UI content without changing tree selection;
  - applicable form keys are routed only while the UI leaf is selected;
  - Enter drills a selected non-UI container, `d` is not a drill shortcut, and
    inapplicable keys are not consumed by the form;
  - run-view quit and navigation keys remain owned by the chrome; and
  - the chosen action resolves the step and its recorded result appears under
    `Current outcome`.
- Evidence: screenshots showing the embedded form, selection away while the
  form remains pending, the restored form after `l`, and the recorded outcome.
- Effects and cleanup: isolated local workflow and run artifacts only. Remove
  them after evidence capture.
- Permitted substitutes: None.

## Human-Only Testing

None.

The visual and interactive judgments in this change can be exercised by an
agent using the built product in an ANSI-capable terminal and recording
meaningful final-frame screenshots. No personal credentials, subjective product
approval, physical device, irreversible action, or inaccessible authority
requires separate human-only testing.

## Coverage Map

| Requirement or journey | INT | E2E | AT | HT |
| --- | --- | --- | --- | --- |
| Durable historical reconstruction, missing-workflow recovery, and failed-run initialization | INT-001 | — | AT-001, AT-003 | — |
| Previous execution ordering, skipped reasons, output-source selection, and visual-row bounds | INT-001 | — | AT-001, AT-003 | — |
| Per-type selected detail, compact metadata, latest attempts, metrics, nested resume, and lazy output | INT-001 | — | AT-001 | — |
| Saved-run version and unversioned breadcrumb presentation | INT-001 | — | AT-001 | — |
| Selectable flattened tree, manual drill, and responsive pane layout | — | — | AT-001, AT-002 | — |
| Live dual-focus windowing and independent tree viewport ownership | INT-002 | — | AT-002 | — |
| Live output attribution, path escaping, ANSI handling, first-byte latency, paused exploration, and accumulated output | INT-002 | — | AT-002 | — |
| Active-step follow, detail-tail follow, `t`, `l`, and no automatic drilling | INT-002 | — | AT-002 | — |
| Live and historical failure select the failed leaf at root scope | INT-001, INT-002 | — | AT-003 | — |
| Historical agent-call reconstruction, filtering, and resume | INT-001 | — | AT-001 | — |
| Live agent-call selection, streaming, and independent attribution | INT-002 | — | AT-002 | — |
| Live UI remains pending during navigation and routes form keys only when selected | INT-002 | — | AT-004 | — |
