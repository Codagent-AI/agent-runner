# Task: Deliver Four-Role Native Setup

## Goal

Replace the two-role native setup and persistence path with one responsive, recommendation-first four-role experience that safely writes canonical direct profiles, supports accept-all or ordered customization, preserves cancellation and persistence boundaries, and clearly explains role, backend, fallback, scope, and progress behavior.

## Background

`internal/onboarding/native/native.go` currently detects adapters synchronously, discovers models after individual CLI selections, stores separate Planner/Implementor fields and stages, and writes a two-role request. `internal/profilewrite/profilewrite.go` and `cmd/agent-runner/internal_cmd.go` expose the matching two-role persistence contract. Refactor these surfaces together around the completed `internal/profilerecommend` contract so the repository remains buildable and testable throughout this delivery unit.

Expand `internal/profilewrite.Request` into an explicit four-role request containing the target path plus a CLI and optional model for each canonical role. The writer—not the caller—derives `lead` as interactive and `crosscheck`, `implementor`, and `tester` as autonomous. Continue using YAML nodes and the same-directory atomic rename path. Write direct canonical entries, omit empty model fields, preserve every unrelated key, profile set, and unmanaged agent, and remove `planner` and `reviewer` only from `profiles.default.agents` during an explicitly confirmed write. Collision detection must report sorted canonical and legacy managed entries without treating bases, Summarizer, or arbitrary agents as managed. Preserve `0o600` file mode, `0o755` creation for a missing parent, and the existing parent directory's mode.

Keep `agent-runner internal write-profile` as a strict JSON wrapper over the same shared writer. Replace its hidden two-role payload with the four-role contract, reject missing CLIs and unknown fields, and retain target-path validation; the old internal payload is not a compatibility surface. Cover the shared request, YAML merge, collision detection, atomic failure behavior, file modes, and command wrapper in `internal/profilewrite/profilewrite_test.go` and `cmd/agent-runner/internal_cmd_test.go`.

Start setup in a dedicated non-actionable loading stage. Detect supported adapters and query every detected adapter concurrently, with at most the five supported CLI calls and the existing per-query timeout. Return one aggregate Bubble Tea message only after all discovery completes. Preserve detector order and each adapter's model order explicitly so goroutine completion cannot affect recommendations. Keep this discovery snapshot immutable for the session; accept-all, customization, and paired-role recomputation must not launch another subprocess.

After loading, render one four-role recommendation summary with exactly two profile-selection actions: accept the complete recommendation or customize. Accept-all freezes all four displayed selections. Customize uses a generic role cursor in Lead, Crosscheck, Implementor, Tester order and generic CLI/model/default-model substates. Finalizing Lead recomputes Crosscheck from the immutable snapshot; finalizing Implementor recomputes Tester. Manual same-family choices remain valid. If the engine reports that known family diversity could not be established, the summary must visibly identify the affected Lead/Crosscheck or Implementor/Tester pair and explain the limitation. If the engine recommends an adapter default while models exist, prepend and focus `Use CLI default`; keep all discovered models available. Empty and failed discovery use an explicit default-model surface, and a per-CLI error must remain visible without removing that CLI.

Derive wizard progress from a semantic step plan rather than enum arithmetic. Loading and demo-only mode have no progress. The summary is step 1; customization conditionally contributes one step per role; all substates for a role share its step; overwrite contributes a step only when collisions exist. Keep the existing plugin, scope, overwrite, demo, setup-completion, and cancellation boundaries. Show Autonomous Backend immediately after accept-all or completed customization, preselect Headless, then show permission mode. Persist backend, permission, profile, and completion only through the successful setup path; cancellation must not introduce a backend value from the abandoned attempt or mark setup complete.

Remove Claude programmatic/API-billing disclosure copy and tests. Backend copy must explain invocation behavior without claiming that a mode avoids or incurs billing. Retain existing plugin-install behavior and demo-only behavior unless an approved scenario changes it.

Update `internal/onboarding/native/native_test.go`, `internal/onboarding/native/plugin_test.go`, and command-level onboarding coverage. Use controllable fakes/channels to prove discovery concurrency, immutable reuse, aggregate ordering, and no partial actionable state. Keep visual assertions resilient by testing semantic content and progress rather than internal stage ordinals.

