## MODIFIED Requirements

### Requirement: Engine-aware step matching

Agent Runner SHALL ask the engine whether it manages a step, passing the workflow step ID. It SHALL pass the step ID to the engine hooks (`EnrichPrompt`, `ValidateStep`) only for managed steps. There is no explicit mapping field on steps: by convention, step IDs match engine-managed entity IDs. An engine MAY also map step IDs to entities through its own opaque engine configuration.

#### Scenario: Engine receives step ID in hooks
- **WHEN** Agent Runner calls `EnrichPrompt` or `ValidateStep`
- **THEN** Agent Runner passes the current step's ID, and the call happens only because the engine reported that it manages that step

#### Scenario: Engine ignores unrecognized step ID
- **WHEN** the engine reports that it does not manage a step ID
- **THEN** Agent Runner calls no enrichment or validation hook for that step (no-op)

#### Scenario: Engine config maps a step to entities
- **WHEN** an engine's configuration maps step ID `plan` to several managed entities
- **THEN** the engine reports `plan` as managed and handles those entities in its hooks, without any step-level schema field
