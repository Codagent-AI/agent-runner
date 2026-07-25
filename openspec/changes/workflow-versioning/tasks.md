Tasks are strict sequential prerequisites. Complete and validate each task, including its stated intermediate compatibility contract, before starting the next; do not execute these tasks in parallel.

- [x] Build the Workflow Version Catalog (`tasks/01-build-version-catalog.md`) — no prerequisite
- [x] Enforce and Migrate Versioned Definitions (`tasks/02-enforce-versioned-definitions.md`) — requires task 01
- [x] Resolve Logical Workflow Launches (`tasks/03-resolve-logical-launches.md`) — requires task 02
- [x] Present Latest Logical Workflows (`tasks/04-present-latest-workflows.md`) — requires task 03
- [x] Preserve Recorded and Referenced Versions (`tasks/05-preserve-recorded-versions.md`) — requires task 04
