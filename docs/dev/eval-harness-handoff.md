# Handoff: and-scene eval harness

## Objective

Stand up a working, scored eval harness that runs Agent Runner against the
reviewed-but-unimplemented `and-scene` fixture in a browser-capable sandbox,
rebuilds the presentation skill + a demo presentation, and grades the result:

- **tier-1** = objective build gate (`npm ci`, `npm run build`, `npm run verify`)
- **tier-2** = LLM judge scoring scenario compliance against the spec, using
  captured render screenshots as visual evidence.

The harness runs the *real* production workflow, not a custom one, stopping
before the outward-facing PR tail.

## Current State

**Working end-to-end and discriminating.** Two live codex runs completed:

- **Run 1 (`live-codex-run-1`): fully blocked, empty diff, score 20.** Every
  shell command codex issued failed with
  `bwrap: No permissions to create a new namespace`. Codex's default
  `--sandbox workspace-write` wraps commands in bubblewrap, which needs
  unprivileged user namespaces that the Playwright Docker container disallows.
  The agent made zero changes; `verify` failed downstream on a missing script.
- **Fix:** the harness now writes `autonomous_permission_mode: yolo` to
  `$HOME/.agent-runner/settings.yaml`, which makes codex use
  `--sandbox danger-full-access` (no bwrap). This is `write_user_settings()` in
  the harness (committed in `40afbec`).
- **Run 2 (`live-codex-run-2`): full, real result.** agent_runner exit 0
  (`--until run-validator` stopped it before archive/finalize, no PR),
  `npm ci` 0, build 0, **verify 0 (tier-1 PASS)**. Tier-2 ran: **overall_score
  76**, `hard_pass false` because 5 critical scenarios failed
  (`stable-entity-morphs`, `style-ownership-boundary`,
  `self-bootstrapping-scaffold`, `self-verify-skill`,
  `visual-inspection-artifacts`); 16 scenarios passed. 131 KB implementation
  diff, clean of harness cruft (touches only `skills/`, `src/`, `scripts/`,
  config files). `soft_score` 90.4.

The harness plumbing (real workflow, `--until`, planner+implementor both
autonomous, `reward.json`, `manifest.json`, pinned fixture SHA) is all
committed and verified. The `--until` flag itself landed earlier (commit
`d2d0d68`).

The screenshot selector bug is now fixed. Capture uses the spec-mandated
`[data-step-count]` / `data-step-index` contract, writes
`screenshot-manifest.json` with expected-versus-captured coverage, and exits
non-zero when evidence is incomplete. Against the run-2 checkout it captured
all **9/9** steps. Incomplete evidence gets one judge-model repair attempt
against a temporary helper copy; using it incurs a five-point soft-score
penalty and never modifies the evaluated fixture.

The fixture now recommends `chrome-devtools-axi` in root `AGENTS.md`, with
`CLAUDE.md` symlinked to it. Fixture commit
`26d2866e5003f34786fffa528891e6092c87cf8b` is published and pinned. The sandbox
installs pinned AXI/MCP packages and its browser proof requires a successful AXI
snapshot of the reference app.

**A run-2 local preview is currently running** for manual inspection: the
fixture was cloned at its base SHA, run 2's diff applied, `npm install`, and
`vite` started at **http://localhost:5173/** (landing page;
`/how-to-make-a-presentation` is the one produced talk). The checkout lives in
the scratchpad (see Relevant Files). This is a throwaway preview, not committed.

## Key Decisions

- **Run the real workflow, not a custom one.** The harness runs
  `workflows/openspec/implement-change.yaml` with `--until run-validator` to
  stop before `archive`/`finalize` (which push a branch and open a real PR).
  A previously-created custom `and-scene-implement.yaml` was deleted.
- **YOLO permission mode is required in this sandbox.** It is the only way to
  avoid bwrap in the unprivileged container. Set via the user settings file, not
  per-agent config.
- **Pinned fixture, branch reference.** Fixture is pinned to SHA
  `26d2866e5003f34786fffa528891e6092c87cf8b`; the reference stays a branch
  (`origin/change/create-and-scene`). Repo:
  `https://github.com/Codagent-AI/and-scene.git`.
- **Implementation-only eval, no resume, no OpenSpec change in agent-runner.**
  Only a lightweight design note (`docs/dev/eval-runner-plan.md`).