Complete the product documentation cutover. `docs/agent-profiles.md` must document the four canonical roles, Crosscheck versus Agent Validator, role CLI/family/tier policy and practical quality/cost rationale, worked examples, customization, direct entries, and undated legacy aliases. `docs/setup.md` must explain discovery, loading, recommendation summary, accept-all/customization, fallback behavior, and the Headless recommendation. Update other public documentation and examples to canonical names, while keeping explicit legacy-compatibility guidance and the rollback rename described in `design.md`. Follow `docs/AGENTS.md`.

## Spec

### Requirement: Editor produces a fixed four-agent shape

The editor SHALL write or replace exactly four setup-managed entries under `profiles.default.agents`: `lead`, `crosscheck`, `implementor`, and `tester`. `lead` SHALL include `default_mode: interactive`; `crosscheck`, `implementor`, and `tester` SHALL each include `default_mode: autonomous`. Every managed entry SHALL include its selected `cli` and SHALL include its selected `model` only when the selection is concrete. The four managed entries SHALL be direct profiles and SHALL NOT use `extends`.

The editor SHALL NOT create or replace `interactive_base`, `autonomous_base`, `summarizer`, or any other unmanaged agent. If any unmanaged entry already exists, the editor SHALL preserve it unchanged.

#### Scenario: Successful first-time write
- **WHEN** the user accepts lead `claude`/`opus`, crosscheck `codex`/`gpt-5.6-sol`, implementor `codex`/`gpt-5.6-terra`, and tester `claude`/`sonnet` at global scope
- **THEN** `~/.agent-runner/config.yaml` contains direct entries for those four roles with lead interactive, the other three autonomous, and the selected CLI and model on each entry

#### Scenario: Adapter default omits model
- **WHEN** one managed role is configured to use its CLI's adapter default
- **THEN** the editor writes that role's `default_mode` and `cli` and omits its `model` field

#### Scenario: Editor preserves unmanaged agents
- **WHEN** the selected config already contains `interactive_base`, `autonomous_base`, `summarizer`, or another unmanaged agent
- **THEN** the editor leaves every unmanaged entry unchanged while writing the four managed roles

The recommendation engine task owns generation of diversity-limitation metadata. This task owns the user-visible clause of the following shared acceptance scenario:

#### Scenario: Diversity fallback is explained
- **WHEN** every eligible crosscheck or tester candidate has the paired creator's family or an unknown or unclassified family
- **THEN** the editor applies normal precedence without claiming family diversity and visibly explains which pair could not be diversified

### Requirement: User chooses CLI and model for each base agent

The native setup profile editor SHALL first present a summary of its four-role recommendation with exactly two profile-selection actions: accept the complete recommendation or customize the roles individually. Accepting the recommendation SHALL retain all four recommended role selections without showing individual role-selection screens.

The customization path SHALL visit roles in this order: lead, crosscheck, implementor, tester. Each role's CLI and model screens SHALL initially focus the current recommendation. CLI options SHALL contain only adapters detected at runtime. Model options SHALL contain the models discovered for the selected CLI, including unclassified models. When the current recommendation uses the CLI default despite one or more discovered models, the model screen SHALL also include and initially focus a synthetic `Use CLI default` option that leaves the model unset.

After the user selects lead, the editor SHALL recompute the crosscheck recommendation against the selected lead family. After the user selects implementor, the editor SHALL recompute the tester recommendation against the selected implementor family. The editor SHALL allow the user to override either recommendation with a same-family choice.

When model discovery returns no models or fails for a selected CLI, the editor SHALL show an explicit default-model surface with explanatory copy. Continuing from that surface SHALL leave the role's model unset. A discovery failure SHALL NOT remove the selected CLI or block setup. Detecting no supported CLI adapters SHALL block profile setup with an error indicating that none were found on `$PATH`.

#### Scenario: User accepts all recommendations
- **WHEN** the recommendation summary is shown and the user selects the accept action
- **THEN** the editor retains all four displayed role selections and proceeds without showing role CLI or model screens

#### Scenario: User customizes roles in pair order
- **WHEN** the recommendation summary is shown and the user selects customize
- **THEN** the editor presents lead, crosscheck, implementor, and tester selection screens in that order

#### Scenario: Lead change recomputes crosscheck
- **WHEN** the user changes lead to a model family different from the initial recommendation
- **THEN** the crosscheck screen initially focuses the highest-precedence eligible recommendation that differs from the selected lead family when one exists

