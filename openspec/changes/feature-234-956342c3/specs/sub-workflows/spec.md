## ADDED Requirements

### Requirement: Engine inheritance

A sub-workflow that declares no `engine` block SHALL run under the engine in effect in its parent workflow scope, if there is one. A sub-workflow that declares its own `engine` block SHALL run under that engine instead, for its own steps and for any descendants that declare no engine. A sub-workflow inherits only the engine, not the parent's params: engine hooks for the child's steps SHALL receive the child's own params.

#### Scenario: Child without engine inherits parent engine
- **WHEN** a parent workflow declares the openspec engine and calls a sub-workflow that declares no engine and has a step with ID `proposal`
- **THEN** that child step is enriched and validated by the parent's engine, using the child's `change_name` param

#### Scenario: Child declares its own engine
- **WHEN** a parent workflow declares one engine and calls a sub-workflow that declares a different engine block
- **THEN** the child's steps run under the child's engine

#### Scenario: No engine anywhere
- **WHEN** neither the parent nor the sub-workflow declares an engine
- **THEN** the sub-workflow's steps run with no engine

#### Scenario: Inherited engine in a child without the change param
- **WHEN** an inherited openspec engine reaches a child workflow whose params do not include the change param, and the child's steps are unmanaged
- **THEN** the child's steps run normally and no engine error occurs
