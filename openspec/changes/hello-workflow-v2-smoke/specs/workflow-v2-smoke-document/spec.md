## ADDED Requirements

### Requirement: Disposable workflow v2 smoke document

The repository SHALL contain `docs/workflow-v2-hello.md` with only the following H1 heading and sentence, separated by one blank line:

```markdown
# Hello, workflow v2

This file exists solely to exercise the OpenSpec change2 lifecycle end to end.
```

The change MUST NOT modify code, runtime behavior, APIs, dependencies, existing documentation, documentation navigation or metadata, or general-purpose workflow testing infrastructure.

#### Scenario: Exact smoke document is added

- **WHEN** the change is applied
- **THEN** `docs/workflow-v2-hello.md` exists with exactly the specified heading, blank line, and sentence and contains no other content

#### Scenario: Change remains isolated

- **WHEN** the change is applied
- **THEN** no code, runtime behavior, API, dependency, existing documentation, documentation navigation or metadata, or general-purpose workflow testing infrastructure is modified