#### Scenario: Implementor change recomputes tester
- **WHEN** the user changes implementor to a model family different from the initial recommendation
- **THEN** the tester screen initially focuses the highest-precedence eligible recommendation that differs from the selected implementor family when one exists

#### Scenario: Same-family override is allowed
- **WHEN** a different-family crosscheck or tester recommendation is available
- **AND** the user manually selects the same family as its paired creator
- **THEN** the editor accepts the user's explicit selection

#### Scenario: CLI options reflect detected adapters
- **WHEN** the host has `claude` and `codex` on `$PATH` but not `cursor`
- **THEN** every role CLI selection screen presents `claude` and `codex` and does not present `cursor`

#### Scenario: Model options reflect the chosen CLI
- **WHEN** the user selects a CLI whose discovery result contains three models
- **THEN** the corresponding model screen presents those three models in recommendation-adjusted order without hiding unclassified models

#### Scenario: Unclassified models retain focused CLI default
- **WHEN** the selected CLI returns models but none match a recognized tier
- **THEN** the corresponding model screen initially focuses `Use CLI default`
- **AND** lists every discovered model afterward for manual selection
- **AND** selecting the default option leaves the role's model unset

#### Scenario: Model discovery returns empty
- **WHEN** the user selects a CLI whose model discovery result is empty
- **THEN** the editor shows a default-model surface and continuing leaves the role's model unset

#### Scenario: Discovery snapshot contains an error for a customized CLI
- **WHEN** the immutable discovery snapshot contains an error for the CLI selected during customization
- **THEN** the editor explains the error, permits the user to continue with the adapter default, and leaves the role's model unset

#### Scenario: No detected adapters
- **WHEN** adapter detection returns an empty list
- **THEN** native setup fails profile setup with an error indicating no supported CLI adapters were found on `$PATH`

### Requirement: Overwrite confirmation when entries already exist

Before writing, the native setup profile editor SHALL inspect the selected scope's config file, if it exists, for canonical managed entries `lead`, `crosscheck`, `implementor`, and `tester` and legacy aliases `planner` and `reviewer` under `profiles.default.agents`. If any managed entry or alias is present, the editor SHALL display an overwrite confirmation naming every collision. Confirming overwrite SHALL write all four current selections under canonical names, replace canonical collisions, and remove legacy aliases from that profile layer. Canceling SHALL leave the file unchanged and leave native setup incomplete.

#### Scenario: No collisions skip overwrite
- **WHEN** the selected config file contains none of the four canonical role names and neither legacy alias
- **THEN** the editor proceeds to write without showing overwrite confirmation

#### Scenario: Legacy roles trigger overwrite and migration
- **WHEN** the selected config contains hand-authored `planner`, `reviewer`, and `tester` entries
- **THEN** the overwrite confirmation names all three entries
- **AND** confirming writes `lead`, `crosscheck`, `implementor`, and `tester` while removing `planner` and `reviewer`

#### Scenario: User cancels overwrite
- **WHEN** overwrite confirmation is shown and the user selects cancel
- **THEN** no profile file is modified and native setup is not marked complete

#### Scenario: User confirms overwrite
- **WHEN** overwrite confirmation is shown and the user confirms
- **THEN** the editor writes all four managed selections, replaces colliding managed entries, and preserves unrelated configuration

This task owns the screen transition, confirmation/cancellation boundary, and integration with the collision detector and shared writer implemented in this same delivery unit.

### Requirement: Profile write uses shared Go writer

The native setup profile editor SHALL use the tested internal Go profile-writing path directly. The existing `agent-runner internal write-profile` subcommand SHALL remain a wrapper around that same shared writer. User-selected values SHALL NOT enter a YAML emitter inside a shell script. The shared writer SHALL:

- Accept structured CLI and optional model values for lead, crosscheck, implementor, and tester plus the target path.
- Read and parse the existing file, if any, merge the four canonical managed entries into `profiles.default.agents`, and remove legacy `planner` and `reviewer` aliases from that profile layer.
- Preserve every unmanaged agent, other profile set, and unrelated top-level key.
- Write the result atomically using a temporary file and rename in the same directory.
- Set the resulting file mode to `0o600` and create missing parent directories with mode `0o755`.

