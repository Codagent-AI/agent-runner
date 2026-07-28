---
title: Built-In Workflows
group: Reference
order: 7
description: Embedded workflow namespaces and workflow purposes.
---

# Built-In Workflows

Agent Runner embeds namespaced, versioned workflow files into the binary. Use
the TUI or `agent-runner -list` to browse one version-neutral row per logical
workflow.

Launch built-ins with logical names such as `openspec:change`; Agent Runner
selects the latest embedded version. Exact embedded references such as
`builtin:openspec/change-v2.0.yaml` are available through read-only debug
inspection, not fresh execution.

## Namespaces

| Namespace | Description |
| --- | --- |
| `core` | General-purpose sub-workflows invoked by other workflows and skills. |
| `openspec` | Change planning and implementation using OpenSpec. |
| `spec-driven` | Spec-driven planning and implementation workflows with no OpenSpec dependency. |
| `onboarding` | Guided tours and demos for new users. |

## Core

| Workflow | Purpose |
| --- | --- |
| `core:accept-change` | Run the shared human acceptance, refinement, and targeted re-testing phases for a structured change. |
| `core:debug` | Debug a failed Agent Runner run and optionally file an issue. |
| `core:define-change` | Run the shared proposal, specification, design, test-plan, and approach-review phases. |
| `core:finalize-pr` | Push PR, wait for CI, fix failures, and repeat until green, with a maximum of three fix cycles. |
| `core:implement-change` | Run the shared task implementation, validation, draft-PR, and acceptance-preparation phases. |
| `core:implement-task` | Implement a single task with an agent step followed by a validator retry loop. |
| `core:plan-change` | Run the shared definition validation, task planning, and task-review phases. |
| `core:review-proposal` | Run adversarial proposal review, lead response, and up to three discussion rounds. |
| `core:review-tasks` | Independently review and autonomously correct a structured task plan. |
| `core:run-validator` | Run Agent Validator with a counted retry loop and fix-on-failure step. |

Some `core:*` workflows are hidden from normal browsing because they are intended to be invoked by higher-level workflows. The shared change-lifecycle workflows accept an artifact directory and validation instructions; the OpenSpec and spec-driven namespaces provide those backend-specific values.

## OpenSpec

| Workflow | Purpose |
| --- | --- |
| `openspec:change` | Define, plan, implement, validate, prepare evidence, support human acceptance, and finalize a feature change. |
| `openspec:implement-change` | Implement reviewed task files, validate the result, open a draft PR, and prepare test-plan-driven acceptance evidence. |
| `openspec:plan-change` | Validate an approved proposal, specs, design, and test plan, then create and review implementation tasks. |
| `openspec:scaffold` | Bootstrap a brand new OpenSpec project, configure validation, and optionally publish it to GitHub. |
| `openspec:simple-change` | Run a quick plan, implement, and validate workflow for small changes. |

The `openspec:change` workflow is a full development flow: it collaboratively defines the proposal,
specifications, design, and test plan; autonomously plans and implements reviewed tasks; validates and
prepares acceptance evidence; supports human review; and continues through PR finalization.
`openspec:simple-change` keeps planning and implementation inline for smaller changes.
`openspec:scaffold` runs `openspec init --tools none` so the project has OpenSpec directories without
installing OpenSpec agent skills or slash commands.

## Spec-Driven

| Workflow | Purpose |
| --- | --- |
| `spec-driven:change` | Define, plan, implement, prepare acceptance evidence, support human acceptance, and finalize a feature change without OpenSpec. |
| `spec-driven:implement-change` | Implement reviewed task files, validate the result, open a draft PR, and prepare test-plan-driven acceptance evidence. |
| `spec-driven:plan-change` | Validate an approved proposal, specs, design, and test plan, then create and review implementation tasks without OpenSpec. |
| `spec-driven:scaffold` | Bootstrap a brand new project, configure validation, and optionally publish it to GitHub. |
| `spec-driven:simple-change` | Run a quick plan, implement, and validate workflow for small changes. |

The latest `spec-driven:change` workflow mirrors the full OpenSpec change lifecycle except for OpenSpec creation, CLI validation, and archival. It requires `change_name` and stores its tracked artifacts under `specs/changes/<change_name>/`. `spec-driven:simple-change` keeps planning and implementation inline for smaller changes. The `scaffold` variant is for new project setup.

## Onboarding

| Workflow | Purpose |
| --- | --- |
| `onboarding:advanced` | Introduce advanced Agent Runner concepts and open the help agent. |
| `onboarding:guided-workflow` | Guide the user through planning, tutoring, and autonomous implementation during onboarding. |
| `onboarding:help` | Open an interactive Agent Runner help agent backed by bundled onboarding documentation. |
| `onboarding:onboarding` | Run the optional Agent Runner workflow demo chain. |
| `onboarding:step-types-demo` | Demonstrate Agent Runner workflow step types during onboarding. |
| `onboarding:validator` | Demonstrate Agent Validator setup and the validation feedback loop during onboarding. |

Onboarding workflows are used by first launch and help flows. Several are hidden from normal browsing because they are designed as steps in the onboarding chain.

The `onboarding:validator` workflow includes an internal Agent Validator init step. It scaffolds the `task-compliance` built-in review and passes configured Agent Runner CLI names through to Agent Validator when project agent profiles are available.
