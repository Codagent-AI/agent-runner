# agent-runner

## 0.4.0

Agent Runner 0.4.0 ships the v2 change workflows, interactive intake, exploratory acceptance, and automatic failure recovery, plus a long run of reliability fixes found in real workflow runs.

### Highlights

- **v2 change workflows.** Full and simple workflows for OpenSpec and spec-driven changes run proposal, specs, design, test plan, tasks, implementation, acceptance, archive, and PR finalization, with dedicated Lead, Crosscheck, Implementor, and Tester roles. Spec-driven keeps its artifacts outside the repository. ([#58](https://github.com/Codagent-AI/agent-runner/pull/58), [#155](https://github.com/Codagent-AI/agent-runner/pull/155))
- **Interactive intake.** `agent-runner -i` opens a conversation with an agent that clarifies what you want, then routes it into the right workflow with that context carried along. ([#59](https://github.com/Codagent-AI/agent-runner/pull/59), [#58](https://github.com/Codagent-AI/agent-runner/pull/58))
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
