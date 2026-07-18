## Why

The OpenSpec `change2` workflow needs a minimal disposable change to verify that its full lifecycle can run end to end. A documentation-only artifact provides an observable result without affecting Agent Runner behavior.

## What Changes

- Add `docs/workflow-v2-hello.md` with the heading `Hello, workflow v2` and one sentence stating that the file exists solely to exercise the OpenSpec `change2` lifecycle end to end.
- Do not add page metadata, update documentation indexes, or modify any other files outside the OpenSpec lifecycle artifacts.

## Capabilities

### New Capabilities

- `workflow-v2-smoke-document`: Defines the disposable documentation artifact used to validate the OpenSpec `change2` lifecycle.

### Modified Capabilities

None.

## Technical Approach

Create one standalone Markdown file containing only the requested heading and explanatory sentence. Use the normal OpenSpec artifacts to carry the change through proposal, specs, design, tasks, implementation, acceptance, and archive.

## Out of Scope

- Code or runtime behavior changes.
- Dependency or API changes.
- Updates to existing documentation, navigation, or metadata.
- General-purpose workflow testing infrastructure.

## Impact

The product change affects only the new `docs/workflow-v2-hello.md` file. The change also creates the expected OpenSpec lifecycle artifacts for the new smoke-document capability; it does not affect code, APIs, dependencies, or runtime systems.