#### Scenario: Existing other agents are preserved
- **WHEN** the selected config contains an unmanaged `team_implementor` agent, a `summarizer` agent, unrelated profile sets, and unrelated top-level keys
- **THEN** the resulting file contains the four managed role entries and preserves all unmanaged content unchanged

#### Scenario: Atomic write
- **WHEN** the write operation is interrupted or fails part-way through writing
- **THEN** the original file, if any, remains intact and no partial target file is left

#### Scenario: File mode and parent directory creation
- **WHEN** the write target is `~/.agent-runner/config.yaml` and `~/.agent-runner/` does not exist
- **THEN** the directory is created with mode `0o755` and the file is written with mode `0o600`

#### Scenario: Internal command remains wrapper
- **WHEN** `agent-runner internal write-profile` is invoked with a valid four-role payload
- **THEN** it writes through the same shared Go writer used by native setup

#### Scenario: Shell-side YAML is rejected by tests
- **WHEN** the native setup profile editor implementation is examined by automated tests
- **THEN** the tests confirm user-selected values are not rendered into YAML by shell-script string construction

### Requirement: User-initiated, never auto-generated

The editor SHALL run only from a user-facing setup action. The runner SHALL NOT silently generate profile configuration. The recommendation summary SHALL require the user either to accept the displayed recommendation or enter customization before the editor can reach its write path. Canceling before the write SHALL leave profile configuration unchanged and native setup incomplete. The workflow entry point `onboarding:setup-agent-profile` is not required for this capability.

#### Scenario: Editor only runs from setup interaction
- **WHEN** the runner starts on a host with no config file and native setup is not eligible
- **THEN** no profile file is created

#### Scenario: Accept recommendation permits write
- **WHEN** the user explicitly accepts the displayed four-role recommendation and completes the remaining setup choices
- **THEN** native setup can invoke the profile write path with those four selections

#### Scenario: Complete customization permits write
- **WHEN** the user completes all four role customizations and the remaining setup choices
- **THEN** native setup can invoke the profile write path with the customized selections

#### Scenario: Cancel before write preserves configuration
- **WHEN** the user cancels from the recommendation or customization flow before profile writing
- **THEN** no profile file is created or modified and native setup remains incomplete

#### Scenario: Old setup workflow is not required
- **WHEN** the user runs `agent-runner run onboarding:setup-agent-profile`
- **THEN** the agent profile editor capability does not require that workflow to exist

### Requirement: One profile pass per editor session

A single editor session SHALL perform at most one profile write. Accepting all recommendations SHALL constitute one profile-selection pass. Choosing customization SHALL constitute one sequential lead, crosscheck, implementor, and tester selection pass. The editor SHALL NOT provide back navigation to a role whose selection has been finalized; revising an earlier finalized role SHALL require canceling and rerunning the editor.

#### Scenario: Accept-all produces one pass
- **WHEN** the user accepts the complete recommendation and finishes setup
- **THEN** the editor performs one profile write containing the four displayed selections

#### Scenario: Customization produces one sequential pass
- **WHEN** the user customizes all four roles and finishes setup
- **THEN** the editor performs one profile write containing the four finalized selections

#### Scenario: User wants to revise an earlier role
- **WHEN** the user has finalized a role and wants to change it after advancing
- **THEN** the editor offers cancellation rather than back navigation and does not write partial selections

### Requirement: Native setup is mandatory

Native setup SHALL detect supported CLI adapters and discover their available models before presenting an actionable profile-selection surface. While recommendation inputs are being collected, setup SHALL display a non-actionable loading state. When at least one supported CLI is detected, the first actionable profile surface SHALL be the four-role recommendation summary defined by the agent-profile-editor capability rather than an individual lead CLI screen.

Native setup SHALL NOT offer skip, not-now, or dismiss actions during required profile setup. A user who cancels or interrupts recommendation, customization, backend, permission, scope, overwrite, or skill-installation stages SHALL leave setup incomplete, and native setup SHALL be offered again on the next eligible launch.

Failure to discover models from one detected CLI SHALL NOT fail setup; setup SHALL retain that CLI with its adapter default, carry the discovery limitation into the recommendation summary, and continue. Failure to detect any supported CLI SHALL present a blocking failure as the first actionable result.

#### Scenario: Recommendation summary is the first actionable profile surface
- **WHEN** native setup detects at least one supported CLI and finishes recommendation discovery
- **THEN** setup presents the four-role recommendation summary without first presenting an individual lead CLI screen

