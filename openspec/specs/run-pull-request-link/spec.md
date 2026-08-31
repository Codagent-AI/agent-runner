# run-pull-request-link Specification

## Purpose
TBD - created by archiving change add-run-view-pr-link. Update Purpose after archive.
## Requirements
### Requirement: Reserved capture records a run's pull request URL

The captured-variable name `pr_url` SHALL be reserved. Whenever a step's capture binds `pr_url` to a string value that is non-empty after trimming surrounding whitespace, a `pull_request_recorded` audit event SHALL be emitted at the point of capture, carrying the trimmed URL in a `url` field of the event data. While a repository is active, the recording SHALL be associated with that repository; otherwise it SHALL remain a run-level recording.

The value SHALL be trimmed of surrounding whitespace before it is compared, recorded, or emitted, so that a captured command output with a trailing newline yields a usable URL.

Recording SHALL apply at any nesting depth. A capture made inside a sub-workflow, loop iteration, group, or repository container SHALL be recorded for its run and active repository. Emission SHALL NOT depend on the captured value propagating to the root execution context, because captured variables do not propagate from a sub-workflow to its parent context.

Recording SHALL be tolerant of absent and unusable values. A step that produces no capture, an empty or whitespace-only value, or a capture bound to a non-string kind such as a list or map SHALL emit no event and SHALL NOT clear a previously recorded URL. Recording SHALL never fail a step or a run: it is an observation of the run, not a step outcome.

The most recently recorded value within the same scope SHALL win. A repository-scoped recording SHALL replace only the current URL for that repository and SHALL NOT replace another repository's URL. A workspace-scoped recording SHALL replace only the run-level URL. Within a single run session the event SHALL be emitted only when the trimmed value differs from the last value emitted for that scope, so a value re-bound unchanged SHALL NOT emit again. A resumed session MAY re-emit each restored URL once; because the most recent value within each scope wins, a repeated identical event SHALL NOT change what the run view displays.

The reserved name SHALL behave as an ordinary captured variable in every other respect: it remains interpolable as `{{pr_url}}` in its available execution context, is written to the run's state file with the other captured variables, and is restored on resume.

#### Scenario: Shell capture records the URL
- **WHEN** a shell step with `capture: pr_url` completes and its captured stdout is `https://github.com/Codagent-AI/agent-runner/pull/62`
- **THEN** the run's audit stream contains a `pull_request_recorded` event whose URL is `https://github.com/Codagent-AI/agent-runner/pull/62`

#### Scenario: Capture inside a sub-workflow records against the run
- **WHEN** a step inside a sub-workflow captures `pr_url`, as `core/implement-change-v1.0.yaml` does when composed by `openspec/change-v2.0.yaml`
- **THEN** the event is emitted for the run and the run view shows the link, even though the captured value never reaches the root execution context

#### Scenario: Capture inside a loop iteration records against the run
- **WHEN** a step inside a loop iteration nested in a sub-workflow captures `pr_url`
- **THEN** the event is emitted for the run in the step's workspace or active-repository scope

#### Scenario: Two repositories record pull requests
- **WHEN** backend and frontend each capture a different `pr_url` while their respective repository is active
- **THEN** the run retains one current pull-request URL for backend and one for frontend

#### Scenario: Repository URL changes
- **WHEN** backend captures a new `pr_url` after backend and frontend already recorded URLs
- **THEN** backend's current URL changes while frontend's current URL remains unchanged

#### Scenario: Trailing newline is trimmed
- **WHEN** a shell step captures `pr_url` from a command whose stdout is `https://github.com/Codagent-AI/agent-runner/pull/62\n`
- **THEN** the emitted event's `url` is the URL with no trailing newline, and the breadcrumb's hyperlink target contains no newline

#### Scenario: Non-string capture is ignored
- **WHEN** a step binds `pr_url` to a list or map capture kind
- **THEN** no event is emitted, the step does not fail, and any previously recorded URL in that scope is retained

#### Scenario: Carried-forward capture does not re-emit
- **WHEN** further steps run in the same scope and session after `pr_url` was recorded, carrying the same captured value forward
- **THEN** no second `pull_request_recorded` event is emitted and the recorded URL is unchanged

