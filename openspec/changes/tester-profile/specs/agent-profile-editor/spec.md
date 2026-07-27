## ADDED Requirements

### Requirement: Four-role profile recommendations

The native setup profile editor SHALL build a concrete recommendation for `planner`, `reviewer`, `implementor`, and `tester` from the CLI adapters detected on the host and the models discovered through those adapters. Each recommendation SHALL identify a CLI and either a concrete model or the adapter default when no model is available.

The editor SHALL order recommendation candidates as follows:

- `planner`: CLI precedence `claude`, `codex`, `copilot`, `cursor`, `opencode`; family precedence Claude, OpenAI GPT, Gemini, other; flagship model preference.
- `reviewer`: CLI precedence `codex`, `claude`, `copilot`, `cursor`, `opencode`; family precedence OpenAI GPT, Claude, Gemini, other; flagship model preference.
- `implementor`: CLI precedence `codex`, `claude`, `copilot`, `cursor`, `opencode`; family precedence OpenAI GPT, Claude, Gemini, other; balanced model preference.
- `tester`: CLI precedence `claude`, `codex`, `copilot`, `cursor`, `opencode`; family precedence Claude, OpenAI GPT, Gemini, other; balanced model preference.

For `reviewer`, the editor SHALL exclude the recommended planner model family before applying reviewer precedence whenever any different-family candidate is available. For `tester`, the editor SHALL exclude the recommended implementor model family before applying tester precedence whenever any different-family candidate is available. When diversity is unavailable, the editor SHALL retain all candidates, apply normal precedence, and explain the same-family fallback on the recommendation surface.

The maintained power-tier policy SHALL remain bounded to these recognized model aliases:

- Claude flagship: `opus`
- Claude balanced: `sonnet`
- OpenAI GPT flagship: `gpt-5.6-sol`
- OpenAI GPT balanced: `gpt-5.6-terra`

For a role whose preferred recognized tier is unavailable, the editor SHALL prefer the other recognized tier in the selected family, then the first unrecognized model in that CLI's discovery order, then the CLI default with the model unset. Models outside the bounded policy SHALL remain eligible and SHALL retain their discovery order; the editor SHALL NOT reject or hide them because they are unclassified.

#### Scenario: Claude and Codex produce the standard recommendation
- **WHEN** `claude` exposes `opus` and `sonnet` and `codex` exposes `gpt-5.6-sol` and `gpt-5.6-terra`
- **THEN** the editor recommends `claude` with `opus` for planner, `codex` with `gpt-5.6-sol` for reviewer, `codex` with `gpt-5.6-terra` for implementor, and `claude` with `sonnet` for tester

#### Scenario: Reviewer differs from planner when possible
- **WHEN** the selected planner family is Claude and at least one OpenAI GPT candidate is available
- **THEN** the editor recommends a non-Claude reviewer candidate according to reviewer precedence

#### Scenario: Tester differs from implementor when possible
- **WHEN** the selected implementor family is OpenAI GPT and at least one Claude candidate is available
- **THEN** the editor recommends a non-GPT tester candidate according to tester precedence

#### Scenario: One multi-provider CLI supplies independent families
- **WHEN** only one supported CLI is detected and it exposes both Claude and OpenAI GPT models
- **THEN** the editor SHALL recommend that CLI for paired roles while recommending different model families according to role precedence

#### Scenario: Diversity fallback is explained
- **WHEN** every eligible reviewer candidate has the planner family or every eligible tester candidate has the implementor family
- **THEN** the editor applies normal precedence without family exclusion and visibly explains which pair could not be diversified

#### Scenario: Balanced model is unavailable
- **WHEN** a balanced role selects a family whose balanced recognized alias is unavailable but whose flagship recognized alias is available
- **THEN** the editor recommends the flagship alias

#### Scenario: Recognized models are unavailable
- **WHEN** a selected CLI returns models but none match a recognized alias for its family
- **THEN** the editor recommends the first returned model and preserves the remaining models in discovery order for customization