#### Scenario: Recommendation discovery shows loading state
- **WHEN** native setup is still detecting adapters or discovering models needed for the recommendation
- **THEN** setup displays a non-actionable loading state and does not permit accepting an incomplete recommendation

#### Scenario: Individual discovery failure remains usable
- **WHEN** model discovery fails for one detected CLI and at least one supported CLI was detected
- **THEN** setup presents the recommendation summary with that CLI eligible at its adapter default and visibly identifies the discovery limitation

#### Scenario: Adapter detection failure is blocking
- **WHEN** native setup detects no supported CLI adapters
- **THEN** the failure message is the first actionable result and no profile recommendation can be accepted

#### Scenario: Setup cannot be skipped
- **WHEN** native setup renders recommendation, customization, backend, permission, scope, overwrite, or skill-installation stages
- **THEN** no skip, not-now, or dismiss action is available

#### Scenario: Cancel leaves setup incomplete
- **WHEN** the user cancels or interrupts native setup before all required setup actions complete
- **THEN** setup completion is not recorded and native setup is offered again on the next eligible launch

### Requirement: Autonomous backend selection during setup

After the user accepts the complete four-role recommendation or finishes lead, crosscheck, implementor, and tester customization, native setup SHALL present an "Autonomous Backend" selection screen. The screen SHALL display the three `autonomous_backend` options Headless, Interactive, and Interactive for Claude, each with a one-sentence explanation of invocation behavior. `Headless` SHALL be preselected and labeled as the recommended default. Backend descriptions SHALL NOT claim that one invocation mode avoids API billing.

After the user selects an autonomous backend, setup SHALL present the existing Autonomous Permission Mode selection screen. The selected backend SHALL be written to `~/.agent-runner/settings.yaml` only when setup completes successfully. Changing the setup recommendation SHALL NOT migrate or rewrite an already persisted backend outside a completed setup flow.

#### Scenario: Accept-all proceeds to backend selection
- **WHEN** the user accepts the complete four-role recommendation
- **THEN** setup presents the Autonomous Backend screen without showing individual role-selection screens

#### Scenario: Customization proceeds to backend selection
- **WHEN** the user completes tester customization after lead, crosscheck, and implementor
- **THEN** setup presents the Autonomous Backend screen

#### Scenario: Headless is preselected
- **WHEN** the Autonomous Backend screen is presented
- **THEN** `Headless` is focused and labeled as the recommended default

#### Scenario: Every backend option has behavioral copy
- **WHEN** the Autonomous Backend screen is presented
- **THEN** Headless, Interactive, and Interactive for Claude are all available with explanations of invocation behavior and without provider-billing claims

#### Scenario: Permission mode follows backend
- **WHEN** the user selects an Autonomous Backend option
- **THEN** setup presents the Autonomous Permission Mode screen before scope and skill installation complete

#### Scenario: Selected backend is persisted on setup completion
- **WHEN** the user selects an autonomous backend and setup completes successfully
- **THEN** `~/.agent-runner/settings.yaml` contains the selected `autonomous_backend` value

#### Scenario: Cancelled setup does not persist backend
- **WHEN** the user selects an autonomous backend but cancels setup before completion
- **THEN** `~/.agent-runner/settings.yaml` does not contain an `autonomous_backend` value from the canceled attempt

#### Scenario: Existing backend is not migrated automatically
- **WHEN** a user already has `autonomous_backend: interactive-claude` and native setup does not complete a new setup flow
- **THEN** the runner leaves that persisted value unchanged

### Requirement: Native setup implementor CLI billing disclosure

The native setup implementor CLI selection SHALL disclose Claude programmatic usage billing next to the Claude option. The disclosure SHALL appear before the user confirms the implementor CLI choice so users can choose the default implementor backend with that cost context visible.

**Reason**: Current Claude Code billing depends on authentication and provider policy rather than whether Agent Runner invokes Claude interactively or through `claude -p`; the provider's previously announced programmatic-usage separation was paused. A mandatory warning tied specifically to autonomous Claude selection is therefore misleading.

**Migration**: Remove Claude-specific programmatic/API-billing warnings from native setup recommendation and customization surfaces. Keep backend descriptions focused on invocation behavior. Provider billing guidance, if presented elsewhere, must describe authentication-dependent behavior without using backend mode as a billing proxy.