#### Scenario: Changed URL replaces the recording
- **WHEN** a later step captures `pr_url` with a different non-empty URL in the same scope
- **THEN** a new `pull_request_recorded` event is emitted with the new URL and the run view shows the new URL for that scope

#### Scenario: Empty capture retains the prior URL
- **WHEN** a step captures `pr_url` as an empty or whitespace-only value after a URL was already recorded in that scope
- **THEN** no event is emitted and the previously recorded URL remains current

#### Scenario: No capture records nothing
- **WHEN** a run completes without any step capturing `pr_url`
- **THEN** the run has no recorded pull request URL and its audit stream contains no `pull_request_recorded` event

#### Scenario: Recording survives resume
- **WHEN** a run that recorded repository pull-request URLs is interrupted and resumed
- **THEN** the resumed run still has every repository's recorded URL and the run view still shows them, whether or not the resumed session re-emits the events

### Requirement: Run view links the recorded pull request in the breadcrumb

When a run has recorded pull-request URLs, the run-view breadcrumb SHALL include a pull-request segment after the run status, rendered in the same dim style and separated by the same `·` separator as the existing recorded-version and profile-set segments. A multi-repository run SHALL show every current repository pull request in persisted affected-repository order, separated by `, `. A run-level URL, when present, SHALL precede repository URLs. When a run has no recorded URL, the breadcrumb SHALL be unchanged from its current form.

Each displayed pull request SHALL be an independent OSC 8 terminal hyperlink whose target is its complete recorded URL and whose visible text is a short label. The label SHALL be `PR #<number>` when the URL's path has the form `/<owner>/<repo>/pull/<number>`, and SHALL be `PR` for any other URL, so a non-GitHub or unparseable URL still renders and still links rather than showing a malformed number. The label is derived from the path shape alone, so a self-hosted GitHub Enterprise pull request is numbered like a `github.com` one.

Whether a URL may be linked and how it is labelled SHALL be independent decisions. A recorded URL SHALL be linked when it is safe to embed as a hyperlink target — that is, when it contains no control characters, uses the `https` scheme, has a non-empty host, and carries no userinfo component. Host and path SHALL NOT affect linkability. A recorded URL failing any safety condition SHALL render the plain `PR` label with no OSC 8 escape sequence at all, so a hostile captured value can neither break out of the escape sequence nor become clickable.

The run view SHALL render the segment for every entry mode that displays a run, including a live run in progress, `--inspect`, and entry from the run list. During a live run each label SHALL appear as soon as its URL is recorded, without the user re-entering the run view. Because the metrics summary screen renders the same chrome, the complete segment SHALL be present there as well, at every manual drill depth.

Width measurement for chrome layout SHALL count only visible labels and separators, not the OSC 8 escape sequences, so the logo and rule stay aligned. A terminal that does not support OSC 8 SHALL display the plain label text with no stray escape characters.

#### Scenario: Recorded pull request shown on a completed run
- **WHEN** a completed run with recorded URL `https://github.com/Codagent-AI/agent-runner/pull/62` is opened through `--inspect` or from the run list
- **THEN** the breadcrumb includes a dim `· PR #62` segment after the run status, hyperlinked to the full URL

#### Scenario: Multiple repository pull requests are comma separated
- **WHEN** backend records pull request 62 and frontend records pull request 17 in that affected-repository order
- **THEN** the workspace breadcrumb includes `· PR #62, PR #17`, with each label independently linked to its repository's complete URL

#### Scenario: Repository completion order does not reorder links
- **WHEN** recorded repository events are replayed in an order different from the persisted affected-repository order
- **THEN** the breadcrumb orders repository pull requests by persisted affected-repository order

#### Scenario: Segment appears mid-run
- **WHEN** a live run records another repository pull-request URL while the run view is open
- **THEN** the breadcrumb adds its label in repository order without the user leaving or re-entering the run view

#### Scenario: No recorded pull request shows no segment
- **WHEN** the run view displays a run with no recorded pull request URL
- **THEN** the breadcrumb includes no pull-request segment

#### Scenario: Segment survives drill-in
- **WHEN** the user drills into a sub-workflow, loop, iteration, group, repository, or agent parent while viewing a run with recorded pull requests
- **THEN** the breadcrumb retains the complete comma-separated pull-request segment while appending the entered container

#### Scenario: Segment present on the summary screen
- **WHEN** the user presses `s` to open the metrics summary for a run with recorded pull requests
- **THEN** the summary screen's breadcrumb shows the same comma-separated pull-request segment

