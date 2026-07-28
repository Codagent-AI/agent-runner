# agent-profile-editor Specification

## Purpose
Define the interactive agent profile editor that guides users through choosing CLI adapters, models, and scope to produce the standard four-agent profile shape, writing results atomically via a tested internal Go subcommand.
## Requirements
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

### Requirement: User chooses CLI and model for each role

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

### Requirement: User chooses scope

The native setup profile editor SHALL prompt the user to choose `global` or `project`. `global` SHALL write to `~/.agent-runner/config.yaml`. `project` SHALL write to `.agent-runner/config.yaml` resolved against the runner's working directory. The runner SHALL NOT inspect the cwd for project markers; whichever cwd the user invoked from is the project location.

#### Scenario: Global scope writes to home
- **WHEN** the user picks scope `global`
- **THEN** the write target is `~/.agent-runner/config.yaml`

#### Scenario: Project scope writes to cwd
- **WHEN** the user picks scope `project` and the runner was invoked from `/path/to/project`
- **THEN** the write target is `/path/to/project/.agent-runner/config.yaml`

#### Scenario: Project scope without project markers
- **WHEN** the user picks scope `project` and the cwd has no `.git` directory or other project marker
- **THEN** the write proceeds without warning and the file is created at `<cwd>/.agent-runner/config.yaml`

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

### Requirement: Four-role profile recommendations

The native setup profile editor SHALL build a concrete recommendation for `lead`, `crosscheck`, `implementor`, and `tester` from the CLI adapters detected on the host and the models discovered through those adapters. Each recommendation SHALL identify a CLI and either a recognized concrete model or the adapter default when no recognized tier is available.

The editor SHALL order recommendation candidates as follows:

- `lead`: CLI precedence `claude`, `codex`, `opencode`, `copilot`, `cursor`; family precedence Claude, OpenAI GPT, other; flagship model preference.
- `crosscheck`: the same pinned CLI precedence as `lead`; family precedence OpenAI GPT, Claude, other; flagship model preference.
- `implementor`: CLI precedence `codex`, `cursor`, `opencode`, `claude`, `copilot`; family precedence OpenAI GPT, Claude, other; balanced model preference.
- `tester`: the same pinned CLI precedence as `lead`; family precedence Claude, OpenAI GPT, other; balanced model preference.

After applying any diversity filter, the editor SHALL evaluate CLIs in the role's pinned CLI precedence order. Within each CLI, it SHALL apply the role's family precedence, then its preferred-tier and fallback rules. Discovery order SHALL break otherwise equal candidate rankings.

When the selected lead has a recognized family and at least one candidate has a known, different family, the editor SHALL exclude same-family, `other`, and unknown candidates from Crosscheck's automatic recommendation before applying normal precedence. For Tester, the editor SHALL apply the same rule against the selected Implementor. A concrete model SHALL be treated as Claude or OpenAI GPT only when its CLI or boundary-delimited identifier establishes that family. A model-less default from `claude` SHALL be treated as Claude, a model-less default from `codex` SHALL be treated as OpenAI GPT, and a model-less default from a multi-provider CLI SHALL have unknown family. `other` and unknown candidates SHALL remain available during customization and SHALL remain eligible for automatic recommendation when known diversity cannot be established, but they SHALL NOT establish diversity. When diversity cannot be established, the editor SHALL apply normal precedence and explain the limitation on the recommendation surface.

The maintained power-tier policy SHALL remain bounded to:

- Claude flagship: a discovered identifier containing the whole token `opus`
- Claude balanced: a discovered identifier containing the whole token `sonnet`
- OpenAI GPT flagship: a discovered identifier containing a boundary-safe `gpt` marker or `gpt` immediately followed by a digit, plus a boundary-safe `sol` marker
- OpenAI GPT balanced: a discovered identifier containing a boundary-safe `gpt` marker or `gpt` immediately followed by a digit, plus a boundary-safe `terra` marker

GPT matching SHALL be case-insensitive, SHALL treat common provider and punctuation separators as token boundaries, and SHALL NOT match `sol` or `terra` as substrings of larger tokens. When multiple GPT identifiers match one tier, the editor SHALL select the highest numeric version immediately following the GPT marker; equal or unparseable versions SHALL retain discovery order. The editor SHALL persist the exact discovered identifier rather than a normalized alias or wildcard.

