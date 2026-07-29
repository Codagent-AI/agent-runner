## ADDED Requirements

### Requirement: Intake conversation responsibilities

The intake workflow SHALL present the user with an interactive agent session that is able to inspect the repository, research the problem, and recommend a course of action, and that is enabled to submit a route. The agent SHALL recommend; the human SHALL decide, and the agent SHALL NOT commit the user to an implementation path without the user's agreement in the conversation. Agent Runner SHALL supply a run-owned path at which the agent writes the handoff, so that its location is known to the runner whether or not a route is ever submitted.

#### Scenario: Intake presents a capable conversational agent
- **WHEN** an intake run reaches its agent step
- **THEN** the user is placed in an interactive agent session that can inspect the repository and is enabled to submit a route

#### Scenario: Agent recommends and the user decides
- **WHEN** the intake agent identifies a suitable workflow
- **THEN** it presents the recommendation to the user and submits a route only after the user agrees in the conversation

#### Scenario: Handoff location is runner-owned
- **WHEN** the intake agent writes a handoff
- **THEN** it writes to the run-owned path Agent Runner supplied
- **AND** Agent Runner can report that path even when no route was ever submitted

### Requirement: Shipped workflows consume the handoff

The built-in workflows that intake most commonly routes to SHALL read the handoff when one is present. Specifically, the first agent step of the shared change-definition workflow and of the simple-change workflow SHALL instruct the agent to read `{{intake_handoff}}` before asking the user for context when that value is non-empty, and SHALL behave exactly as they do today when it is empty. Exact prompt wording is an implementation detail.

#### Scenario: Change definition reads an intake handoff
- **WHEN** the shared change-definition workflow is launched from intake
- **THEN** its first agent step is instructed to read the handoff before asking the user what the change is about

#### Scenario: Simple change reads an intake handoff
- **WHEN** the simple-change workflow is launched from intake
- **THEN** its first agent step is instructed to read the handoff before asking the user what the change is about

#### Scenario: Direct invocation is unchanged
- **WHEN** either workflow is invoked directly, so the handoff value is empty
- **THEN** its first agent step behaves exactly as it does today, asking the user for context

### Requirement: Intake entry points

Agent Runner SHALL provide two entry points into the built-in intake workflow: a dedicated "Plan with an agent" entry on the New tab, and an `-i` command-line flag that starts intake directly without opening the TUI. Invoking `agent-runner` with no arguments SHALL continue to open the New tab rather than starting intake automatically.

#### Scenario: New tab entry starts intake
- **WHEN** the user selects the "Plan with an agent" entry on the New tab
- **THEN** Agent Runner starts a run of the built-in intake workflow

#### Scenario: `-i` starts intake directly
- **WHEN** the user runs `agent-runner -i` in an interactive terminal
- **THEN** Agent Runner starts a run of the built-in intake workflow without first rendering the New tab

#### Scenario: No-argument invocation is unchanged
- **WHEN** the user runs `agent-runner` with no arguments in an interactive terminal
- **THEN** the New tab opens as it does today, and no intake run starts until the user selects the intake entry

### Requirement: Intake invocation constraints

`-i` SHALL be rejected when combined with `--headless`, `--list`, `--resume`, `--inspect`, `--validate`, `--onboarding-from`, or a workflow positional argument. `--cli` and `--model` SHALL be rejected unless `-i` is also present. `-C` SHALL remain compatible with `-i`.

Intake SHALL require that both stdin and stdout are real interactive terminals, and SHALL NOT honor the `AGENT_RUNNER_NO_TUI` bypass that other TTY checks accept. This requirement attaches to intake itself rather than to the `-i` flag, so naming the intake workflow explicitly under `--headless` SHALL be rejected for the same reason.

#### Scenario: `-i` with `--headless` is rejected
- **WHEN** the user runs `agent-runner --headless -i`
- **THEN** Agent Runner exits nonzero with an error stating the flags are mutually exclusive
- **AND** no intake run is created

#### Scenario: `-i` with a workflow argument is rejected
- **WHEN** the user runs `agent-runner -i spec-driven:change`
- **THEN** Agent Runner exits nonzero with an error stating that `-i` cannot be combined with an explicit workflow
- **AND** neither intake nor the named workflow starts

#### Scenario: Override flags require `-i`
- **WHEN** the user runs `agent-runner --cli codex spec-driven:change`
- **THEN** Agent Runner exits nonzero with an error stating that `--cli` and `--model` require `-i`

#### Scenario: Intake requires a real terminal
- **WHEN** the user runs `agent-runner -i` with `AGENT_RUNNER_NO_TUI=1` set and stdout redirected to a file
- **THEN** Agent Runner exits nonzero with an error stating an interactive terminal is required
- **AND** the `AGENT_RUNNER_NO_TUI` value does not suppress the check

