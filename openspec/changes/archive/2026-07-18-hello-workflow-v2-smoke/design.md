## Context

This change is a disposable end-to-end validation of the OpenSpec `change2` lifecycle. The approved proposal and `workflow-v2-smoke-document` specification require one new Markdown file with exact content while excluding code, runtime, dependency, existing-documentation, metadata, navigation, and test-infrastructure changes.

Normal public documentation uses frontmatter and index integration, but those conventions are intentionally omitted because the smoke artifact must contain only the specified heading and sentence.

## Goals / Non-Goals

**Goals:**

- Add the exact documentation artifact required by the specification.
- Verify its content and the isolation of the resulting change deterministically.
- Keep implementation and rollback trivial.

**Non-Goals:**

- Integrating the file into published documentation navigation or metadata.
- Changing code, runtime behavior, APIs, dependencies, or existing documentation.
- Adding automated tests or general-purpose workflow testing infrastructure.

## Approach

Create `docs/workflow-v2-hello.md` as a standalone Markdown file containing exactly the specified H1 heading, one blank line, and the specified sentence.

Verify implementation with:

1. An exact-content comparison that detects wording, whitespace, or extra-content differences.
2. Git diff and status inspection confirming that implementation changed only the new documentation file and expected OpenSpec lifecycle artifacts.

No application components, interfaces, data flows, or runtime error paths are involved.

## Decisions

### Use a standalone documentation artifact

Add the requested file directly without frontmatter, index updates, navigation changes, or supporting infrastructure. This keeps the smoke change isolated and satisfies the exact-content requirement.

**Alternatives considered:**

- Add normal documentation metadata and index integration: rejected because it would violate the exclusive-content and isolation requirements.
- Reuse or modify an existing document: rejected because it would affect existing documentation and make rollback less isolated.

### Use command-level acceptance checks

Use exact-content comparison plus Git diff inspection instead of adding tests. This provides deterministic verification without introducing persistent testing machinery for a disposable fixture.

**Alternatives considered:**

- Manual inspection alone: rejected because subtle whitespace or scope drift could be missed.
- Automated test code: rejected as disproportionate and explicitly outside the approved scope.

## Risks / Trade-offs

- [The file does not follow normal public-doc metadata conventions] → Keep it intentionally unindexed and treat it solely as a disposable workflow artifact.
- [Exact-content validation is sensitive to whitespace] → Use byte-for-byte comparison so whitespace differences fail visibly.
- [Unrelated working-tree changes could complicate scope inspection] → Restrict diff inspection to the expected paths and separately review repository status.

## Migration Plan

Add the new file, run the exact-content and diff checks, and continue the OpenSpec lifecycle. No deployment or data migration is required.

Rollback consists of removing the newly added documentation file and reverting only this change’s OpenSpec artifacts.

## Open Questions

None.