For a role whose preferred recognized tier is unavailable, the editor SHALL prefer the other recognized tier in the selected family, then the CLI default with the model unset. Models outside the bounded tier policy SHALL remain available in discovery order during customization but SHALL NOT be selected automatically merely because they were returned first.

#### Scenario: Claude and Codex produce the standard recommendation
- **WHEN** `claude` exposes `opus` and `sonnet` and `codex` exposes `gpt-5.6-sol` and `gpt-5.6-terra`
- **THEN** the editor recommends `claude` with `opus` for lead, `codex` with `gpt-5.6-sol` for crosscheck, `codex` with `gpt-5.6-terra` for implementor, and `claude` with `sonnet` for tester

#### Scenario: Crosscheck differs from lead when possible
- **WHEN** the selected lead family is Claude and at least one OpenAI GPT candidate is available
- **THEN** the editor recommends a crosscheck candidate whose known family is OpenAI GPT according to crosscheck precedence

#### Scenario: Tester differs from implementor when possible
- **WHEN** the selected implementor family is OpenAI GPT and at least one Claude candidate is available
- **THEN** the editor recommends a non-GPT tester candidate according to tester precedence

#### Scenario: One multi-provider CLI supplies independent families
- **WHEN** only one supported CLI is detected and it exposes both Claude and OpenAI GPT models
- **THEN** the editor SHALL recommend that CLI for paired roles while recommending different model families according to role precedence

#### Scenario: Diversity fallback is explained
- **WHEN** every eligible crosscheck or tester candidate has the paired creator's family or an unknown or unclassified family
- **THEN** the editor applies normal precedence without claiming family diversity and visibly explains which pair could not be diversified

#### Scenario: Multi-provider CLI default has unknown family
- **WHEN** the selected lead or implementor uses a model-less default from OpenCode, Copilot, or Cursor
- **THEN** the editor does not infer a family from the CLI name
- **AND** it claims diversity only if the creator and evaluator later have concrete, recognized, different families

#### Scenario: CLI precedence is evaluated before family precedence
- **WHEN** the user selects an OpenCode default with unknown family for lead, Claude exposes `opus`, and Cursor exposes `gpt-5.7-sol`
- **THEN** Crosscheck recommends Claude with `opus` because Claude precedes Cursor in Crosscheck's CLI order even though Crosscheck prefers the GPT family within a CLI

#### Scenario: Balanced model is unavailable
- **WHEN** a balanced role selects a family whose balanced recognized alias is unavailable but whose flagship recognized alias is available
- **THEN** the editor recommends the flagship alias

#### Scenario: Latest flexible GPT tier match is selected
- **WHEN** a CLI discovers `openai:gpt_5_7_terra`, `gpt-5.6-terra`, and `gpt-5.7-sol-latest`
- **THEN** a balanced GPT role selects the exact identifier `openai:gpt_5_7_terra`
- **AND** a flagship GPT role selects the exact identifier `gpt-5.7-sol-latest`

#### Scenario: GPT marker may be immediately followed by a version
- **WHEN** a CLI discovers `gpt5.7-sol-latest`
- **THEN** the identifier is classified as a GPT flagship tier model with version 5.7

#### Scenario: Tier markers require safe boundaries
- **WHEN** a CLI discovers `solar-model`, `terrain-large`, and `claude-sol`
- **THEN** none of those identifiers is classified as a GPT Sol or Terra tier

#### Scenario: Recognized models are unavailable
- **WHEN** a selected CLI returns models but none match a recognized tier
- **THEN** the editor recommends the CLI default with its model unset
- **AND** preserves every returned model in discovery order for customization

#### Scenario: Model discovery fails for one CLI
- **WHEN** model discovery fails for a detected CLI
- **THEN** the editor keeps that CLI eligible with its model unset, displays the discovery limitation, and continues building the four-role recommendation

#### Scenario: No models are discoverable
- **WHEN** a recommended CLI returns no models
- **THEN** the recommendation identifies that CLI's adapter default and leaves its model unset
