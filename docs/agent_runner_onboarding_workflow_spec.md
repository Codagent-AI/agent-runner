# Agent Runner Onboarding Workflow — Spec (v0.1)

## 1. Overview

The onboarding experience is split between mandatory native setup and a
first-class Agent Runner workflow demo.

It serves two purposes:
1. Configure the agent profiles Agent Runner needs before the main TUI opens
2. Teach users how Agent Runner works through an optional workflow demo

Key principle:
> We use Agent Runner to teach Agent Runner

---

## 2. Entry & Lifecycle

### 2.1 First Run Behavior
- Native setup runs before the bare/list TUI when setup has not completed
- Successful setup records `settings.setup.completed_at`
- After setup succeeds, Agent Runner starts the optional onboarding workflow
  when it has not been completed or dismissed

### 2.2 Resume Behavior
- Native setup is not started before direct resume handling
- The optional onboarding workflow uses normal workflow state persistence

### 2.3 Access After First Run
- Onboarding is accessible via:
  - `agent-runner run onboarding:onboarding`
  - Future main menu / tab ("Onboarding")
- If incomplete:
  - Show "Continue onboarding"
- If complete:
  - Optionally allow replay

### 2.4 Dismissal
- User can:
  - Mark onboarding as complete
  - Hide it permanently

---

## 3. New Capability: Instructional UI Step

### 3.1 Problem
Existing step types are insufficient:
- shell → too raw
- headless → no user interaction
- interactive → agent-centric, not UI-centric

### 3.2 New Step Type (proposed)

```
mode: ui
```

### 3.3 Capabilities
- Render structured TUI screen
- Display:
  - Title
  - Body text
  - Optional diagrams/code snippets
- Provide actions:
  - Continue
  - Learn more
  - Skip
- Optional branching via conditions

### 3.4 Future Extension
- Could evolve into:
  - menus
  - forms
  - config editors

---

## 4. Onboarding Structure (Top-Level Workflow)

```
onboarding/
  ├── intro
  ├── step-types-demo
  ├── guided-workflow
  ├── validator-setup
  ├── validation-run
  ├── advanced
```

Setup is native TUI functionality, not a workflow phase. Each remaining phase
can be a **sub-workflow**.

---

## 5. Phase Breakdown

---

### Phase 1: Intro

**Goal:** orient the user

**Step:**
- UI screen

**Content:**
- What Agent Runner is
- What they will learn
- What they will accomplish

**Action:**
- "Let’s get started"

---

### Native Setup: Agent Profiles

**Goal:** configure agents before any execution

**Type:** native TUI + config editor

**Requirements:**
- Detect:
  - available adapters (Claude, Codex, etc.)
  - available models
- Allow user to:
  - define agent profiles
  - choose model per profile
  - choose adapter per profile
  - choose storage:
    - local (project)
    - global (user)

**Implementation:**
- New UI-based config editor
- Writes config file

---

### Phase 2: Step Types Demo

**Goal:** teach primitives without real work

**Structure:**
Walk through each step type:

#### 1. Interactive step
- Start agent session
- User plays around
- Instruction: "type /continue when done"

#### 2. Headless step
- Show autonomous execution

#### 3. Shell step
- Show deterministic command

**Important:**
- No real task
- Just format + behavior

---

### Phase 3: Guided Real Workflow

**Goal:** run a real workflow with explanation

User must:
- Be in a real project
- Choose a small real task

---

#### Step 4.1: Planning

- UI screen explains:
  - what planning step is
  - what will happen

- Then:
  - run planning step (interactive agent)

- After:
  - transition to tutorial agent

---

#### Tutorial Agent (new concept)

A **separate agent session** used for:
- answering questions
- explaining what's happening
- guiding next steps

---

#### Step 4.2: Implementation

- UI explanation:
  - "now we implement"
  - headless agent behavior

- Run:
  - headless implementation step

---

### Phase 4: Agent Validator Setup

**Goal:** introduce validation without overwhelming early

**Flow:**

1. UI explanation:
   - what Agent Validator is
   - why it matters

2. Interactive step:
   - run validator setup skill

User configures:
- checks (tests, lint, etc.)
- AI review (optional)

---

### Phase 5: Validation Run

**Goal:** close the loop

- Run validator on implemented task

User sees:
- validation output
- potential failures
- concept of feedback loop

---

### Phase 6: Advanced + Help

**Goal:** expand understanding + provide ongoing support

---

#### 7.1 Advanced Concepts Screen

Topics:
- workflows
- sessions (new/resume/inherit)
- loops
- validator loops
- sub-workflows

---

#### 7.2 Interactive Help Agent

New capability:

- dedicated help agent
- can:
  - answer questions
  - read docs
  - explain concepts

Accessible:
- at end of onboarding
- from main menu (permanent feature)

---

## 6. Key Concepts Introduced

The onboarding must explicitly teach:

- workflows are deterministic
- agent does not decide next steps
- sessions are first-class
- validation is required
- loops create reliability
- different step modes have different purposes

---

## 7. Design Principles

### 7.1 Progressive Disclosure
- Don’t overwhelm early
- Delay validator until later

### 7.2 Learn by Doing
- real project
- real task
- real output

### 7.3 Separation of Concerns
- tutorial agent ≠ execution agent

### 7.4 Reuse Core System
- onboarding uses:
  - workflows
  - sessions
  - sub-workflows
  - state persistence

(no special casing)

---

## 8. Open Questions / Future Work

- Exact UI framework for `mode: ui`
- How rich branching should be
- Whether tutorial agent persists beyond onboarding
- How to handle failures during onboarding task
- Whether to include parallel execution concepts later

---

## 9. Summary

This onboarding is not a tutorial bolted on top.

It is:

> a real workflow that teaches the system by being the system

It demonstrates:
- orchestration
- session management
- validation loops
- real-world usage

And leaves the user with:
- configured environment
- completed real task
- mental model of how Agent Runner works
