## Why

Agent Runner requires the user to choose a workflow before any agent can help them make that choice. Running `agent-runner` with no arguments opens the list TUI on its New tab, which browses workflows and collects their declared parameters. That form assumes the user already knows whether the work ahead is a full spec-driven change, a simple change, an onboarding run, or not a code change at all.

The assumption breaks whenever the right answer depends on facts the user has not gathered yet:

- Choosing between `spec-driven:change` and `spec-driven:simple-change` depends on how large the change turns out to be, which is precisely what the user is trying to determine.
- Some sessions should end with no workflow at all, because reading the code shows the bug is elsewhere or the feature already exists.
- Naming a change requires knowing what the change is, and `change_name` is a required parameter collected before any agent has spoken.

The ways through this today are to guess a workflow and hope, or to explore in a separate agent session and then re-explain the conclusions to the workflow's first agent. The second cost is the sharper one. Exploration produces repository findings, constraints, non-goals, and decisions, and none of it crosses the boundary into the run. The change lifecycle makes this concrete: the first agent prompt in `core:define-change` instructs the agent to ask the user what the change is about and explicitly not to guess, so every run begins from a cold start even when the user has already done the thinking.

This is the default entry point, so it sits in front of everyone starting work in an Agent Runner project. It matters now because the spec-driven change workflow has just landed with a define → plan → implement → accept → finalize lifecycle that is expensive to enter incorrectly, and the current entry point offers the user no help in avoiding that mistake.

## What Changes

- Add a built-in interactive **intake workflow** containing a single interactive agent step. The agent explores the problem with the user: inspecting the repository, researching, brainstorming, clarifying scope and non-goals, and recommending whether implementation is warranted and under which workflow. The human decides.
- Add **"Plan with an agent"** as a highlighted first entry on the New tab, above the existing workflow browser. Selecting it starts the intake workflow. The manual browser and parameter form remain unchanged and fully usable for users who already know what they want.
- Add **`agent-runner -i`** as a direct entry point that starts intake immediately, skipping the New tab:

  ```
  agent-runner -i
  agent-runner -i --cli codex --model gpt-5.2
  ```

  `--cli` and `--model` are **run-scoped overrides applied to the intake agent only**, resolved with precedence *command override → step declaration → agent profile*. Omitting them falls back to the intake agent's configured profile. They never reach the workflow intake subsequently launches, which resolves its agents normally.
- **Define `-i`'s flag compatibility explicitly.** `-i` is rejected in combination with `--headless`, `--list`, `--resume`, `--inspect`, `--validate`, `--onboarding-from`, or any workflow positional argument. `--cli` and `--model` require `-i`. `-C` remains compatible. Intake checks real stdin and stdout TTY status directly rather than through `requireTTY`, which honors the `AGENT_RUNNER_NO_TUI` bypass that `--headless` sets.
- Add **`agent-runner step submit-route`**, a fixed, argument-free control command. Before invoking it, the agent writes a durable Markdown handoff and a typed JSON route request into runner-provided run-owned paths.
- Add a **`submit_route` control message** to the per-run control channel, carrying the same attempt-scoped authentication as `complete_step`. The runner validates workflow resolution through the existing catalog, required and optional parameters, handoff readability and containment, attempt eligibility, and idempotency. Validation failures return **inline, while the intake session is still live**, so the agent can correct the request and retry within the same conversation.
- On acceptance, the runner **seals a snapshot** of the route and handoff into a run-owned sidecar. The sealed route records the **exact resolved workflow source path**, not just the canonical name, so the definition that was validated is the definition that runs. Subsequent reads never re-read the agent-writable originals.
- **Acceptance of `complete_step` freezes the staged route under the same lock**, so a `submit_route` arriving after an accepted completion is rejected. While the step is still active, a later valid submission replaces an earlier one, and identical retries deduplicate by request ID.
- Expose **`{{intake_handoff}}`** as a built-in template variable defined on **every** run: the **contents** of the sealed handoff on an intake launch, and the empty string on every direct or headless launch. Delivering the text rather than a location is what makes the handoff unconditional; a path is consumed only if the agent elects to read it. The inlined text is bounded well below the handoff file's own limit, and an oversized handoff is truncated at a line boundary with a marker naming the full path rather than failing the run. A run resumes with the same value it originally had, so a resumed intake child still sees its sealed handoff.
- Expose **`{{intake_handoff_path}}`** alongside it, carrying the sealed handoff's absolute path under the same always-defined and preserved-across-resume rules, for workflows that need to address the file rather than read the text.
- **Reserve both names** as parameter and capture names. Built-ins otherwise have the lowest interpolation precedence, so a workflow declaring a parameter of either name would silently replace the sealed handoff and break the provenance guarantee the feature rests on.
- Update the first agent prompt in **`core:define-change`** and in `spec-driven:simple-change` to carry `{{intake_handoff}}` directly, replacing an instruction to go read a file. `core:define-change` is shared by both the spec-driven and OpenSpec change flows, so both gain the behavior.
- **Launch the selected workflow as a new top-level run** after the intake run finalizes, passing the sealed route through a private internal launch path. Provenance is **child-owned**: the launched run records its parent intake run ID, its own run ID, and the workflow version it actually ran. The parent records the frozen route and that a launch was attempted, but cannot record the child's run ID, because that ID is generated only after the process has been replaced and the parent's audit log is already closed.
- **Define interruption and recovery.** A staged route persists in the sidecar across interruption, and a resumed intake attempt may replace it. If re-exec fails, the intake process is still alive and reports the failure along with the sealed handoff path. If the process is killed after the route freezes but before the launch, the intake run is already complete and cannot be resumed, so the user restarts intake; the sealed handoff is preserved.
- **Exploration-only is a success path.** If the intake agent completes its step with no accepted route, the intake run finishes cleanly, launches nothing, and preserves any handoff that was written.
- Explicit CLI and headless invocations continue to bypass intake entirely, unchanged.

