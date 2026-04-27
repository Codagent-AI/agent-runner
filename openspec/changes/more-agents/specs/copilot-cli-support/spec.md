## REMOVED Requirements

### Requirement: Copilot interactive mode rejected at runtime

**Reason**: Copilot interactive mode is now supported. The cross-cutting "Adapter mode coverage" requirement in `cli-adapter` mandates that every registered adapter support both interactive and headless modes.

**Migration**: Workflows declaring `cli: copilot` with `default_mode: interactive` (or interactive steps using a copilot profile) no longer fail at runtime; they spawn a real interactive Copilot session. Tooling or tests that asserted on the rejection error message must be updated.