#### Scenario: Claude implementor option shows programmatic billing disclosure
- **WHEN** native setup renders the implementor CLI selection and `claude` is an available option
- **THEN** the `claude` option includes a visible programmatic credits/API-rate billing disclosure

The requirement above is removed by this change: delete the disclosure behavior and its positive tests, and retain only billing-neutral invocation explanations.

### Requirement: Recommendation-first setup presentation

Native setup SHALL present a dedicated non-actionable loading state while it detects available CLIs and discovers models for the four standard roles. After discovery completes, native setup SHALL present a single actionable summary containing the recommended lead, crosscheck, implementor, and tester selections before asking the user to accept or customize them.

The summary SHALL identify the recommended CLI and model for each role, SHALL disclose any model-discovery failure or fallback that affected a recommendation, and SHALL offer distinct actions to accept all recommendations or customize them. The customization path SHALL visit roles in lead, crosscheck, implementor, then tester order.

#### Scenario: Discovery has a dedicated loading state
- **WHEN** native setup is detecting CLIs or discovering models
- **THEN** the panel shows a non-actionable loading state
- **AND** the panel does not show stale recommendation or selection controls

#### Scenario: Summary presents all recommendations together
- **WHEN** CLI and model discovery completes
- **THEN** native setup presents lead, crosscheck, implementor, and tester recommendations on one summary screen
- **AND** each recommendation identifies its CLI and selected model or CLI-default fallback
- **AND** the screen offers actions to accept all recommendations or customize them

#### Scenario: Discovery limitation is visible
- **WHEN** a CLI remains eligible after its model discovery fails
- **THEN** the recommendation summary identifies that CLI's discovery limitation
- **AND** the affected recommendation identifies that the CLI default will be used

#### Scenario: Customization follows role order
- **WHEN** the user chooses to customize the recommendations
- **THEN** native setup presents role customization in lead, crosscheck, implementor, then tester order

### Requirement: Explanatory copy on each screen

Every actionable selection screen in native setup SHALL include a title and a short explanatory paragraph (2-4 sentences) that describes what is being asked and why it matters. The tone SHALL be friendly and informative — not terse, not verbose. The non-actionable recommendation loading state SHALL include concise status copy explaining that Agent Runner is inspecting available CLIs and models.

The recommendation experience SHALL include a brief welcoming sentence acknowledging this is initial setup. Its summary SHALL explain that the four selections configure interactive leadership, independent planning crosscheck, implementation, and acceptance testing. Crosscheck copy SHALL explain that it challenges Lead's planning artifacts and looks for omissions, while Agent Validator owns implementation-code review. Each role-customization screen SHALL explain what that role does and what its CLI or model selection controls.

A default-model screen SHALL explain that Agent Runner will use the CLI default and leave the model field unset. The autonomous-backend screen SHALL explain the runtime behavior of the headless, interactive, and interactive-Claude choices, SHALL identify headless as the recommendation, and SHALL NOT make claims about provider billing policy. The scope screen SHALL explain the difference between global and project scope. The demo prompt screen SHALL explain what the onboarding demo is and what the user will see if they continue.

#### Scenario: Recommendation experience includes welcome language
- **WHEN** native setup renders recommendation loading or the recommendation summary
- **THEN** the experience includes a welcoming sentence for initial setup
- **AND** it explains that Agent Runner is preparing or presenting a four-role profile

#### Scenario: Role customization explains purpose
- **WHEN** native setup renders a CLI or model selection screen for a role
- **THEN** the screen explains what the corresponding role does
- **AND** it explains what the current selection controls

#### Scenario: Crosscheck is distinguished from code review
- **WHEN** native setup presents the recommendation summary or Crosscheck customization
- **THEN** the copy describes Crosscheck as an independent challenge to Lead's planning artifacts and completeness
- **AND** identifies Agent Validator as the implementation-code review system

#### Scenario: Default-model screen explains fallback
- **WHEN** native setup renders a default-model screen after empty or failed model discovery
- **THEN** the screen explains that Agent Runner will use the CLI default and leave the model field unset

#### Scenario: Backend screen explains behavior without billing claims
- **WHEN** native setup renders the autonomous-backend selection screen
- **THEN** the screen explains the runtime behavior of headless, interactive, and interactive-Claude execution
- **AND** it identifies headless as the recommended choice
- **AND** it does not claim that a choice avoids or incurs provider billing

