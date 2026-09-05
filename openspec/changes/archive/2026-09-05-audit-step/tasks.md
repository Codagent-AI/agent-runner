# Tasks: audit-step

Tasks are dependency ordered. Each task consumes the stable artifacts and interfaces established above it; the final task is the first point at which the complete seven-stage workflow and cross-cutting CLI journeys must pass end to end.

- [x] [Persist execution-session metrics and conservative Git checkpoints](tasks/01-execution-evidence.md)
- [x] [Add the development-only linked audit lifecycle and replay commands](tasks/02-audit-lifecycle.md)
- [x] [Prepare bounded evidence and produce validated value observations](tasks/03-value-audit.md)
- [x] [Verify Runner defects, publish deduplicated GitHub issues, and assemble local reports](tasks/04-correctness-publication.md)
- [x] [Import Google credentials, deliver Sheets rows, and prove complete CLI journeys](tasks/05-sheets-delivery.md)
