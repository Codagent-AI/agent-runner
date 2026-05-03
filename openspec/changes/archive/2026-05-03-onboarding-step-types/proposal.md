## Why

The current onboarding flow stops after agent profile setup, so new users do not see the core workflow step primitives in action before being asked to use Agent Runner for real work. Implementing the documented Phase 3 demo now extends onboarding from setup into practical orientation using the workflow engine itself.

## What Changes

- Add a built-in `step-types-demo` onboarding sub-workflow that demonstrates UI, interactive, headless, shell, and capture behavior in sequence.
- Update the top-level `onboarding:welcome` workflow so the continue path runs agent profile setup and then the step types demo before recording onboarding completion.
- Keep the demo non-destructive and instructional: it should teach step format and runtime behavior without modifying the user's project.
- Use existing workflow primitives and embedded workflow packaging; no bespoke onboarding state or new runtime path is introduced.

## Capabilities

### New Capabilities

- None.

### Modified Capabilities

- `onboarding-workflow`: The embedded onboarding namespace and top-level welcome flow now include the Phase 3 step types demo sub-workflow, and successful onboarding completion requires that demo to complete after setup.

## Impact

- Affected built-in workflow files under `workflows/onboarding/`, including a new `step-types-demo.yaml` and updates to `welcome.yaml`.
- Affected onboarding requirements in `openspec/specs/onboarding-workflow/spec.md` via a delta spec for this change.
- Affected embedded workflow validation/tests to ensure the new sub-workflow is packaged, resolves from the onboarding namespace, and remains non-destructive.
- No expected changes to public CLI flags, config schema, external dependencies, or core executor semantics.