- **Screenshots are inspection artifacts, not automated pass/fail.** Per the
  spec, tier-1 (`npm run verify`) is the machine gate; screenshots feed the
  tier-2 judge and the skill's visual-composition check.
- **Harbor eval framework: adopt concepts, do not adopt the tool now.** Cheap
  concepts already borrowed: `reward.json` (gate + soft score), `manifest.json`
  (file inventory + sha256), pinned commit. A separate verifier environment is
  deferred until the judge model differs from the agent and needs separate
  credentials.

## Open Questions / Open Issues

1. **Genuine implementation gap (agent's, legitimate fail).** Run 2 shipped no
   per-step screenshot helper. The spec
   (`presentation-verification/spec.md:66`, a *critical* scenario) requires
   scaffolded projects to ship a project-local helper that captures per-step
   screenshots and emits advisory overlap/chrome/attribution warnings. The
   produced tree has zero `page.screenshot` code; `verify.mjs`/`render-check.mjs`
   only check for render/console errors. So `visual-inspection-artifacts` and
   `self-verify-skill` are legitimate fails independent of issue 1. Open design
   question worth raising upstream: is baking a Playwright screenshot helper into
   *every* generated project the right design, or too heavyweight?

2. **Score trustworthiness.** The original 76 is still based on one screenshot.
   Capture is fixed and verified at 9/9, but the judge stage must be rerun before
   treating a replacement score as the baseline.

## Next Steps

1. **Re-run just the judge stage** against the corrected 9-step evidence from
   the existing run-2 local
   checkout (already at http://localhost:5173/ / in the scratchpad) to measure
   how much the score moves once the judge sees every step. This isolates the
   harness-bug effect from the real implementation gaps before spending another
   full live run.
2. **Re-run the full live codex eval** to get a
   trustworthy baseline score.
3. **Decide on the shipped-screenshot-helper requirement**: keep it as
   a critical deliverable the agent must satisfy, or soften it in the spec.
4. **Beyond:** update `docs/dev/eval-runner-plan.md` to record run-2 results and
   the closed diff-exclusion open item; consider the deferred separate-verifier
   environment when moving to cross-model judging; revisit Harbor for the
   results-viewer / dataset model if the eval grows beyond one fixture.

## Relevant Files

- `scripts/eval-and-scene.sh` — the harness (real workflow, `--until`,
  `write_user_settings` yolo fix, `write_reward`, `write_manifest`,
  planner+implementor config).
- `scripts/eval-workflows/scene-shots.mjs` — screenshot capture for the judge;
  enforces and reports complete step coverage.
- `scripts/eval_sandbox_scripts_test.go` — assertions over the rendered harness
  script (fixture SHA, workflow path, `--until`, yolo, reward/manifest).
- `scripts/sandbox-run.sh` — sandbox substrate (Playwright image, artifact
  mounts, codex auth mount).
- `workflows/openspec/implement-change.yaml` — the real workflow the eval runs.
- `docs/dev/eval-runner-plan.md` — design note (needs a run-2 update).
- `artifacts/evals/and-scene/live-codex-run-1/` — blocked run (bwrap, score 20).
- `artifacts/evals/and-scene/live-codex-run-2/` — real run
  (`reward.json`, `tier2-result.json`, `implementation.diff`, `metadata.json`,
  `logs/`, `screenshots/`).
- Scratchpad preview checkout (throwaway, not committed):
  `/private/tmp/claude-501/-Users-paul-codagent-agent-runner/e9b04698-6d51-424d-8874-83aed8fac7c5/scratchpad/run2-preview`
  (vite dev server running at http://localhost:5173/).

## Reference: spec / app facts a resuming agent will need

- Fixture repo: `https://github.com/Codagent-AI/and-scene.git`; fixture SHA
  `26d2866e5003f34786fffa528891e6092c87cf8b`; reference branch
  `origin/change/create-and-scene`.
- Step contract mandated by spec: `data-step-count` (total) and
  `data-step-index` (active) attributes on the progress chrome. The reference
  app *additionally* carries `data-testid="step-progress"` for its own unit
  test; this is NOT a spec requirement.
- Produced app routing is path-based: `/` = landing (lists presentations),
  `/<slug>` = the presentation. ArrowRight/ArrowLeft advance steps.
- `npm run verify` (= `node scripts/verify.mjs`) boots `vite preview`, walks
  every step via the bare `[data-step-count]`/`[data-step-index]` selectors,
  and fails on console errors. This is the tier-1 gate.
