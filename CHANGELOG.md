# agent-runner

## 0.4.0

### Minor Changes

- [#57](https://github.com/Codagent-AI/agent-runner/pull/57) Setup now recommends Lead, Crosscheck, Implementor, and Tester agent roles with accept-all or per-role customization, renaming the planner and reviewer roles while keeping them as deprecated aliases.
- [#58](https://github.com/Codagent-AI/agent-runner/pull/58) Ships the v2 OpenSpec and spec-driven workflows with role-based proposal, test-plan, task, implementation, acceptance, archive, and finalization phases, plus runtime hardening for long nested runs.
- [#59](https://github.com/Codagent-AI/agent-runner/pull/59) Adds interactive workflow intake (`agent-runner -i`), where an agent gathers context about the change before handing off to the chosen workflow.
- [#61](https://github.com/Codagent-AI/agent-runner/pull/61) Redesigns run view navigation and the step detail pane.
- [#82](https://github.com/Codagent-AI/agent-runner/pull/82) Replaces the checklist-driven acceptance step with an exploratory pass that sizes testing to what the change actually touched.
- [#155](https://github.com/Codagent-AI/agent-runner/pull/155) Extracts a reusable `verify-change` workflow from `implement-change` covering validation, draft PR creation, and acceptance.
- [#158](https://github.com/Codagent-AI/agent-runner/pull/158) Records the acceptance-round validator result in `verify-change` so later steps can rely on it.
- [#160](https://github.com/Codagent-AI/agent-runner/pull/160) Lets `verify-task-commit` accept task delivery that was verified to live outside the repository instead of failing it.
- [#175](https://github.com/Codagent-AI/agent-runner/pull/175) Automatically cleans up old run directories under a configurable retention policy.
- [#194](https://github.com/Codagent-AI/agent-runner/pull/194) Includes token usage from Claude subagents in run metrics.

### Patch Changes

- [#63](https://github.com/Codagent-AI/agent-runner/pull/63) Adds development-build-only automatic audits of completed workflow runs (not included in release builds).
- [#66](https://github.com/Codagent-AI/agent-runner/pull/66) Connects repository issues to agent-factory routing.
- [#67](https://github.com/Codagent-AI/agent-runner/pull/67) Fixes issue event parsing in the factory routing deployment.
- [#83](https://github.com/Codagent-AI/agent-runner/pull/83) Removes the mandatory TDD skill instruction from built-in workflow prompts.
- [#89](https://github.com/Codagent-AI/agent-runner/pull/89) Adds `max_param` for counted loops, a `ci_fix_cycles` parameter on `finalize-pr`, and `builtin:` sub-workflow references so project workflows can call built-in ones.
- [#90](https://github.com/Codagent-AI/agent-runner/pull/90) Adds a `--session-dir` flag to `agent-runner run` to place the run's session directory at a chosen path.
- [#98](https://github.com/Codagent-AI/agent-runner/pull/98) Keeps interactive and sandbox tests hermetic when launched from another agent's environment.
- [#102](https://github.com/Codagent-AI/agent-runner/pull/102) Evaluates `skip_if` on steps inside a group.
- [#110](https://github.com/Codagent-AI/agent-runner/pull/110) Excludes gitignored artifacts from audit source snapshots.
- [#112](https://github.com/Codagent-AI/agent-runner/pull/112) Routes issues retyped as Bug after creation to the factory.
- [#113](https://github.com/Codagent-AI/agent-runner/pull/113) Keeps the lead agent's session continuous across the `finalize-pr` CI fix loop.
- [#114](https://github.com/Codagent-AI/agent-runner/pull/114) Adds failure recovery with repair cycles, async agent calls (start, poll, cancel), validator metrics instrumentation, and sandboxed audits.
- [#117](https://github.com/Codagent-AI/agent-runner/pull/117) Makes the development image usable as the Fly sandbox image.
- [#122](https://github.com/Codagent-AI/agent-runner/pull/122) Verifies CI after the last `finalize-pr` fix cycle instead of ending on an unverified push.
- [#127](https://github.com/Codagent-AI/agent-runner/pull/127) Hardens development audit recovery and delivery.
- [#129](https://github.com/Codagent-AI/agent-runner/pull/129) Includes models used by nested agent calls in value observation source models.
- [#130](https://github.com/Codagent-AI/agent-runner/pull/130) Treats an incomplete review-bot review as a warning rather than a failure in `finalize-pr`.
- [#131](https://github.com/Codagent-AI/agent-runner/pull/131) Excludes paths that were already dirty before a step from that step's changed paths.
- [#132](https://github.com/Codagent-AI/agent-runner/pull/132) Limits working-tree changed paths to what the step itself changed.
- [#133](https://github.com/Codagent-AI/agent-runner/pull/133) Restarts a loop iteration on resume when its loop variable changed.
- [#134](https://github.com/Codagent-AI/agent-runner/pull/134) Keeps git checkpoint attribution correct when a step commits renames.
- [#135](https://github.com/Codagent-AI/agent-runner/pull/135) Excludes unchanged preexisting paths from git attribution.
- [#141](https://github.com/Codagent-AI/agent-runner/pull/141) Makes development audits work for factory runs.
- [#143](https://github.com/Codagent-AI/agent-runner/pull/143) Attributes Codex cumulative session usage to the individual step that used it.
- [#144](https://github.com/Codagent-AI/agent-runner/pull/144) Files auto-audit issues with the Bug issue type.
- [#145](https://github.com/Codagent-AI/agent-runner/pull/145) Reports a not-invoked usage reason for skipped agent steps.
- [#146](https://github.com/Codagent-AI/agent-runner/pull/146) Attributes Claude cumulative cost per step and narrows which audit findings are filed.
- [#147](https://github.com/Codagent-AI/agent-runner/pull/147) Gives the Linux audit sandbox a read-only `/dev`.
- [#149](https://github.com/Codagent-AI/agent-runner/pull/149) Delivers the step prompt as a user message when resuming Claude sessions.
- [#152](https://github.com/Codagent-AI/agent-runner/pull/152) Runs the value-audit sandbox as the caller's uid and marks Claude as sandboxed.
- [#154](https://github.com/Codagent-AI/agent-runner/pull/154) Keeps headless Claude tasks in the foreground so they are not left running in the background.
- [#156](https://github.com/Codagent-AI/agent-runner/pull/156) Stops `verify-change` before opening a PR when the validator stays red.
- [#159](https://github.com/Codagent-AI/agent-runner/pull/159) Materializes a built-in script's whole namespace so its helper scripts resolve.
- [#162](https://github.com/Codagent-AI/agent-runner/pull/162) Rejects step completion signals sent from headless attempts.
- [#164](https://github.com/Codagent-AI/agent-runner/pull/164) Bundles sibling scripts with an embedded workflow's script, fixing OpenSpec archive failures from a missing helper.
- [#167](https://github.com/Codagent-AI/agent-runner/pull/167) Releases the smoke test's Docker image and artifacts on exit.
- [#169](https://github.com/Codagent-AI/agent-runner/pull/169) Emits uncached input token counts for Codex attempts.
- [#170](https://github.com/Codagent-AI/agent-runner/pull/170) Gives each task delivery a unique record path.
- [#171](https://github.com/Codagent-AI/agent-runner/pull/171) Regenerates an existing draft pull request's body in `verify-change`.
- [#173](https://github.com/Codagent-AI/agent-runner/pull/173) Pins the dev-audit smoke test's lead agent to a fake Codex.
- [#176](https://github.com/Codagent-AI/agent-runner/pull/176) Updates existing PR bodies through the REST API and preserves bodies without runner markers.
- [#180](https://github.com/Codagent-AI/agent-runner/pull/180) Takes the audit's source outcome from the run end event.
- [#182](https://github.com/Codagent-AI/agent-runner/pull/182) Routes defects found by simplify into fixes instead of dropping them.
- [#183](https://github.com/Codagent-AI/agent-runner/pull/183) Gives acceptance and test-flows steps a runner-managed scratch folder.
- [#184](https://github.com/Codagent-AI/agent-runner/pull/184) Lets the audit judge read the run snapshot and runner source.

## 0.3.0

### Minor Changes

- [#51](https://github.com/Codagent-AI/agent-runner/pull/51) Add the second-generation OpenSpec change workflows with explicit define, plan, implement, acceptance, and finalization phases.
- [#52](https://github.com/Codagent-AI/agent-runner/pull/52) Replace the interactive PTY proxy with direct terminal handoff for more faithful native agent sessions.
- [#54](https://github.com/Codagent-AI/agent-runner/pull/54) Record durable token usage, estimated cost, and timing metrics across workflow and child-agent execution.
- [#55](https://github.com/Codagent-AI/agent-runner/pull/55) Add synchronous Runner-owned agent calls with named sessions, audit evidence, cancellation, and usage accounting.
- [#56](https://github.com/Codagent-AI/agent-runner/pull/56) Add versioned workflow resolution and explicit reusable orchestration for reviews and targeted acceptance verification.

## 0.2.0

### Minor Changes

- [#48](https://github.com/Codagent-AI/agent-runner/pull/48) Added a Playwright-based Docker/devcontainer sandbox substrate for Agent Runner eval work, including browser proof scripts with explicit secret/auth handling and devcontainer shell tooling.

### Patch Changes

- [#47](https://github.com/Codagent-AI/agent-runner/pull/47) Restructured Agent Runner's user documentation into focused docs-site pages with an index and generated CLI reference, replacing the old single user guide.
- [#49](https://github.com/Codagent-AI/agent-runner/pull/49) Fixed the PTY layer dropping mouse input for interactive agent sessions, which broke selection, copy, and scrolling when running Claude Code's fullscreen terminal renderer through Agent Runner.

## 0.1.3

### Patch Changes

- [#42](https://github.com/Codagent-AI/agent-runner/pull/42) Refresh guided onboarding directory checks when users restart from a different project directory, ground the tutor prompt in the immediate implement step, handle symlink aliases safely, and ensure the Homebrew cask upgrades installed dependent formulas before installing.

## 0.1.2

### Patch Changes

- [#41](https://github.com/Codagent-AI/agent-runner/pull/41) Improve failed-run debugging follow-up by launching debug from the original run cwd, resolving failed runs by absolute session directory, showing a visible debug hint in failed run views, including environment details in debug-created issue reports, and hardening the release skill so previous releases and stale branch PRs are not included again.

## 0.1.1

### Minor Changes

- [#38](https://github.com/Codagent-AI/agent-runner/pull/38) Improve the new-workflow tab so workflows are grouped more clearly, hidden/current-directory visibility is easier to understand, and descriptions are more useful while browsing.

### Patch Changes

- [#39](https://github.com/Codagent-AI/agent-runner/pull/39) Add a built-in debug workflow and failure entry points so failed runs can be inspected through state/audit-summary commands and routed toward user remediation or issue filing.

## 0.1.0

### Minor Changes

- Initial public release of Agent Runner, a Go CLI workflow orchestrator for deterministic multi-step AI agent workflows.
- Add built-in onboarding workflows, live run and list TUIs, workflow composition, step execution for agents/shell/scripts/UI, session resume support, and Agent Validator integration.
- Add release automation and Homebrew packaging support for publishing `agent-runner`.

### Patch Changes

- Fix live-run follow behavior while UI steps are active so follow mode does not snap back to the previous process-backed step.
- Treat omitted optional workflow parameters as empty strings during interpolation, allowing shell conditionals such as optional validator context files to render and branch correctly.
- Improve the Docker live-update skill with binary permission checks and robust install target discovery.