#### Scenario: Directory change remains compatible
- **WHEN** the user runs `agent-runner -C /path/to/project -i` in an interactive terminal
- **THEN** Agent Runner changes to the named directory and starts intake there

#### Scenario: Headless intake by name is rejected
- **WHEN** the user names the intake workflow explicitly under `--headless`
- **THEN** Agent Runner exits nonzero stating that intake requires an interactive terminal
- **AND** no intake run is created

### Requirement: Intake agent overrides

`--cli` and `--model` SHALL apply only to the intake agent, resolved with precedence *command override → step declaration → agent profile*. The overrides SHALL NOT be inherited by the workflow that intake subsequently launches. An override SHALL be rejected before the intake run starts when it names an unknown CLI, or when it names a known CLI that does not support interactive steps, since intake cannot execute under such an adapter. The overrides SHALL be persisted with the intake run and restored on resume, so a resumed intake continues under the same CLI and model it started with.

#### Scenario: Command override beats the profile
- **WHEN** the user runs `agent-runner -i --cli codex` and the intake agent's profile declares a different CLI
- **THEN** the intake agent session runs under `codex`

#### Scenario: Command override beats the step declaration
- **WHEN** the user runs `agent-runner -i --model <model>` and the intake workflow's agent step declares a different model
- **THEN** the intake agent session runs under the model given on the command line

#### Scenario: Overrides do not reach the launched run
- **WHEN** an intake run started with `--cli codex` launches a selected workflow
- **THEN** the launched run resolves every agent through the normal step and profile rules, with no intake override applied

#### Scenario: Unknown CLI is rejected before intake starts
- **WHEN** the user runs `agent-runner -i --cli not-a-real-cli`
- **THEN** Agent Runner exits nonzero naming the invalid value and the accepted values
- **AND** no intake run is created

#### Scenario: CLI that cannot run interactive steps is rejected
- **WHEN** the user overrides the CLI with a known adapter that rejects interactive steps
- **THEN** Agent Runner exits nonzero explaining that intake requires an interactive-capable CLI
- **AND** no intake run is created

#### Scenario: Overrides survive resume
- **WHEN** an intake run started with `--cli` or `--model` is interrupted and resumed
- **THEN** the resumed intake agent session runs under the same overridden CLI and model

### Requirement: Exploration-only completion

An intake run whose agent step completes without an accepted route SHALL finish successfully, SHALL NOT launch any workflow, and SHALL preserve any handoff the agent wrote. Because the handoff path is runner-owned rather than supplied by the agent, Agent Runner SHALL be able to report that location whenever the file exists and is non-empty, even though no route request was ever submitted.

#### Scenario: Completing with no route launches nothing
- **WHEN** the intake agent completes its step and no route was ever accepted
- **THEN** the intake run finishes with a success outcome
- **AND** no new run is created

#### Scenario: Handoff written without a route is preserved
- **WHEN** the intake agent wrote a handoff but completed the step without submitting a route
- **THEN** the handoff file remains readable after the run finishes
- **AND** Agent Runner reports its path

### Requirement: Launching the selected workflow

Agent Runner SHALL launch the selected workflow only when all three conditions hold: the intake run finished successfully, its persisted state is marked completed, and its route is frozen. A frozen route alone SHALL NOT authorize a launch, because a route freezes when completion is accepted, which is an intermediate state that a later durability failure can still turn into a failed step.

When those conditions hold, Agent Runner SHALL start exactly one new top-level run of the selected workflow using the sealed exact workflow source reference recorded at acceptance, without re-resolving the logical workflow name. The launched run SHALL be prepared through the same loading, parameter binding, pre-validation, and engine-creation path as a manually launched run, so an intake-launched workflow is validated and configured identically to the same workflow launched by hand.

Launching SHALL be a property of the intake run rather than of the entry point used to start it, so an intake run started by workflow name launches identically to one started through `-i` or the New tab entry, and the transition SHALL occur without requiring the user to dismiss a completed-run view first.

The launched run's first agent session SHALL be fresh and SHALL NOT inherit the intake conversation. The sealed handoff SHALL be copied into the launched run's own directory so the launched run does not depend on the intake run's directory surviving.

#### Scenario: Exactly one run is launched
- **WHEN** an intake run finishes successfully with a frozen route and completed state
- **THEN** exactly one new top-level run of the selected workflow is created

#### Scenario: A frozen route on a failed run does not launch
- **WHEN** an intake run's route is frozen but the run finishes with a failed outcome
- **THEN** no workflow is launched