#### Scenario: Model discovery fails for one CLI
- **WHEN** model discovery fails for a detected CLI
- **THEN** the editor keeps that CLI eligible with its model unset, displays the discovery limitation, and continues building the four-role recommendation

#### Scenario: No models are discoverable
- **WHEN** a recommended CLI returns no models
- **THEN** the recommendation identifies that CLI's adapter default and leaves its model unset

## MODIFIED Requirements

### Requirement: Editor produces a fixed four-agent shape

The editor SHALL write or replace exactly four setup-managed entries under `profiles.default.agents`: `planner`, `reviewer`, `implementor`, and `tester`. `planner` SHALL include `default_mode: interactive`; `reviewer`, `implementor`, and `tester` SHALL each include `default_mode: autonomous`. Every managed entry SHALL include its selected `cli` and SHALL include its selected `model` only when the selection is concrete. The four managed entries SHALL be direct profiles and SHALL NOT use `extends`.

The editor SHALL NOT create or replace `interactive_base`, `autonomous_base`, `summarizer`, or any other unmanaged agent. If any unmanaged entry already exists, the editor SHALL preserve it unchanged.

#### Scenario: Successful first-time write
- **WHEN** the user accepts planner `claude`/`opus`, reviewer `codex`/`gpt-5.6-sol`, implementor `codex`/`gpt-5.6-terra`, and tester `claude`/`sonnet` at global scope
- **THEN** `~/.agent-runner/config.yaml` contains direct entries for those four roles with planner interactive, the other three autonomous, and the selected CLI and model on each entry

#### Scenario: Adapter default omits model
- **WHEN** one managed role is configured to use its CLI's adapter default
- **THEN** the editor writes that role's `default_mode` and `cli` and omits its `model` field

#### Scenario: Editor preserves unmanaged agents
- **WHEN** the selected config already contains `interactive_base`, `autonomous_base`, `summarizer`, or another unmanaged agent
- **THEN** the editor leaves every unmanaged entry unchanged while writing the four managed roles

### Requirement: User chooses CLI and model for each base agent

The native setup profile editor SHALL first present a summary of its four-role recommendation with exactly two profile-selection actions: accept the complete recommendation or customize the roles individually. Accepting the recommendation SHALL retain all four recommended role selections without showing individual role-selection screens.

The customization path SHALL visit roles in this order: planner, reviewer, implementor, tester. Each role's CLI and model screens SHALL initially focus the current recommendation. CLI options SHALL contain only adapters detected at runtime. Model options SHALL contain only models discovered for the selected CLI, including unclassified models.

After the user selects planner, the editor SHALL recompute the reviewer recommendation against the selected planner family. After the user selects implementor, the editor SHALL recompute the tester recommendation against the selected implementor family. The editor SHALL allow the user to override either recommendation with a same-family choice.

When model discovery returns no models or fails for a selected CLI, the editor SHALL show an explicit default-model surface with explanatory copy. Continuing from that surface SHALL leave the role's model unset. A discovery failure SHALL NOT remove the selected CLI or block setup. Detecting no supported CLI adapters SHALL block profile setup with an error indicating that none were found on `$PATH`.

#### Scenario: User accepts all recommendations
- **WHEN** the recommendation summary is shown and the user selects the accept action
- **THEN** the editor retains all four displayed role selections and proceeds without showing role CLI or model screens

#### Scenario: User customizes roles in pair order
- **WHEN** the recommendation summary is shown and the user selects customize
- **THEN** the editor presents planner, reviewer, implementor, and tester selection screens in that order

#### Scenario: Planner change recomputes reviewer
- **WHEN** the user changes planner to a model family different from the initial recommendation
- **THEN** the reviewer screen initially focuses the highest-precedence eligible recommendation that differs from the selected planner family when one exists

#### Scenario: Implementor change recomputes tester
- **WHEN** the user changes implementor to a model family different from the initial recommendation
- **THEN** the tester screen initially focuses the highest-precedence eligible recommendation that differs from the selected implementor family when one exists