## Capabilities

### New Capabilities

- `workflow-intake`: The intake entry points (the New tab entry and the `-i` flag with its flag-compatibility rules and run-scoped `--cli`/`--model` overrides), the intake workflow, the exploration-only exit, launching the selected workflow as a fresh top-level run with a fresh agent session, child-owned provenance, interruption and recovery behavior, and the bypass rules for explicit and headless invocation.
- `intake-route-submission`: The `step submit-route` command, its control message and authentication, the route-request format, validation and inline error reporting, the sealed route sidecar and its state transitions, snapshot sealing rules, replacement and retry idempotency, and the freeze-on-completion rule.

### Modified Capabilities

- `step-control-channel`: Add a fourth authenticated message type alongside completion, committed-turn evidence, and agent calls, with its own acceptance state. Completion acceptance changes: it now also freezes the staged route under the same ordering lock before acknowledging.
- `builtin-vars`: Add `{{intake_handoff}}`, carrying the sealed handoff's contents under a bound sized for a prompt, and `{{intake_handoff_path}}`, carrying its location. Both, unlike existing built-ins, are defined unconditionally, including as the empty string, and both are reserved against workflow parameters and captures.
- `new-tab-layout`: Add the "Plan with an agent" entry and its relationship to the existing grouped workflow list.
- `builtin-workflows`: Ship the intake workflow as an embedded built-in.
- `resume-by-session-id`: Preserve a run's original intake provenance across resume, so a resumed intake child restores its sealed handoff path and a resumed intake parent restores its staged route.
- `audit-log-entries`: Record route submission, rejection, acceptance, freeze, and launch-attempt outcomes.
- `workflow-pre-validation`: Recognize both handoff built-ins so prompts referencing them are not reported as undefined variables.

## Technical Approach

The intake experience is an ordinary interactive agent step inside a built-in workflow, so it inherits session management, the control channel, terminal handoff, audit, and run state without new machinery. The decisions below carry most of the structural risk.

**Route submission is file-backed behind a fixed command.** The agent writes its choice to a JSON file at a runner-provided path and then runs a command whose text never varies:

```
env:  AGENT_RUNNER_ROUTE_REQUEST=<run-dir>/route-request.json

      { "workflow": "spec-driven:change",
        "params":   { "change_name": "interactive-workflow-intake" },
        "handoff":  "<run-dir>/intake-handoff.md" }

run:  agent-runner step submit-route
```

