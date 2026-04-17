- [x] Live TUI as the workflow console (`tasks/live-tui-console.md`)
- [ ] Lockout guards and live-run navigation (`tasks/lockout-and-live-navigation.md`)

Task 1 delivers most of the change; Task 2 completes it. Do NOT mark the change complete after Task 1 — the `Cursor auto-follows the active step` and `Detail-pane tail-follow` requirements in `specs/live-run-view/spec.md`, plus the `view-run` and `list-runs` spec deltas, are owned by Task 2. The change is complete only when both boxes are checked and `openspec validate --type change live-run-view` passes with all scenarios implemented.