#### Scenario: Non-GitHub URL still links
- **WHEN** a run records `https://gitlab.com/example/project/-/merge_requests/42`
- **THEN** the breadcrumb shows a `PR` label hyperlinked to that complete URL

#### Scenario: Enterprise pull request keeps its number
- **WHEN** a run records `https://github.example.com/team/repo/pull/17`
- **THEN** the breadcrumb shows a `PR #17` label hyperlinked to that complete URL

#### Scenario: Query and fragment are preserved in the link
- **WHEN** a run records a pull-request URL carrying a query string or fragment
- **THEN** the breadcrumb links to the recorded URL verbatim, including the query and fragment

#### Scenario: Unsafe URL renders unlinked
- **WHEN** a run records a URL that contains control characters, uses a scheme other than `https`, or carries a userinfo component
- **THEN** its position in the comma-separated segment shows a plain `PR` label containing no OSC 8 escape sequence

#### Scenario: Chrome alignment unaffected by escapes
- **WHEN** the breadcrumb includes multiple hyperlinked pull-request labels
- **THEN** the chrome logo and rule are positioned using visible label and separator width only

#### Scenario: Terminal without hyperlink support
- **WHEN** the run view renders pull-request labels in a terminal that does not support OSC 8
- **THEN** every label is displayed and no escape characters appear in the breadcrumb

### Requirement: Built-in pull-request workflows record their URL

The built-in workflows that open or update a pull request SHALL record its URL through the reserved `pr_url` capture, so runs of every shipped change lineage show the link for the finalized workspace repository and each finalized implementation repository.

`core/implement-change-v1.0.yaml` SHALL record the URL from its existing draft-PR verification step once per active repository. Adding repository association SHALL NOT weaken its existing validation: it SHALL continue to fail the repository execution unless exactly one open, draft pull request exists for the current branch whose head matches local `HEAD`. Recording occurs only once those checks have passed.

`core/finalize-pr-v1.0.yaml` SHALL record the URL after its push step once per active repository, since that workflow can open the pull request when run standalone. That step SHALL be best-effort: when `gh` is unavailable, unauthenticated, or finds no open pull request for the current branch, it SHALL record nothing and SHALL NOT fail the run.

A workflow that opens no pull request SHALL record nothing and SHALL display no label for that scope.

#### Scenario: OpenSpec change workflow shows the link
- **WHEN** a multi-repository run of `openspec/change-v2.0.yaml` finalizes the workspace pull request and a pull request for each affected implementation repository
- **THEN** the run-view breadcrumb shows the workspace pull request first followed by every implementation pull request in affected-repository order

#### Scenario: Spec-driven change workflow shows every link
- **WHEN** a multi-repository spec-driven change opens a draft pull request for each affected repository
- **THEN** the run-view breadcrumb shows every repository's pull-request label in affected-repository order

#### Scenario: Simple change records repository pull requests
- **WHEN** a `simple-change` run finalizes multiple affected repositories
- **THEN** it records and displays one pull-request link for each repository

#### Scenario: Traditional simple change now finalizes its pull request
- **WHEN** `simple-change` runs in a project with no repository declarations
- **THEN** it finalizes and records the implicit repository's pull request while preserving the existing flattened presentation

#### Scenario: Standalone finalize records the URL
- **WHEN** `core/finalize-pr-v1.0.yaml` runs standalone for one repository and its push step opens a pull request
- **THEN** the URL is recorded and the breadcrumb shows the segment

#### Scenario: Missing gh does not fail the standalone finalize run
- **WHEN** the best-effort recording step in `core/finalize-pr-v1.0.yaml` runs where `gh` is not installed or not authenticated
- **THEN** the step does not fail the run, nothing is recorded for that repository, and no label is added for it

#### Scenario: Draft-PR verification still fails on a missing pull request
- **WHEN** `verify-draft-pr` in `core/implement-change-v1.0.yaml` finds no open draft pull request matching local `HEAD` for the active repository
- **THEN** that repository execution fails exactly as it does today, and adding URL recording has not made it tolerant

#### Scenario: Workflow without a pull request shows nothing
- **WHEN** a run of a workflow that opens no pull request completes
- **THEN** the breadcrumb shows no pull-request segment

