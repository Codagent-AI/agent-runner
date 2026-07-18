# Task: Add Workflow V2 Smoke Document

## Goal

Add the disposable documentation artifact that proves the OpenSpec `change2` lifecycle can carry a minimal change end to end, without affecting Agent Runner behavior or the published documentation structure.

## Background

You MUST read these files before starting:

- `proposal.md` → **Why**, **What Changes**, and **Out of Scope** for the approved motivation, product scope, and explicit exclusions.
- `design.md` → **Approach**, **Use a standalone documentation artifact**, **Use command-level acceptance checks**, and **Risks / Trade-offs** for the implementation shape, exact-content verification approach, and scope-inspection constraints.
- `specs/workflow-v2-smoke-document/spec.md` → **Requirement: Disposable workflow v2 smoke document**, **Scenario: Exact smoke document is added**, and **Scenario: Change remains isolated** for the complete requirement and acceptance contract.

Create `docs/workflow-v2-hello.md` as the only implementation artifact. Although `docs/AGENTS.md` normally requires public documentation metadata and index integration, the approved design intentionally overrides those conventions for this disposable smoke artifact. The file must contain only the specified H1 heading and sentence, separated by one blank line; do not add frontmatter, navigation, index entries, commentary, tests, or supporting infrastructure.

This is a documentation-only configuration change. Do not modify code, runtime behavior, APIs, dependencies, existing documentation, documentation navigation or metadata, or general-purpose workflow testing infrastructure. Verification must use an exact-content comparison and repository diff/status inspection, as specified by the design, rather than adding automated test code.

## Done When

- `docs/workflow-v2-hello.md` exists and matches the exact Markdown content in `specs/workflow-v2-smoke-document/spec.md`, including the H1 text, one intervening blank line, sentence wording, whitespace, and absence of extra content.
- The "Exact smoke document is added" scenario in `specs/workflow-v2-smoke-document/spec.md` passes under a deterministic exact-content comparison.
- The "Change remains isolated" scenario passes: repository diff and status inspection confirm that implementation changes are limited to `docs/workflow-v2-hello.md`, apart from the expected OpenSpec lifecycle artifacts for this change.
- No frontmatter, documentation index or navigation updates, code changes, dependency changes, tests, or general-purpose workflow testing infrastructure are added.
