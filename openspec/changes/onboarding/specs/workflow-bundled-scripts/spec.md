## ADDED Requirements

### Requirement: Script step shape and field rules

A workflow step MAY declare `script: <path>` to invoke a script bundled alongside the workflow YAML. A script step SHALL NOT also set any other step-type field (`command`, `prompt`, `agent`, `workflow`, `loop`, or nested `steps`). A script step SHALL NOT set `cli`, `model`, `mode`, or `session`. A script step MAY set `workdir`, `capture`, `capture_stderr`, `skip_if`, `break_if`, `continue_on_failure`, `script_inputs`, and `capture_format`.

#### Scenario: Minimal script step validates
- **WHEN** a step declares `script: detect-adapters.sh` and no other step-type fields
- **THEN** validation succeeds and the step is loaded as a script step

#### Scenario: Script step combined with command rejected
- **WHEN** a step declares both `script: x.sh` and `command: echo hi`
- **THEN** validation fails with an error indicating exactly one step type is allowed

#### Scenario: Script step combined with agent rejected
- **WHEN** a step declares both `script: x.sh` and `agent: planner`
- **THEN** validation fails with an error indicating exactly one step type is allowed

#### Scenario: Agent-only fields rejected on script step
- **WHEN** a script step sets `cli: claude`, `model: opus`, `mode: headless`, or `session: new`
- **THEN** validation fails with an error indicating that field is not valid on script steps

### Requirement: `script:` is a static literal path

The `script:` value SHALL be a literal string at workflow-load time. `{{...}}` interpolation expressions in the `script:` field SHALL be rejected by validation. This eliminates the possibility of runtime-derived values resolving to unintended scripts.

#### Scenario: Interpolation in script path rejected
- **WHEN** a step declares `script: '{{user_choice}}.sh'`
- **THEN** validation fails with an error indicating that `script:` must be a static path

### Requirement: Path resolution and traversal protection

The runner SHALL resolve `script:` paths relative to the workflow YAML's containing directory. Validation SHALL reject:
- absolute paths;
- paths that contain `..` segments after normalization;
- paths that, after symlink resolution, refer to a target outside the workflow's directory.

#### Scenario: Absolute path rejected
- **WHEN** a step declares `script: /etc/passwd`
- **THEN** validation fails with an error indicating absolute paths are not allowed

#### Scenario: Parent-directory traversal rejected
- **WHEN** a step declares `script: ../../other.sh`
- **THEN** validation fails with an error indicating path traversal is not allowed

#### Scenario: Symlink escape rejected
- **WHEN** the workflow contains `script.sh` that is a symlink to `../outside/evil.sh`
- **THEN** validation fails with an error indicating the script must resolve inside the workflow's directory

#### Scenario: Permitted relative path inside workflow directory
- **WHEN** a step declares `script: helpers/detect.sh` and that path resolves to a regular file inside the workflow's directory
- **THEN** validation succeeds

### Requirement: Embedded vs on-disk script resolution

When the containing workflow is part of the embedded builtin set, the runner SHALL resolve `script:` references only against the embedded namespace and SHALL NOT fall back to user-authored workflows under `.agent-runner/workflows/`. When the containing workflow is loaded from disk, the runner SHALL read the script from disk relative to the workflow file's directory.

#### Scenario: Embedded script resolves within embedded namespace
- **WHEN** the embedded workflow `onboarding:setup-agent-profile` declares `script: detect-adapters.sh`
- **THEN** the runner reads the script from the embedded `onboarding/detect-adapters.sh` and executes it

#### Scenario: Embedded script does not fall back to user directory
- **WHEN** the embedded workflow `onboarding:setup-agent-profile` declares `script: detect-adapters.sh` and a file `.agent-runner/workflows/onboarding/detect-adapters.sh` exists on the user's disk
- **THEN** the runner uses the embedded script, not the user file

#### Scenario: On-disk workflow reads script from disk
- **WHEN** a workflow loaded from `.agent-runner/workflows/foo/main.yaml` declares `script: helper.sh`
- **THEN** the runner executes `.agent-runner/workflows/foo/helper.sh`

### Requirement: Execution mechanism and lifecycle

The runner SHALL execute the script directly via its shebang (or platform-equivalent execution path). Embedded scripts SHALL be materialized to a temporary file with mode `0o700` in the OS temp directory before execution; user-authored scripts execute from disk in place. The runner SHALL clean up materialized temp files on workflow completion, on failure, and on signal-driven termination (SIGINT, SIGTERM).

#### Scenario: Embedded script materialized with restrictive permissions
- **WHEN** an embedded script is about to execute
- **THEN** the runner writes it to a temp file with mode `0o700` and exec's that path

#### Scenario: Temp file removed after successful run
- **WHEN** an embedded script executes and the workflow completes
- **THEN** the temp file no longer exists on disk

#### Scenario: Temp file removed after script failure
- **WHEN** an embedded script exits non-zero
- **THEN** the temp file is removed before the runner records the step failure

#### Scenario: Temp file removed on signal
- **WHEN** the runner receives SIGINT while an embedded script is executing
- **THEN** the temp file is removed before process exit

### Requirement: Working directory

