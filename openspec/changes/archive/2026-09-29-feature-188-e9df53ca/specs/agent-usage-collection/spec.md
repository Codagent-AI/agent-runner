## ADDED Requirements

### Requirement: Main-thread and subagent usage allocations

When a step's usage comes from more than one thread of the same invocation, such as a Claude main thread and its subagents, the step's usage record SHALL keep each source as a distinct allocation with its own observed model identity and token categories. The step's attempt-level token categories and canonical totals SHALL equal the sum over its allocations, with each token counted exactly once. Allocation rows SHALL never be summed again alongside the attempt totals. When any allocation is partial or unavailable, the attempt-level values SHALL retain the known subtotal and the record's completeness SHALL be `partial`. It SHALL NOT be presented as complete.

#### Scenario: Attempt totals include subagents
- **WHEN** a Claude step's main thread reports 1,000 input, 50,000 cache-read, 2,000 cache-write, and 500 output tokens, and its single subagent reports 200 input, 10,000 cache-read, 1,000 cache-write, and 300 output tokens
- **THEN** the step's usage record reports 1,200 input, 60,000 cache-read, 3,000 cache-write, and 800 output tokens, and its canonical totals are input 64,200, output 800, and overall 65,000
- **AND** the record separately lists the main-thread allocation and the subagent allocation with their own counts and observed models

#### Scenario: Partial subagent collection keeps the known subtotal
- **WHEN** a Claude step's main-thread usage is collected, one subagent's usage is collected, and a second subagent's transcript is missing
- **THEN** the step's token categories and canonical totals equal the main thread plus the collected subagent, and the record's completeness is `partial` with a reason naming the missing subagent evidence

#### Scenario: Main thread unavailable, subagents collected
- **WHEN** a Claude step's stdout has no `result` usage event but its spawned subagents' transcripts are readable
- **THEN** the main-thread allocation is unavailable with reason `no-usage-event`, subagent usage is retained, and the step's usage completeness is `partial`
