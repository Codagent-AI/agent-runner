# Eval Runner Harness — Plan

Internal design note (not a product feature, not an OpenSpec change). Guides the
implementation of the first scored Agent Runner eval, using **and-scene** as the
fixture. Written for an implementing agent with no shared conversation context —
read it top to bottom.

## Goal (this session)

Stand up the first **scored** eval that runs Agent Runner against a reviewed-but-
unimplemented and-scene change **inside the browser-capable sandbox**, produces a
working artifact (the and-scene skill + reference demo presentation), and grades it.

Concretely, by the end:

1. The and-scene fixture branch is the correct **implementation-ready** snapshot
   (latest specs + tasks, no implementation).
2. `agent-runner` runs an implementation workflow against that fixture in the
   sandbox and rebuilds the skill + demo presentation.
3. The harness captures run outputs and grades: **tier-1** = build + `npm run
   verify` pass/fail; **tier-2** = LLM judge against the spec.

## Key design decisions (with rationale)

These were settled through discussion; keep them unless there's a strong reason.

1. **The eval measures implementation only. Task generation is NOT under test.**
   - Therefore **tasks are baked into the fixture**. The fixture is the
     "implementation-ready" snapshot: reviewed `proposal` + `design` + `specs` +
     `tasks.md` + `tasks/*.md`, and **no implementation code**.

2. **The harness is workflow-agnostic.** It takes the workflow to run as an input
   (name or path). It assumes nothing about whether that workflow uses tasks. This
   is what lets a future "no-tasks" workflow variant slot in with zero harness
   changes (that variant would ignore/strip the baked tasks and implement straight
   from specs). Out of scope this session, but the harness must not hard-code a
   tasks assumption.

3. **No resume. Every eval run starts fresh (`session: new`).**
   - Rationale: Agent Runner "resume" depends on both `state.json` *and* the agent
     CLI's own conversation store. The conversation store does not exist in a fresh
     sandbox and is not portable, so a cross-environment "resume" is a cold start
     wearing a resume costume — with an added brittle dependency on internal
     `state.json` format stability. No fidelity gain, real fragility.
   - **Principle:** prior workflow progress is always expressed as **committed
     artifacts in the fixture**, never as resumed run state. "Resume after
     planning" is modeled as "the plan artifacts (specs + tasks) are committed in
     the fixture, and a fresh implementation run starts from there."

4. **The fixture is model- and workflow-agnostic.** All variation (agent, model,
   workflow variant, judge config) lives in the harness, not the fixture repo.

## Fixture prep (in the `and-scene` repo — a DIFFERENT repo)

Repo: `/Users/paul/codagent/and-scene` (public: `Codagent-AI/and-scene`).
Fixture branch: `eval/create-and-scene-spec-only` (worktree
`/Users/paul/codagent/and-scene.eval`).

**STATUS: DONE** — completed and pushed as `cd0cc00` (from `4e8615e`). Specs +
proposal re-synced from main's canonical versions and the 3 task files baked in;
`openspec validate --type change create-and-scene` passes; no implementation
present. The table below records what was done (for reference / re-derivation).

The fixture previously held the reviewed change under
`openspec/changes/create-and-scene/` but was **stale** and **missing tasks**. It
was brought to the implementation-ready snapshot:

Canonical (latest) sources live on and-scene `main`:
- Living specs: `openspec/specs/<capability>/spec.md` (format: `# Title` /
  `## Purpose` / `## Requirements`).
- Archived change: `openspec/changes/archive/2026-06-05-create-and-scene/`
  (proposal, design, tasks.md, tasks/01..03).

Actions on the fixture branch, under `openspec/changes/create-and-scene/`:

| File | State now | Action |
|---|---|---|
| `specs/evolving-scene-presentations/spec.md` | drifted | Re-sync requirement bodies from main's **living** spec, kept in **delta format** (`## ADDED Requirements`, then `### Requirement:` blocks). NOT a raw copy — the living spec has `# Title`/`## Purpose`/`## Requirements` wrappers that must become `## ADDED Requirements`. |
| `specs/presentation-skill/spec.md` | drifted | same |
| `specs/presentation-verification/spec.md` | drifted | same |
| `proposal.md` | drifted | Re-sync from the archived canonical `proposal.md`. |
| `design.md` | identical | leave. |
| `.openspec.yaml` | identical | leave. |
| `tasks.md` | **missing** | Add, ported from archived `tasks.md`. |
| `tasks/01-scene-kit-app-shell.md`, `tasks/02-presentation-skill.md`, `tasks/03-reference-sample-verification.md` | **missing** | Add, ported from the archived `tasks/`. |

The fixture must still contain **no implementation**: no `skills/`, `scripts/`,
`src/presentation-kit/`, `src/presentations/`, and no implementation history.

Commit to `eval/create-and-scene-spec-only` and push to origin (public repo push —
confirm before pushing). Verify `openspec validate --type change create-and-scene`
passes in the fixture checkout.

> Note: the branch name `...-spec-only` is now a slight misnomer (it also carries
> tasks). Renaming is optional and out of scope here; if renamed, update the
> harness default `--fixture-ref`.

## The harness (in the `agent-runner` repo)

Build on the existing sandbox substrate — do not reinvent it:
- `scripts/sandbox-run.sh` — builds the local agent-runner checkout inside the
  Playwright image and runs a command with `/artifacts` mounted. Reuse as-is.
- `scripts/eval-and-scene.sh` — currently only the **narrow browser proof**
  (clone → build fixture, clone → build+verify reference; never runs agent-runner).
  This is what grows into the real eval runner (or a sibling script; see below).

The eval runner (extending `eval-and-scene.sh` with a new mode, e.g.
`--run-agent`, or a new `scripts/eval-and-scene-run.sh` — implementer's call;
prefer extending to keep one entry point) must, inside the sandbox:

1. Clone and-scene, check out the fixture ref (default
   `origin/eval/create-and-scene-spec-only`), record `fixture_commit`.
2. Run `agent-runner run <workflow>` against that checkout, non-interactively /
   autonomously. For this session the workflow is an **implementation-only**
   variant that: loops the implement-task cycle over the baked `tasks/*.md`, then
   is followed by build + verify. See "Workflow" below.
3. Capture outputs (see "Captured artifacts").
4. Grade: tier-1 (build + verify), tier-2 (LLM judge).

### Workflow to run (first variant)

The existing `workflows/openspec/implement-change.yaml` loops `implement-task`
over `tasks/*.md` — the right core — but its tail steps (`review-assumptions`,
`simplify`) are interactive `lead-agent` prompts that can't run unattended, and it
has no build/verify step. For the eval, author a **thin internal eval workflow**
(NOT embedded/shipped in `workflows/`, since those get baked into the binary):
the implement-task loop over the baked tasks + `npm ci && npm run build && npm run
verify`, all autonomous, no interactive review/simplify tail.

**Verify during implementation:** confirm `agent-runner run` can execute a workflow
from an arbitrary **file path** (not only embedded names). If it can't, decide how
to inject the eval workflow (e.g. a temp workflow dir on the sandbox workspace, or
a build-tag). Do not assume; check `internal/loader` / the `run` command wiring.

### Sandbox & auth

- Both Claude Code and Codex must work; the eval picks one per run (default Claude).
- Auth reuses the existing host-config passthrough: `--with-host-config`
  (`devcontainer-shell.sh`) mounts host `~/.claude` / `~/.codex` creds read-only
  into the container, and `scripts/sandbox-sync-home.sh` copies them into the
  writable container `$HOME`. The **non-interactive** `sandbox-run.sh` path exposes
  the same via `--mount-claude-auth` / `--mount-codex-auth` (which mount to
  `/host-home/...`, then `sandbox-sync-home.sh` runs in the bootstrap). Wire the
  eval runner to forward the chosen agent's auth through `sandbox-run.sh`; do not
  invent a new auth path.

## Captured artifacts & scoring

Copy to the host `/artifacts` dir (default under
`artifacts/evals/and-scene/<timestamp>/`):

- The produced **implementation diff**: `git diff <fixture-base>..<final-HEAD>`.
- **diff hash** (for run identity / dedup).
- Agent Runner **`state.json`** and **`audit.log`** from the run.
- Logs: agent-runner run log, `npm ci`, `npm run build`, `npm run verify`.
- `metadata.json`: repo, fixture_ref, fixture_commit, agent_runner_commit +
  dirty flag, workflow name/path, agent, model, node/npm versions, started/ended,
  exit codes. (Extend the existing `write_metadata` in `eval-and-scene.sh`.)

**Diff scoring must exclude harness-injected paths** — not part of the artifact
under test. Determine the exact set by inspecting a real run; expected categories:
Agent Runner run-state dirs, `agent-validator` logs/config, `.openspec` workflow
state, and any profile/config the harness writes into the checkout. Score only the
diff that represents the produced implementation.

### Tier-1 (deterministic)

Pass/fail on `npm run build` and `npm run verify` (the Playwright-backed render
check) in the produced checkout.

### Tier-2 (LLM judge) — settled rubric

- **Grade against:** the **spec scenarios** are primary. The judge grades whether
  the produced artifact satisfies each scenario. The **reference branch
  (`change/create-and-scene`) is consulted only as a tiebreak** — for scenarios the
  judge cannot confidently call from the artifact alone. It is not a similarity
  target; a spec-satisfying artifact that diverges from the reference should still
  pass. Provide the reference diff/tree to the judge but instruct it to use the
  reference only to adjudicate low-confidence scenarios, never to penalize valid
  divergence.
- **Evidence shown to the judge:** the **full produced source tree** (post-run
  checkout, not just the diff), the **build + verify logs**, and the **render
  screenshots** from the inspection tooling (`scripts/inspect-presentation.mjs` /
  what `npm run verify` captures) for each presentation step. This is a visual
  artifact — the judge must see what actually rendered.
- **Output:** per-scenario `{id, verdict (pass/fail), note}`, an `overall_score`
  (0–100), a `pass` boolean, and a `rationale`. `pass` = every scenario the rubric
  marks **critical** passes (mark which scenarios are critical when building the
  rubric; at minimum the verification/render scenarios and the core skill-behavior
  scenarios are critical).

Keep the judge model-agnostic and its config in the harness. Record the judge model
and its full structured output in the captured artifacts. Full-tree + screenshots is
token-heavy — expect to filter obvious boilerplate (lockfiles, node_modules is
absent post-`npm ci`? ensure it's excluded) from the tree passed to the judge.

## Out of scope this session / future

- The **no-tasks** workflow variant (implement straight from specs, tasks
  stripped). Harness stays workflow-agnostic so it slots in later.
- Isolating implementation quality with a tasks-baked-but-different fixture — falls
  out for free (another fixture ref); not needed now.
- Reference-comparison judge mode.
- Renaming the `...-spec-only` fixture branch.

## Open items to confirm during implementation

1. **Confirm** `agent-runner run` can run a workflow from a file path; if not,
   choose an injection mechanism for the internal eval workflow.
2. Determine the exact harness-injected path exclusion set from a real run before
   finalizing diff scoring.

Resolved: tier-2 rubric settled (spec-primary, reference-as-tiebreak; full tree +
logs + screenshots; per-scenario pass/fail + 0–100 + pass + rationale). Fixture
branch prepped and pushed (`cd0cc00`).
