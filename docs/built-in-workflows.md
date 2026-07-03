---
title: Built-In Workflows
group: Reference
order: 7
description: Embedded workflow namespaces and workflow purposes.
---

# Built-In Workflows

Agent Runner embeds namespaced workflows into the binary. Use the TUI or `agent-runner -list` to browse the full embedded set.

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
| `core:debug` | Debug a failed Agent Runner run and optionally file an issue. |
| `core:finalize-pr` | Push PR, wait for CI, fix failures, and repeat until green, with a maximum of three fix cycles. |
| `core:implement-task` | Implement a single task with an agent step followed by a validator retry loop. |
| `core:review-proposal` | Run adversarial proposal review, lead response, and up to three discussion rounds. |
| `core:run-validator` | Run Agent Validator with a counted retry loop and fix-on-failure step. |

Some `core:*` workflows are hidden from normal browsing because they are intended to be invoked by higher-level workflows.

## OpenSpec

| Workflow | Purpose |
| --- | --- |
| `openspec:change` | Run the full plan, implement, validate, and PR creation workflow for normal feature development. |
| `openspec:implement-change` | Loop over OpenSpec task files and invoke `core:implement-task` for each task. |
| `openspec:plan-change` | Create and plan an OpenSpec change through proposal, spec, design, task planning, and review. |
| `openspec:scaffold` | Bootstrap a brand new OpenSpec project, configure validation, and optionally publish it to GitHub. |
| `openspec:simple-change` | Run a quick plan, implement, and validate workflow for small changes. |

The `openspec:change` workflow is a full development flow: it plans the change, implements task files, validates the result, and can continue through PR finalization. `openspec:simple-change` keeps planning and implementation inline for smaller changes. `openspec:scaffold` runs `openspec init --tools none` so the project has OpenSpec directories without installing OpenSpec agent skills or slash commands.

## Spec-Driven

| Workflow | Purpose |
| --- | --- |
| `spec-driven:change` | Run the full plan, implement, validate, and PR creation workflow for normal feature development. |
| `spec-driven:implement-change` | Locate task files and invoke `core:implement-task` for each task. |
| `spec-driven:plan-change` | Plan a change through proposal, spec, design, task planning, and review without OpenSpec. |
| `spec-driven:scaffold` | Bootstrap a brand new project, configure validation, and optionally publish it to GitHub. |
| `spec-driven:simple-change` | Run a quick plan, implement, and validate workflow for small changes. |

The `spec-driven:change` workflow is a full development flow without an OpenSpec dependency. `spec-driven:simple-change` keeps planning and implementation inline for smaller changes. The `scaffold` variant is for new project setup.

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