The alternative, passing the workflow and parameters as command arguments, conflicts with a deliberate constraint in the CLI adapters. `CompletionCommand.Valid()` requires the pre-approved argv to be exactly `["step", "complete"]`, and its comment states that separating executable from argv "prevents adapters from accepting shell fragments supplied as a single opaque string." Claude turns that descriptor into an exact-string `Bash(...)` grant. A command carrying model-authored values would either lose pre-approval and prompt on every attempt, or require a wildcard grant that is materially broader than what the codebase currently allows itself. Moving the variable data into a validated file keeps the permission grant exact and makes shell quoting irrelevant.

**Route state lives in a dedicated sidecar, not in `RunState`.** This is not a stylistic preference. `writeStepState` constructs a fresh `model.RunState` from scratch after each step and writes it wholesale rather than reading and merging, so a route field written into `state.json` from the control goroutine would be silently obliterated by the next step write. A run-owned sidecar with its own atomic writes and an explicit `staged → frozen` lifecycle gives route state a single owner under the control mutex, and avoids forcing route fields through every state reconstruction and resume path.

**The control handler stages; the process boundary launches.** The workflow executes in a goroutine while the Bubble Tea program owns the terminal, and TUI-originated launches happen only after that program exits, in the top-level switcher. (Explicit CLI runs bypass the switcher entirely and dispatch straight into the run path.) Executing from inside the socket handler would bypass run finalization, audit flushing, and lock release, all of which `finalizeRun` performs. So `submit_route` only validates and stages, the run finalizes normally, and the launch happens at the process boundary.

**The launch carries the exact resolved workflow path.** The existing re-exec passes only a canonical workflow name, and `resolveWorkflowArg` deliberately rejects versioned names so the child selects the latest version itself. Reusing that path would let a project workflow change between acceptance and launch, and would make an audit record of the "accepted version" actively misleading. Discovery already exposes `WorkflowEntry.SourcePath` as the exact selected path, and the downstream run path already consumes a resolved file path, so a private internal launch channel can hand over the sealed path directly. This pins the versioned definition without hashing its contents.

**The handoff travels as a reserved built-in variable.** A declared parameter would leak into every target workflow's public interface and would require editing each workflow before intake could reach it. A built-in keeps the interface clean, but interpolation treats an unresolved variable as a hard error rather than an empty substitution, so defining `{{intake_handoff}}` only on intake launches would break every direct invocation of a workflow that references it. It must be defined on all runs, empty when there is no intake, which is a deliberate departure from the existing built-ins that omit themselves when unset. Because built-ins have the lowest precedence, both names are additionally reserved against parameters and captures so the sealed handoff cannot be shadowed.

**Sealing and freezing close the gap between validation and use.** The agent keeps running after a successful submission, since it still has to complete its step, and could edit the handoff afterward. Sealing means what was validated is what launches. The rules are ordinary robustness, not an adversarial threat model: strict JSON decoding with unknown fields rejected, bounded request and handoff sizes, the opened handle must identify a regular file, containment within the run directory validated, bytes copied from that same opened handle, and the snapshot published atomically. Freezing at completion acceptance closes the converse race, where a submission lands after the runner has already decided what it is launching.

**`--cli` and `--model` are a run-scoped override, not step interpolation.** Making step-level `cli` and `model` interpolable is not viable as a contained change: `ParseWorkflow` validates `cli` against `cli.KnownCLIs()` at load time, before any interpolation runs, so a placeholder would be rejected outright rather than passed through. Supporting it would mean changing loader validation, prevalidation, and probing, which widens the change well beyond intake. Instead the override travels on the execution context and is consulted during agent resolution, above the step declaration and the profile. It is persisted in intake state so it survives resume, and it is not inherited by the launched run.

Workflow resolution, version selection, and parameter validation reuse the existing catalog and discovery paths rather than being reimplemented against a prompt-supplied name, so any workflow the user could launch manually is launchable from intake, and workflows that never reference the handoff simply ignore it.

## Out of Scope

