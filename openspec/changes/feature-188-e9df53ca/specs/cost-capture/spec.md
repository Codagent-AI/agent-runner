## ADDED Requirements

### Requirement: Claude step cost covers subagent spend

A Claude step's `estimated_api_cost_usd` SHALL continue to come only from the step's attributed share of Claude's reported `total_cost_usd`, which already includes subagent calls priced by Claude Code at each subagent model's own rates. Collecting subagent tokens SHALL NOT add to, recompute, or otherwise change the step's cost. Agent Runner SHALL NOT price subagent allocations from token counts.

Claude's telemetry does not report cost for a main-thread or subagent allocation on its own. Its per-model cost is session-cumulative and does not separate threads that use the same model. Allocation cost for Claude main-thread and subagent allocations SHALL therefore be recorded as unavailable with a reason, never zero, and the step's full reported cost SHALL remain the only Claude cost evidence.

#### Scenario: Step cost unchanged by subagent collection
- **WHEN** a Claude step spawns subagents and its attributed `total_cost_usd` delta is $5.77
- **THEN** the step's `estimated_api_cost_usd` is $5.77 and the run-level cost total includes $5.77 once

#### Scenario: Subagent allocation cost is unavailable
- **WHEN** a Claude step's subagents run on the same model as the main thread or on a different one
- **THEN** each subagent allocation's cost is unavailable with a reason and the step's full cost is still reported

#### Scenario: Main-thread allocation cost is unavailable
- **WHEN** a Claude step's measurement contains a main-thread allocation
- **THEN** that allocation's cost is unavailable with a reason, not zero, and the full reported cost remains at attempt scope

#### Scenario: No token-based pricing
- **WHEN** a Claude step's subagent allocation has token counts but no Claude-reported allocation cost
- **THEN** no USD value is computed for that allocation from its tokens