A script SHALL execute with its working directory set to the step's `workdir` if declared, otherwise to the runner's working directory. This matches the rule already specified for shell steps.

#### Scenario: Step workdir applied
- **WHEN** a script step has `workdir: ./tools` and the runner is invoked from `/path/to/project`
- **THEN** the script executes with cwd `/path/to/project/tools`

#### Scenario: Default cwd is runner cwd
- **WHEN** a script step has no `workdir` and the runner is invoked from `/path/to/project`
- **THEN** the script executes with cwd `/path/to/project`

### Requirement: Exit-code semantics

Exit code `0` SHALL be treated as success; any non-zero exit code SHALL be treated as failure. The success/failure result composes with existing `continue_on_failure` and `break_if: failure` semantics without modification.

#### Scenario: Successful script
- **WHEN** a script exits with code 0
- **THEN** the step succeeds

#### Scenario: Failed script
- **WHEN** a script exits with code 1
- **THEN** the step fails

#### Scenario: Failed script with continue_on_failure
- **WHEN** a script step has `continue_on_failure: true` and the script exits with code 1
- **THEN** the workflow records the failure and continues to the next step

### Requirement: Declared script inputs via stdin

A script step MAY declare a `script_inputs:` map of `name: value` entries. Values support `{{...}}` interpolation against workflow params and captures. When `script_inputs:` is set, the runner SHALL JSON-encode the resolved map and pipe it to the script's stdin. When `script_inputs:` is absent, the runner SHALL close the script's stdin and SHALL NOT pipe any other workflow state. Scripts SHALL NOT receive workflow params or captures other than those explicitly declared.

#### Scenario: Script receives only declared inputs
- **WHEN** a script step declares `script_inputs: {adapter: '{{user_choice.adapter}}', model: '{{user_choice.model}}'}` and the captured `user_choice` is `{adapter: claude, model: opus, scope: global}`
- **THEN** the script's stdin contains exactly the JSON `{"adapter":"claude","model":"opus"}` and `scope` is not visible to the script

#### Scenario: No script_inputs means closed stdin
- **WHEN** a script step has no `script_inputs:` field
- **THEN** the script's stdin is closed (EOF on first read) and no workflow state is piped to it

### Requirement: Capture format

A script step MAY declare `capture_format: text` (default) or `capture_format: json`. With `text`, stdout is captured as a string, matching existing shell-step capture behavior. With `json`, stdout is parsed as JSON after the script exits; the parsed value SHALL be a list of strings or a map of strings (matching the typed-capture set defined in `output-capture`). `capture_format` SHALL be rejected when `capture:` is not set.

#### Scenario: Default capture is text
- **WHEN** a script step declares `capture: out` and no `capture_format:`
- **THEN** the captured value `out` is the stdout string

#### Scenario: JSON capture parses to list
- **WHEN** a script step declares `capture: adapters` and `capture_format: json`, and the script writes `["claude","codex"]` to stdout
- **THEN** the captured value `adapters` is the list `["claude", "codex"]`

#### Scenario: JSON capture parses to map
- **WHEN** a script step declares `capture: profile` and `capture_format: json`, and the script writes `{"adapter":"claude","model":"opus"}` to stdout
- **THEN** the captured value `profile` is the map `{adapter: "claude", model: "opus"}`

#### Scenario: capture_format without capture rejected
- **WHEN** a script step declares `capture_format: json` but no `capture:` field
- **THEN** validation fails with an error indicating that `capture_format` requires `capture`

### Requirement: JSON capture validation

When `capture_format: json` is set, the runner SHALL fail the step with a descriptive error if any of the following holds:
- stdout is not valid UTF-8;
- stdout exceeds 1 MiB;
- stdout fails to parse as JSON;
- stdout parses to a value that is not a list of strings or a map of strings.

#### Scenario: Invalid JSON
- **WHEN** stdout is `not json` and `capture_format: json` is set
- **THEN** the step fails with an error indicating the stdout could not be parsed as JSON

#### Scenario: Non-UTF-8 stdout
- **WHEN** stdout contains bytes that are not valid UTF-8 and `capture_format: json` is set
- **THEN** the step fails with an error indicating the stdout was not valid UTF-8

#### Scenario: Stdout exceeds size cap
- **WHEN** stdout exceeds 1 MiB and `capture_format: json` is set
- **THEN** the step fails with an error indicating the stdout exceeded the 1 MiB limit

#### Scenario: JSON of unsupported shape
- **WHEN** stdout parses to a number, a boolean, a list of objects, or a map whose values are not all strings, and `capture_format: json` is set
- **THEN** the step fails with an error indicating the parsed JSON must be a list of strings or a map of strings

### Requirement: Stdout tee behavior

The runner SHALL tee the script's stdout to the terminal in real time regardless of `capture_format`. JSON parsing happens after the script exits and SHALL NOT delay terminal output.

#### Scenario: Tee with capture_format=text
- **WHEN** a script writes lines to stdout with `capture: out` (default text)
- **THEN** the lines appear on the terminal in real time and the captured string contains the same content

#### Scenario: Tee with capture_format=json
- **WHEN** a script writes a JSON document to stdout with `capture: out` and `capture_format: json`
- **THEN** the raw JSON text appears on the terminal in real time, and after exit the captured value is the parsed JSON