- **A runner-owned confirmation gate.** Agreement reached in the intake conversation is sufficient; no separate proof of human consent, confirmation screen, or pre-filled parameter form is required before launch. This was considered and deliberately rejected as unnecessary ceremony.
- **Multi-round revision after submission.** There is no reject-and-return loop, no route revision state machine beyond replace-while-active, and no new unbounded loop capability in the workflow engine.
- **Crash-safe launch retry.** No preallocated child run ID, launch ledger, or claim protocol. A process killed in the window between route freeze and re-exec requires the user to restart intake. The sealed handoff survives.
- **Content hashing or drift rejection.** The launch pins the exact resolved workflow path, but does not hash its bytes or refuse to run if the file changed underneath it.
- **An adversarial filesystem threat model** for the route request and handoff. The intake agent is not treated as an attacker; sealing addresses malformed and oversized input, not symlink races or fsync-level durability guarantees.
- **A general launch-plan service** shared with manual TUI launches. Existing launch paths are unchanged.
- **Exposing route submission as an MCP tool.** The validation and acceptance logic stays transport-independent so a tool can be added later, but the per-adapter permission and tool-filter surface is not worth paying for a once-per-run operation.
- **General-purpose `--cli` and `--model` overrides** for arbitrary workflow runs. The overrides apply to the intake agent only, and no other workflow or entry point gains them.
- **Workflow rewind**, jumping backward to completed steps, or rerouting from acceptance back to planning or implementation.
- **Redesigning the change lifecycle.** Its prompts gain a handoff reference; its structure does not change.
- **Recursive or dynamic child-workflow orchestration from arbitrary agents.** Route submission is intake-only and terminal.
- **Inferring workflow selection from unstructured model output.** Nothing parses the agent's prose.
- **Preserving the intake transcript** as context for the selected workflow. Only the durable handoff crosses the boundary; the selected workflow's first agent session is fresh.
- **Intake in non-TTY or headless invocations.** These continue to require an explicit workflow, and `-i` is rejected without a real TTY.

## Impact

- A new internal package owns the intake-route sidecar: its schema, atomic writes, `staged → frozen` transitions, and sealed-snapshot publication. Keeping it separate from `internal/stateio`'s `RunState` is what prevents `writeStepState` from clobbering it.
- `internal/control/` gains a fourth message type with its own acceptance state, durable reservation, and retry semantics. Completion acceptance is modified, not merely extended: it must freeze the staged route under the same ordering lock before acknowledging.
- `cmd/agent-runner/` gains the `step submit-route` subcommand, the `-i`, `--cli`, and `--model` flags with their mutual-exclusivity rules and a TTY check that does not honor the `AGENT_RUNNER_NO_TUI` bypass, and a private internal launch path that accepts a sealed workflow source path rather than a canonical name.
- `internal/cli/` needs the new command added to the pre-approved runner commands for interactive adapters. This touches the adapter descriptor and each adapter that materializes command permissions, and is the main reason the command's text must be fixed.
- `internal/exec/` and `internal/model/` gain the run-scoped agent override consulted during agent resolution, with precedence above the step declaration and profile. Loader validation, prevalidation, and probing are untouched by this choice.
- `internal/model/` gains the `intake_handoff` and `intake_handoff_path` built-ins and their reservation against parameters and captures; `internal/prevalidate/` must recognize both, since its built-in set is currently the hardcoded pair `session_dir` and `step_id`; `internal/audit/` records the route lifecycle. `BuiltinVarsForStep` changes shape slightly, since it currently omits empty values and returns nil for an empty map.
- `internal/runner/` records parent provenance and the sealed handoff path in the launched run's state, and restores both on resume. Resume currently copies an explicit field list, so these need deliberate addition.
- `internal/listview/` gains the "Plan with an agent" entry and its selection behavior on the New tab.
- `workflows/` gains the embedded intake workflow. The prompt edit lands in the shared `core:define-change` sub-workflow, so it reaches both the spec-driven and OpenSpec change flows; `spec-driven:simple-change` is edited separately. Because prompts referencing `{{intake_handoff}}` fail to interpolate when the variable is undefined, the built-in must be in place before those prompts ship.
- Sub-workflow and loop execution contexts must propagate both handoff built-ins so nested steps in the launched workflow can reference them. Both context constructors copy run-scoped fields explicitly, so this is a deliberate addition rather than automatic.
- Users see a new entry point, but no existing invocation changes: direct CLI, headless, resume, and manual New-tab launches all behave exactly as they do today, with `{{intake_handoff}}` empty.
- Project-local and user-authored workflows are launchable from intake with no modification, and are unaffected if they never reference the handoff.