#### Scenario: Same-family override is allowed
- **WHEN** a different-family reviewer or tester recommendation is available
- **AND** the user manually selects the same family as its paired creator
- **THEN** the editor accepts the user's explicit selection

#### Scenario: CLI options reflect detected adapters
- **WHEN** the host has `claude` and `codex` on `$PATH` but not `cursor`
- **THEN** every role CLI selection screen presents `claude` and `codex` and does not present `cursor`

#### Scenario: Model options reflect the chosen CLI
- **WHEN** the user selects a CLI whose discovery result contains three models
- **THEN** the corresponding model screen presents those three models in recommendation-adjusted order without hiding unclassified models

#### Scenario: Model discovery returns empty
- **WHEN** the user selects a CLI whose model discovery result is empty
- **THEN** the editor shows a default-model surface and continuing leaves the role's model unset

#### Scenario: Model discovery fails during customization
- **WHEN** model discovery returns an error for the CLI selected during customization
- **THEN** the editor explains the error, permits the user to continue with the adapter default, and leaves the role's model unset

#### Scenario: No detected adapters
- **WHEN** adapter detection returns an empty list
- **THEN** native setup fails profile setup with an error indicating no supported CLI adapters were found on `$PATH`

### Requirement: Overwrite confirmation when entries already exist

Before writing, the native setup profile editor SHALL inspect the selected scope's config file, if it exists, for `planner`, `reviewer`, `implementor`, and `tester` under `profiles.default.agents`. If any managed entry is present, the editor SHALL display an overwrite confirmation naming every colliding managed entry. Confirming overwrite SHALL write all four current selections, replacing every colliding managed entry. Canceling SHALL leave the file unchanged and leave native setup incomplete.

#### Scenario: No collisions skip overwrite
- **WHEN** the selected config file contains none of the four managed role names
- **THEN** the editor proceeds to write without showing overwrite confirmation

#### Scenario: Existing review roles trigger overwrite
- **WHEN** the selected config contains hand-authored `reviewer` and `tester` entries
- **THEN** the overwrite confirmation names both entries

#### Scenario: User cancels overwrite
- **WHEN** overwrite confirmation is shown and the user selects cancel
- **THEN** no profile file is modified and native setup is not marked complete

#### Scenario: User confirms overwrite
- **WHEN** overwrite confirmation is shown and the user confirms
- **THEN** the editor writes all four managed selections, replaces colliding managed entries, and preserves unrelated configuration

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

A single editor session SHALL perform at most one profile write. Accepting all recommendations SHALL constitute one profile-selection pass. Choosing customization SHALL constitute one sequential planner, reviewer, implementor, and tester selection pass. The editor SHALL NOT provide back navigation to a role whose selection has been finalized; revising an earlier finalized role SHALL require canceling and rerunning the editor.

#### Scenario: Accept-all produces one pass
- **WHEN** the user accepts the complete recommendation and finishes setup
- **THEN** the editor performs one profile write containing the four displayed selections

#### Scenario: Customization produces one sequential pass
- **WHEN** the user customizes all four roles and finishes setup
- **THEN** the editor performs one profile write containing the four finalized selections

#### Scenario: User wants to revise an earlier role
- **WHEN** the user has finalized a role and wants to change it after advancing
- **THEN** the editor offers cancellation rather than back navigation and does not write partial selections

### Requirement: Profile write uses shared Go writer

The native setup profile editor SHALL use the tested internal Go profile-writing path directly. The existing `agent-runner internal write-profile` subcommand SHALL remain a wrapper around that same shared writer. User-selected values SHALL NOT enter a YAML emitter inside a shell script. The shared writer SHALL:

- Accept structured CLI and optional model values for planner, reviewer, implementor, and tester plus the target path.
- Read and parse the existing file, if any, and merge the four managed entries into `profiles.default.agents`.
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
