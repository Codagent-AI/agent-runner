# agent-runner

## 0.5.0

### Minor Changes

- [#196](https://github.com/Codagent-AI/agent-runner/pull/196) `core:finalize-pr` now waits for CI in scripted workflow steps instead of having the lead agent session poll for it.
- [#199](https://github.com/Codagent-AI/agent-runner/pull/199) `core:verify-change` replaces its `/simplify` pass with a single, narrower maintainability pass that targets duplication and wrong-layer logic, preserves behavior, and records anything needing a decision as an assumption.
- [#209](https://github.com/Codagent-AI/agent-runner/pull/209) Adds the experimental `openspec:orchestrated-implement-change` workflow, which implements a change directly from its proposal, specs, design, and test plan with the lead agent orchestrating, skipping the task breakdown.
- [#213](https://github.com/Codagent-AI/agent-runner/pull/213) Agent steps whose CLI crashes, including steps inside sub-workflows, are now reported as infrastructure failures rather than ordinary step failures.
- [#214](https://github.com/Codagent-AI/agent-runner/pull/214) Adds external-user mode (`--external-user <dir>`, `--external-user-timeout`), so interactive Claude and Codex steps can run without a terminal while an external program supplies user turns through files.
- [#225](https://github.com/Codagent-AI/agent-runner/pull/225) Workflow steps can set `autonomous_backend` to override the user's autonomous backend setting for that step.
- [#226](https://github.com/Codagent-AI/agent-runner/pull/226) Agent-call MCP tools gain per-call deadlines, follow-ups addressed by call ID, structured result details, and more informative progress updates.
- [#241](https://github.com/Codagent-AI/agent-runner/pull/241) Usage and cost are now collected for Claude steps that run on the autonomous-interactive backend.
- [#251](https://github.com/Codagent-AI/agent-runner/pull/251) The Claude adapter sets an MCP tool timeout for the agent-call server so long-running agent calls are not cut off by Claude's default timeout.
- [#254](https://github.com/Codagent-AI/agent-runner/pull/254) Adds the core `plan-and-implement-change` workflow, which plans tasks and implements a change in a single run with one run ID, resume target, and `run-metrics.json`.
- [#257](https://github.com/Codagent-AI/agent-runner/pull/257) Removes the task-compliance step from the built-in non-factory workflows.
- [#262](https://github.com/Codagent-AI/agent-runner/pull/262) The orchestrated implementation workflow's lead now delegates each slice to the implementor through `call_agent` instead of its built-in sub-agent tool.
- [#266](https://github.com/Codagent-AI/agent-runner/pull/266) Adds `core:audited-implement-change`, a candidate workflow where one implementor session owns the whole change and an independent crosscheck reviewer verifies it delivered the approved definition.
- [#275](https://github.com/Codagent-AI/agent-runner/pull/275) The audited implementation workflow requires recorded outcome evidence before its audit and stops acceptance rounds that make no progress.
- [#281](https://github.com/Codagent-AI/agent-runner/pull/281) Run metrics now record each agent session's context size per step.
- [#286](https://github.com/Codagent-AI/agent-runner/pull/286) Run metrics and the run view now show an estimated API-equivalent cost for Codex steps.
- [#290](https://github.com/Codagent-AI/agent-runner/pull/290) `audit.log` step-end events now record each step's output writes and repository patch, backed by a per-run `step-evidence/` store, so the development audit judge can assess what each step produced.
- [#296](https://github.com/Codagent-AI/agent-runner/pull/296) Built-in workflows support an `openspec` directory that is a symlink to another repository.
- [#297](https://github.com/Codagent-AI/agent-runner/pull/297) `core:verify-change` callers can now set the scope of its acceptance-fix step.
- [#302](https://github.com/Codagent-AI/agent-runner/pull/302) `openspec:change` and `spec-driven:change` v3.0 become the defaults: they drop the separate task-planning step and implement through `core:implement-change` v2.0, the renamed audited implementation workflow.
- [#308](https://github.com/Codagent-AI/agent-runner/pull/308) In change v3, the implementor writes the verification checklist alone, replacing the lead's checklist and the crosscheck merge.

### Patch Changes

- [#193](https://github.com/Codagent-AI/agent-runner/pull/193) The development audit, available only in dev builds, records its judge's token usage and cost.
- [#198](https://github.com/Codagent-AI/agent-runner/pull/198) Validator review usage is captured in run metrics again after agent-validator changed its capabilities reply.
- [#200](https://github.com/Codagent-AI/agent-runner/pull/200) Temporarily disables the automatic post-run development audit.
- [#204](https://github.com/Codagent-AI/agent-runner/pull/204) Sandbox sync-lock tests no longer pick up the launching user's Git configuration.
- [#205](https://github.com/Codagent-AI/agent-runner/pull/205) Claude subagent usage collection follows a symlinked `~/.claude/projects` store instead of failing to read it.
- [#210](https://github.com/Codagent-AI/agent-runner/pull/210) Implementor steps no longer run agent-validator themselves, leaving validation and its metrics to the workflow's own validator steps.
- [#217](https://github.com/Codagent-AI/agent-runner/pull/217) CI pins factory routing to agent-factory 2445e6d, so new Bugs land in Backlog like other issue types.
- [#220](https://github.com/Codagent-AI/agent-runner/pull/220) Interrupted test runs no longer leave copies of the testscript binary behind in the temp directory.
- [#229](https://github.com/Codagent-AI/agent-runner/pull/229) CI installs the latest Go 1.26 patch so govulncheck stops failing on an already fixed vulnerability.
- [#232](https://github.com/Codagent-AI/agent-runner/pull/232) Claude steps on the autonomous-interactive backend in yolo mode now run with Claude's auto permission mode.
- [#237](https://github.com/Codagent-AI/agent-runner/pull/237) Copilot session discovery tolerates filesystems with coarse modification times.
- [#239](https://github.com/Codagent-AI/agent-runner/pull/239) Archives OpenSpec changes that had been left unarchived on main.
- [#242](https://github.com/Codagent-AI/agent-runner/pull/242) Agent calls wait for the child session probe to finish before completing, so the child's session is reliably recorded.
- [#250](https://github.com/Codagent-AI/agent-runner/pull/250) Copilot, Codex, and OpenCode session discovery honor called-child session exclusions, so a parent step no longer claims its child agent call's session.
- [#252](https://github.com/Codagent-AI/agent-runner/pull/252) After escaping from a finished run, the run list relaunches in the run's original working directory.
- [#253](https://github.com/Codagent-AI/agent-runner/pull/253) In the switcher, `q` is routed to the active view instead of always quitting the app.
- [#258](https://github.com/Codagent-AI/agent-runner/pull/258) CI-wait classification tests get load-independent timing headroom so they stop flaking.
- [#264](https://github.com/Codagent-AI/agent-runner/pull/264) Sandbox settings no longer carry over the host's Claude hooks.
- [#276](https://github.com/Codagent-AI/agent-runner/pull/276) The audited workflow's checklist must cite automated checks and a fixed plan, and its checks reject acceptance or human-only tests cited as checks.
- [#277](https://github.com/Codagent-AI/agent-runner/pull/277) The audited workflow keeps checklist checks fast and reruns only the evidence that needs refreshing.
- [#282](https://github.com/Codagent-AI/agent-runner/pull/282) The audited workflow records per-row evidence and the implementor's decisions, so testing claims are backed by output files.
- [#291](https://github.com/Codagent-AI/agent-runner/pull/291) Audit Git numstat counts binary files instead of discarding the step's evidence.
- [#294](https://github.com/Codagent-AI/agent-runner/pull/294) Value audit Git attribution includes commits from every retried attempt of a step, not just the first.
- [#300](https://github.com/Codagent-AI/agent-runner/pull/300) The value audit no longer blanks judge notes that use slashes in ordinary prose.
- [#301](https://github.com/Codagent-AI/agent-runner/pull/301) The value audit records each step's credit role and the outcomes steps share, so finder and fixer are no longer both rated unique.
- [#303](https://github.com/Codagent-AI/agent-runner/pull/303) Simple-change workflows present agent-call findings as the lead's plain-language recommendation instead of relaying the child's raw findings.
- [#305](https://github.com/Codagent-AI/agent-runner/pull/305) Resuming past a completed sub-workflow step starts the next sub-workflow fresh instead of re-entering the finished one.
- [#307](https://github.com/Codagent-AI/agent-runner/pull/307) Agent Runner's private Codex homes share thread state with the real Codex home, so Codex sessions resume across them.

## 0.4.0

Agent Runner 0.4.0 ships the v2 change workflows, experimental interactive intake, exploratory acceptance, and automatic failure recovery, plus a long run of reliability fixes found in real workflow runs.

### Highlights

- **v2 change workflows.** Full and simple workflows for OpenSpec and spec-driven changes run proposal, specs, design, test plan, tasks, implementation, acceptance, archive, and PR finalization, with dedicated Lead, Crosscheck, Implementor, and Tester roles. Spec-driven keeps its artifacts outside the repository. ([#58](https://github.com/Codagent-AI/agent-runner/pull/58), [#155](https://github.com/Codagent-AI/agent-runner/pull/155))
- **Interactive intake (experimental).** `agent-runner -i` opens a conversation with an agent that clarifies what you want, then routes it into the right workflow with that context carried along. ([#59](https://github.com/Codagent-AI/agent-runner/pull/59), [#58](https://github.com/Codagent-AI/agent-runner/pull/58))
- **Exploratory acceptance.** Acceptance reads the change to decide where to look and probes it like a user would, instead of replaying a checklist written before the code existed. Acceptance and flow-testing steps get their own scratch folder. ([#82](https://github.com/Codagent-AI/agent-runner/pull/82), [#183](https://github.com/Codagent-AI/agent-runner/pull/183))
- **Automatic failure recovery.** When a deterministic check fails, such as an archive commit rejected by a hook or a task left uncommitted, Agent Runner runs a bounded repair cycle with an agent, re-verifies, and shows the repair attempts and failure evidence in the run view. ([#114](https://github.com/Codagent-AI/agent-runner/pull/114))
- **Role-based setup.** Setup recommends Lead, Crosscheck, Implementor, and Tester agents from your installed CLIs, favoring model-family diversity, with accept-all or per-role customization. `planner` and `reviewer` still work as deprecated aliases. ([#57](https://github.com/Codagent-AI/agent-runner/pull/57))
- **Redesigned run view.** New navigation and step detail pane, pull-request links in the breadcrumb, and lower CPU use. ([#61](https://github.com/Codagent-AI/agent-runner/pull/61), [#58](https://github.com/Codagent-AI/agent-runner/pull/58))

### Workflows

- `finalize-pr` keeps one lead session across its CI fix loop, re-verifies CI after the last fix, treats an incomplete review-bot review as a warning, and takes a configurable `ci_fix_cycles` budget. ([#113](https://github.com/Codagent-AI/agent-runner/pull/113), [#122](https://github.com/Codagent-AI/agent-runner/pull/122), [#130](https://github.com/Codagent-AI/agent-runner/pull/130), [#89](https://github.com/Codagent-AI/agent-runner/pull/89))
- `verify-change` stops before opening a PR when the validator stays red, records the acceptance-round validator result, and refreshes an existing draft PR's body while leaving hand-written bodies alone. ([#156](https://github.com/Codagent-AI/agent-runner/pull/156), [#158](https://github.com/Codagent-AI/agent-runner/pull/158), [#171](https://github.com/Codagent-AI/agent-runner/pull/171), [#176](https://github.com/Codagent-AI/agent-runner/pull/176))
- Defects found by the simplify step are routed into fixes instead of being dropped. ([#182](https://github.com/Codagent-AI/agent-runner/pull/182))
- `verify-task-commit` accepts a task verifiably delivered outside the repository, and each task delivery gets a unique record path. ([#160](https://github.com/Codagent-AI/agent-runner/pull/160), [#170](https://github.com/Codagent-AI/agent-runner/pull/170))
- Built-in workflow prompts no longer mandate the TDD skill. ([#83](https://github.com/Codagent-AI/agent-runner/pull/83))

### Writing workflows

- Project workflows can call built-in sub-workflows with `builtin:` references, and counted loops accept `max_param`. ([#89](https://github.com/Codagent-AI/agent-runner/pull/89))
- Built-in script steps bundle their sibling helper scripts, fixing failures such as an OpenSpec archive that could not find `validate-change-name.sh`. ([#159](https://github.com/Codagent-AI/agent-runner/pull/159), [#164](https://github.com/Codagent-AI/agent-runner/pull/164))
- `skip_if` works on steps inside a group. ([#102](https://github.com/Codagent-AI/agent-runner/pull/102))
- `agent-runner run --session-dir <path>` places a run's session directory where you choose. ([#90](https://github.com/Codagent-AI/agent-runner/pull/90))
- Agents can start, poll, and cancel runner-owned agent calls asynchronously. ([#114](https://github.com/Codagent-AI/agent-runner/pull/114))

### Runs and resume

- Old run directories are cleaned up automatically, configurable with `run_retention` in settings. ([#175](https://github.com/Codagent-AI/agent-runner/pull/175))
- Resume restarts a loop iteration when its loop variable changed, and resuming nested or failed workflows is more robust. ([#133](https://github.com/Codagent-AI/agent-runner/pull/133), [#58](https://github.com/Codagent-AI/agent-runner/pull/58))
- Resumed Claude sessions receive the step prompt as a user message, headless Claude tasks stay in the foreground, and step completion from headless attempts is rejected. ([#149](https://github.com/Codagent-AI/agent-runner/pull/149), [#154](https://github.com/Codagent-AI/agent-runner/pull/154), [#162](https://github.com/Codagent-AI/agent-runner/pull/162))

### Usage and cost tracking

- Run metrics include Claude subagent usage, per-step attribution of cumulative Codex and Claude session usage and cost, uncached input tokens for Codex, and a reason for skipped agent steps that were never invoked. ([#194](https://github.com/Codagent-AI/agent-runner/pull/194), [#143](https://github.com/Codagent-AI/agent-runner/pull/143), [#146](https://github.com/Codagent-AI/agent-runner/pull/146), [#169](https://github.com/Codagent-AI/agent-runner/pull/169), [#145](https://github.com/Codagent-AI/agent-runner/pull/145))
- Validator runs are instrumented and correlated with run metrics. ([#114](https://github.com/Codagent-AI/agent-runner/pull/114))
- Per-step changed paths and git attribution are accurate: preexisting dirty or unchanged paths are excluded and committed renames are handled. ([#131](https://github.com/Codagent-AI/agent-runner/pull/131), [#132](https://github.com/Codagent-AI/agent-runner/pull/132), [#134](https://github.com/Codagent-AI/agent-runner/pull/134), [#135](https://github.com/Codagent-AI/agent-runner/pull/135))

### Internal and development

- Development-build-only automatic run audits, with sandboxing and hardening. Not included in release builds. ([#63](https://github.com/Codagent-AI/agent-runner/pull/63), [#110](https://github.com/Codagent-AI/agent-runner/pull/110), [#127](https://github.com/Codagent-AI/agent-runner/pull/127), [#129](https://github.com/Codagent-AI/agent-runner/pull/129), [#141](https://github.com/Codagent-AI/agent-runner/pull/141), [#144](https://github.com/Codagent-AI/agent-runner/pull/144), [#146](https://github.com/Codagent-AI/agent-runner/pull/146), [#147](https://github.com/Codagent-AI/agent-runner/pull/147), [#152](https://github.com/Codagent-AI/agent-runner/pull/152), [#180](https://github.com/Codagent-AI/agent-runner/pull/180), [#184](https://github.com/Codagent-AI/agent-runner/pull/184))
- Agent-factory routing and fix-workflow support. ([#66](https://github.com/Codagent-AI/agent-runner/pull/66), [#67](https://github.com/Codagent-AI/agent-runner/pull/67), [#89](https://github.com/Codagent-AI/agent-runner/pull/89), [#112](https://github.com/Codagent-AI/agent-runner/pull/112))
- Development and sandbox images, hermetic tests, and smoke-test cleanup. ([#98](https://github.com/Codagent-AI/agent-runner/pull/98), [#117](https://github.com/Codagent-AI/agent-runner/pull/117), [#167](https://github.com/Codagent-AI/agent-runner/pull/167), [#173](https://github.com/Codagent-AI/agent-runner/pull/173))

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
