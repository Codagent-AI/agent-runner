## MODIFIED Requirements

### Requirement: `debug --show-workflow <ref>` prints resolved YAML

The `agent-runner debug --show-workflow <ref>` command SHALL print the YAML for the named workflow ref to stdout and exit 0 on success. A version-free builtin logical ref (`<namespace>:<logical-name>`) SHALL resolve to the highest embedded version. An exact embedded ref (`builtin:<namespace>/<versioned-file>`) SHALL read that exact historical version. A namespaced version-bearing shorthand such as `core:finalize-pr-v1.0` SHALL NOT act as a historical selector; callers SHALL use the exact `builtin:` ref instead.

Non-builtin refs (relative paths, absolute paths, `~`-prefixed paths) SHALL be resolved from disk and MAY identify an exact historical version. The output SHALL be the YAML for the **named ref only** — composed sub-workflow references SHALL NOT be inlined, expanded, or followed; they SHALL appear in the output exactly as they appear in the source. The output SHALL be the bytes as embedded or stored, with no normalization, reformatting, or comment stripping. Read-only acceptance of an exact path or embedded ref MUST NOT make that historical version launchable as a new run.

#### Scenario: Builtin ref returns embedded YAML
- **WHEN** `agent-runner debug --show-workflow core:finalize-pr` is invoked and versions `v1.0` and `v2.0` are embedded
- **THEN** the bytes for embedded `workflows/core/finalize-pr-v2.0.yaml` are printed to stdout verbatim and the command exits 0

#### Scenario: Exact builtin ref returns historical embedded YAML
- **WHEN** `agent-runner debug --show-workflow builtin:core/finalize-pr-v1.0.yaml` is invoked
- **THEN** the bytes for that exact embedded version are printed to stdout verbatim and the command exits 0

#### Scenario: Namespaced version shorthand is not a historical selector
- **WHEN** `agent-runner debug --show-workflow core:finalize-pr-v1.0` is invoked
- **THEN** the command exits non-zero and instructs the user to provide exact ref `builtin:core/finalize-pr-v1.0.yaml`

#### Scenario: On-disk ref returns file bytes
- **WHEN** `agent-runner debug --show-workflow ./my-workflow-v1.0.yaml` is invoked and the file exists
- **THEN** the file's bytes are printed to stdout verbatim and the command exits 0

#### Scenario: Sub-workflow references preserved
- **WHEN** the requested workflow contains `workflow: plan-change-v2.0.yaml` references in its YAML
- **THEN** those reference lines appear in the output unmodified; no sub-workflow content is inlined

#### Scenario: Unknown ref
- **WHEN** `agent-runner debug --show-workflow` is invoked with a ref that resolves to neither an embedded builtin nor an existing on-disk file
- **THEN** the command exits non-zero and prints an error to stderr naming the missing ref

#### Scenario: Malformed ref string
- **WHEN** `agent-runner debug --show-workflow` is invoked with a ref string that cannot be parsed (e.g. empty, contains illegal characters)
- **THEN** the command exits non-zero and prints a parse error to stderr

#### Scenario: Output is unnormalized
- **WHEN** the resolved YAML contains comments, blank lines, or non-canonical whitespace
- **THEN** those bytes appear in the output unchanged
