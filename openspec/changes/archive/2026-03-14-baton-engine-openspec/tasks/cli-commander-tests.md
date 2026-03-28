# Task: Migrate CLI to commander and add test coverage

## Goal

Replace baton's hand-rolled argument parsing with the `commander` npm package, reorganize into one file per command under `src/commands/`, and add unit test coverage for all existing code. Same behavior, cleaner structure, tested.

## Background

Baton currently has 4 source files with no tests:
- `src/index.ts` — CLI entry point with hand-rolled `parseArgs` function and a `main` function that dispatches to `run` or `validate`
- `src/schema.ts` — Zod schemas for `Step`, `Param`, and `Workflow`
- `src/loader.ts` — `loadWorkflow` (reads YAML, parses with Zod) and `interpolateParams` (replaces `{{param}}` placeholders)
- `src/runner.ts` — `runWorkflow` loop that executes steps by spawning `claude` or shell processes

The project uses Bun as its runtime and test runner (`bun test`). Tests go in `test/` (excluded from tsconfig compilation). Dependencies: `zod`, `yaml`. Dev: `@biomejs/biome`, `@types/bun`.

Add `commander` as a dependency. Create `src/commands/run.ts` (registers `baton run <workflow> [params...]` with `--from <step>` option), `src/commands/validate.ts` (registers `baton validate <workflow>`), and `src/commands/index.ts` (re-exports register functions). Rewrite `src/index.ts` to create a Commander program, register commands, and parse `process.argv` — replacing the hand-rolled `parseArgs` and `main` function.

Follow the commander pattern from agent-gauntlet (`/Users/pcaplan/paul/agent-gauntlet`): each command file exports a `register*Command(program: Command)` function.

Add unit tests: `test/schema.test.ts` (Zod schema validation — valid workflows parse, invalid ones reject, defaults apply), `test/loader.test.ts` (`loadWorkflow` with fixture YAML files, `interpolateParams` with valid/missing params), `test/runner.test.ts` (runner behavior with mocked process spawning — shell steps, agent steps, signal handling, session resume). Use `bun test` with `describe`/`it`/`expect`. Runner tests need to mock `Bun.spawn` since the runner spawns real processes.

This is a pure refactoring task with no behavioral changes — it delivers the CLI structure and test foundation needed for subsequent work.

## Done When

- `baton run` and `baton validate` work identically to before
- `baton --help` shows usage with all commands
- `src/commands/` directory exists with one file per command
- All tests pass via `bun test`
- Test coverage exists for schema validation, loader, interpolation, and runner step execution
