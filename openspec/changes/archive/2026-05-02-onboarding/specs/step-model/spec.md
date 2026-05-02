## ADDED Requirements

### Requirement: `model` field rejected on UI steps

The `model` field SHALL NOT be valid on `mode: ui` steps. UI steps are not agent steps; they have no model concept. Validation SHALL fail at workflow-load time when a UI step sets `model`.

#### Scenario: UI step with model field
- **WHEN** a step has `mode: ui` and sets `model: opus`
- **THEN** validation fails with an error indicating that `model` is not valid on UI steps

### Requirement: `cli` field rejected on UI steps

The `cli` field SHALL NOT be valid on `mode: ui` steps. UI steps are not agent steps; they have no CLI adapter. Validation SHALL fail at workflow-load time when a UI step sets `cli`.

#### Scenario: UI step with cli field
- **WHEN** a step has `mode: ui` and sets `cli: claude`
- **THEN** validation fails with an error indicating that `cli` is not valid on UI steps

### Requirement: `model` field rejected on script steps

The `model` field SHALL NOT be valid on `script:` steps. Script steps are not agent steps; the model concept does not apply to a bundled script. Validation SHALL fail at workflow-load time when a script step sets `model`.

#### Scenario: Script step with model field
- **WHEN** a step declares `script: detect.sh` and sets `model: opus`
- **THEN** validation fails with an error indicating that `model` is not valid on script steps

### Requirement: `cli` field rejected on script steps

The `cli` field SHALL NOT be valid on `script:` steps. Script steps are not agent steps; they do not invoke a CLI adapter. Validation SHALL fail at workflow-load time when a script step sets `cli`.

#### Scenario: Script step with cli field
- **WHEN** a step declares `script: detect.sh` and sets `cli: codex`
- **THEN** validation fails with an error indicating that `cli` is not valid on script steps