#### Scenario: Launched run is prepared like a manual launch
- **WHEN** a workflow that requires pre-validation or an engine is launched from intake
- **THEN** it is pre-validated and its engine created exactly as when the same workflow is launched manually
- **AND** a pre-validation failure prevents the launch and reports the failure rather than starting a partially prepared run

#### Scenario: Intake started by name launches identically
- **WHEN** the user starts intake by naming the intake workflow directly rather than using `-i` or the New tab entry, and the run finishes successfully with a frozen route
- **THEN** the selected workflow is launched exactly as it would have been from either entry point
- **AND** the transition happens without the user having to dismiss a completed-run view

#### Scenario: Sealed path wins over later version selection
- **WHEN** a newer version of the selected workflow becomes available between route acceptance and launch
- **THEN** the launched run executes the workflow definition recorded in the sealed route, not the newer version

#### Scenario: Launched workflow starts a fresh session
- **WHEN** the launched workflow reaches its first agent step
- **THEN** that step runs in a new agent session containing none of the intake conversation

#### Scenario: Handoff is copied into the launched run
- **WHEN** a workflow is launched from intake
- **THEN** the sealed handoff is present in the launched run's own directory
- **AND** deleting the intake run afterward leaves the launched run's handoff readable

### Requirement: Child-owned launch provenance

The launched run SHALL record the intake run's ID, its own run ID, and the workflow version it actually executed. The intake run SHALL record the frozen route and that a launch was attempted, and SHALL NOT record the launched run's ID, because that ID does not exist until after the intake process has been replaced and its audit log closed.

#### Scenario: Launched run names its intake parent
- **WHEN** a run is launched from intake
- **THEN** its persisted state identifies the intake run it came from, its own run ID, and the workflow version it executed

#### Scenario: Intake run records the route and the attempt
- **WHEN** an intake run finalizes with a frozen route
- **THEN** its audit evidence records the selected workflow, the sealed source path, the parameters, the handoff location, and that a launch was attempted

#### Scenario: Intake run does not claim a child run ID
- **WHEN** an intake run's evidence is inspected after a successful launch
- **THEN** it contains no launched-run ID, and the linkage is discoverable from the launched run instead

### Requirement: Launch failure and interruption recovery

If starting the launched run fails, Agent Runner SHALL exit nonzero, report the failure and the sealed handoff path, and SHALL NOT leave a partially created run behind.

A frozen route SHALL be immutable: it survives interruption and failure, and no later submission may replace it. When an intake run freezes a route but then fails — most commonly because turn durability could not be confirmed after completion was accepted — Agent Runner SHALL retain the frozen route, launch nothing, and allow the run to be resumed. A resumed attempt SHALL retry completion against that same frozen route, and a successful resumed attempt SHALL launch it. This preserves a decision the user actually made without acting on a turn that was never durably recorded.

If the process is terminated after the route freezes and after the run completed successfully, but before the launch, the intake run SHALL remain complete and inspectable with its sealed handoff intact, and the user SHALL restart intake rather than resuming it.

#### Scenario: Launch failure leaves no partial run
- **WHEN** starting the launched run fails
- **THEN** Agent Runner exits nonzero, reports the cause and the sealed handoff path
- **AND** no new run directory is left behind

#### Scenario: Durability failure after freeze does not launch
- **WHEN** a route is frozen at completion acceptance and turn durability is subsequently not confirmed, failing the step
- **THEN** no workflow is launched
- **AND** the frozen route is retained unchanged

#### Scenario: Resume after a durability failure launches the original route
- **WHEN** an intake run that failed after freezing a route is resumed and its completion succeeds
- **THEN** the workflow recorded in the original frozen route is launched
- **AND** no submission during the resumed attempt could have replaced it

#### Scenario: Termination between freeze and launch leaves an inspectable run
- **WHEN** the process is killed after a successful intake run froze its route but before the launch, and the user later passes that run to `--resume`
- **THEN** the run opens in inspect mode because it is already complete
- **AND** the sealed handoff remains readable

### Requirement: Invocations naming another workflow bypass intake

Invocations that name a workflow other than the intake workflow, and headless invocations that name no workflow, SHALL never start intake. Naming the intake workflow explicitly is not a bypass: it starts intake, subject to the interactive-terminal requirement above.

#### Scenario: Explicit non-intake workflow never starts intake
- **WHEN** the user runs `agent-runner spec-driven:change change_name=example`
- **THEN** the named workflow runs directly and no intake run is created

#### Scenario: Headless invocation with a workflow is unchanged
- **WHEN** the user runs `agent-runner --headless spec-driven:change change_name=example`
- **THEN** the named workflow runs headlessly exactly as it does today

#### Scenario: Headless invocation without a workflow does not start intake
- **WHEN** the user runs `agent-runner --headless` with no workflow
- **THEN** Agent Runner does not start intake
