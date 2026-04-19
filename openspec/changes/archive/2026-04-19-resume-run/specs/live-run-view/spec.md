## MODIFIED Requirements

### Requirement: Detail-pane tail-follow

While output is streaming into the selected step's detail pane, the viewport SHALL remain pinned at the tail (newest content visible) unless the user has manually scrolled up. Scrolling up (via `k` or mouse wheel) SHALL pause tail-follow. Pressing `t` SHALL jump the viewport to the tail and re-engage tail-follow. `End` and uppercase `G` SHALL NOT be bound to this action (or to anything else).

#### Scenario: Streaming output auto-tails
- **WHEN** new bytes arrive for the currently selected step and tail-follow is engaged
- **THEN** the detail pane viewport stays at the bottom, showing the newest content

#### Scenario: User scroll pauses tail-follow
- **WHEN** the user scrolls the detail pane up (via `k` or mouse-wheel-up)
- **THEN** tail-follow is paused; subsequent output does not move the viewport

#### Scenario: t re-engages tail-follow
- **WHEN** the user presses `t` with tail-follow paused
- **THEN** the viewport jumps to the bottom of the output and tail-follow resumes

#### Scenario: End and G are not bound
- **WHEN** the user presses `End` or uppercase `G`
- **THEN** nothing happens (neither key is bound to tail-follow or any other action)
