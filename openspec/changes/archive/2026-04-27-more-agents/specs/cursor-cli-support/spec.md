## REMOVED Requirements

### Requirement: Cursor interactive mode rejected at runtime

**Reason**: Cursor interactive mode is now supported. The cross-cutting "Adapter mode coverage" requirement in `cli-adapter` mandates that every registered adapter support both interactive and headless modes.

**Migration**: Workflows declaring `cli: cursor` with `default_mode: interactive` (or interactive steps using a cursor profile) no longer fail at runtime; they spawn a real interactive Cursor session. Tooling or tests that asserted on the rejection error message (or on the adapter implementing `cli.InteractiveRejector`) must be updated.