#### Scenario: Scope selection explains options
- **WHEN** native setup renders the scope selection screen
- **THEN** the screen explains the difference between global and project scope

#### Scenario: Demo prompt explains the demo
- **WHEN** native setup renders the demo prompt screen
- **THEN** the screen explains what the onboarding demo contains and what the user will experience

### Requirement: Native setup shows wizard progress

Native setup SHALL show a compact wizard-style step indicator inside the setup panel on actionable setup screens. The indicator SHALL be centered above the screen heading and SHALL include text in the form `Step N of X` plus a visual progress bar. Recommendation loading SHALL NOT show a wizard step indicator, and the recommendation summary SHALL be the first actionable step.

When the user accepts all recommendations, the wizard total SHALL omit the four role-customization steps. When the user chooses customization, the wizard total SHALL include one semantic step for each role in lead, crosscheck, implementor, then tester order. A role's CLI selection, model selection, and default-model fallback SHALL all show the same step number. The total SHALL reflect the selected branch and SHALL include an overwrite-confirmation step only when that confirmation is shown. Demo-prompt-only re-show mode SHALL NOT show native setup progress.

#### Scenario: Loading state omits progress
- **WHEN** native setup is loading recommendations
- **THEN** the panel does not show a `Step N of X` indicator

#### Scenario: Recommendation summary starts progress
- **WHEN** native setup renders the actionable recommendation summary
- **THEN** the panel shows `Step 1 of X`
- **AND** the progress indicator is centered above the screen heading

#### Scenario: Accept-all omits customization steps
- **WHEN** the user accepts all four recommendations
- **THEN** the progress total omits lead, crosscheck, implementor, and tester customization steps
- **AND** the next actionable screen advances from the recommendation-summary step

#### Scenario: Customize adds one step per role
- **WHEN** the user chooses to customize the recommendations
- **THEN** the progress total includes one step each for lead, crosscheck, implementor, and tester
- **AND** those role steps occur in that order

#### Scenario: Role sub-states preserve wizard step
- **WHEN** native setup moves among CLI selection, model selection, or default-model fallback for one role
- **THEN** the panel shows the same step number for all of those states

#### Scenario: Overwrite confirmation conditionally adds a step
- **WHEN** native setup shows the overwrite confirmation screen
- **THEN** the progress total includes that confirmation as one additional step
- **AND** later screens retain the adjusted total

#### Scenario: Demo-only mode omits progress
- **WHEN** native setup re-shows only the demo prompt
- **THEN** the panel does not show a native setup progress indicator

## Done When

- The shared request, validation, collision detector, YAML merge, native caller, and internal command payload move to the approved four-role contract in the same task, with no temporary two-role bridge or uncompilable intermediate handoff.
- Persistence tests cover exact modes, optional-model omission, direct profiles without `extends`, preservation of unmanaged content, canonical replacement, legacy removal, sorted collision reporting, malformed YAML, atomic failure behavior, directory/file modes, strict command decoding, and no shell-side YAML.
- Native setup performs one concurrent, aggregate discovery pass; its loading state is non-actionable, ordering is deterministic, per-CLI failures remain usable, and customization never re-runs discovery.
- Accept-all and customize flows satisfy every scenario above, visibly explain any affected pair when family diversity cannot be established, use the completed recommendation and persistence contracts, perform at most one profile write, and preserve all cancellation/setup-completion boundaries.
- The generic role cursor and semantic step plan cover four roles without duplicated per-role state; role substates share progress, overwrite is conditional, and demo-only mode has no setup progress.
- Backend selection follows both branches, Headless is the billing-neutral recommended default, permission follows it, and settings are persisted only by successful completion.
- Native, plugin, command-level onboarding, concurrency, rendering, and persistence integration tests pass without relying on internal stage ordinals.
- `docs/agent-profiles.md`, `docs/setup.md`, and affected public examples fully describe canonical roles, recommendation policy/rationale/examples, Crosscheck versus Agent Validator, customization/fallback behavior, legacy compatibility, and downgrade guidance according to `docs/AGENTS.md`.
- `go test ./internal/profilewrite ./internal/onboarding/native ./cmd/agent-runner` passes, then `make fmt`, `make test`, `make lint`, and `openspec validate --type change "tester-profile"` all pass.
