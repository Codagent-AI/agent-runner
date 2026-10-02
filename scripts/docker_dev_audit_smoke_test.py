#!/usr/bin/env python3
"""Regression tests for the delivered smoke supervisor using local CLI fixtures."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import threading
import time
import unittest

SCRIPT = Path(__file__).with_name("docker-dev-audit-smoke-container.sh")

class SmokeTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name) / "smoke"
        self.runs = self.root / "home/.agent-runner/projects/fixture/runs"
        self.source = self.runs / "source"
        self.audit = self.runs / "audit"
        self.source.mkdir(parents=True)
        (self.audit / "model-output").mkdir(parents=True)
        self.lifecycle = {"source_run_id": "source", "links": [{
            "audit_run_id": "audit", "execution_session_id": "session", "state": "started"
        }]}
        self.write(self.source / "audit-lifecycle.json", self.lifecycle)
        self.write(self.source / "state.json", {"runId": "source", "completed": True,
            "audit": {"links": [{"auditRunId": "audit", "executionSessionId": "session",
                                 "state": "completed"}]}})
        self.state = {"runId": "audit", "completed": True, "runKind": "audit",
            "audit": {"sourceRunId": "source", "sourceExecutionSessionId": "session",
                      "lifecycleState": "completed"}}
        self.write(self.audit / "state.json", self.state)
        identity = {"source_run_id": "source", "audit_run_id": "audit",
                    "execution_session_id": "session"}
        provenance = {"launch_root": "/agent-runner-source", "coverage": "complete",
                      "verified": True, "launch_git_available": False}
        self.write(self.audit / "request.json", dict(identity, runner_source=provenance,
            auditor={"cli": "codex", "model": "smoke-model", "reasoning_effort": "low"}))
        observation = dict(identity, observation_id="observation", step_id="source-agent",
                           overall_value="medium", change_effect="intended",
                           unique_contribution="unique", downstream_evidence="supporting",
                           confidence="high", evidence_coverage="partial")
        values = {"schema_version": 1, "observations": [observation]}
        self.write(self.audit / "value-observations.json", values)
        self.write(self.audit / "model-output/value-001.json",
                   {"batch_id": "value-001", "observations": [observation]})
        self.write(self.audit / "model-output/correctness.json", {"candidates": []})
        self.report = dict(identity, schema_version="step_value_v1", values=values,
                           correctness={"schema_version": "correctness_v1", "findings": []},
                           runner_source=provenance,
                           destination={"state": "unusable"}, delivery_state="pending")
        self.write(self.audit / "local-report.json", self.report)
        self.bin = Path(self.temp.name) / "bin"
        self.bin.mkdir()
        # Replay restores the staged audit fixtures, as a real replay creates them.
        replay = '''if [ "${1:-}" = audit ] && [ "${2:-}" = replay ]; then
    printf '%s\\n' "$@" > "$SMOKE_TEST_ROOT/replay-args"
    [ -z "${SMOKE_TEST_REPLAY_EXIT:-}" ] || exit "$SMOKE_TEST_REPLAY_EXIT"
    mv "$SMOKE_TEST_STAGED_AUDIT" "$SMOKE_TEST_AUDIT"
    mv "$SMOKE_TEST_STAGED_LIFECYCLE" "$SMOKE_TEST_LIFECYCLE"
fi
exit 0'''
        for name, body in {"mktemp": 'printf "%s\\n" "$SMOKE_TEST_ROOT"',
                           "agent-runner": replay}.items():
            path = self.bin / name
            path.write_text("#!/bin/sh\n" + body + "\n")
            path.chmod(0o755)

    def write(self, path, value):
        staging = path.with_suffix(".new")
        staging.write_text(json.dumps(value))
        staging.replace(path)

    def run_smoke(self, complete=True, delayed_state=False, env=None):
        stop = threading.Event()
        def updater():
            while not (self.root / "release-audit").exists():
                if stop.wait(0.01):
                    return
            if complete:
                self.lifecycle["links"][0]["state"] = "completed"
                self.write(self.source / "audit-lifecycle.json", self.lifecycle)
            if delayed_state:
                if stop.wait(0.2):
                    return
                self.state["completed"] = True
                self.write(self.audit / "state.json", self.state)
        thread = threading.Thread(target=updater)
        thread.start()
        env = dict(os.environ, PATH=str(self.bin) + os.pathsep + os.environ["PATH"],
                   SMOKE_TEST_ROOT=str(self.root), AUDIT_SMOKE_TIMEOUT_SECONDS="1",
                   **(env or {}))
        try:
            result = subprocess.run(["bash", str(SCRIPT)], env=env, capture_output=True,
                                    text=True, timeout=8)
            state_at_return = (json.loads((self.audit / "state.json").read_text())
                               if (self.audit / "state.json").exists() else None)
            return result, state_at_return
        finally:
            stop.set()
            thread.join()

    def test_valid_completed_audit_passes(self):
        result, _ = self.run_smoke()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertFalse((self.root / "replay-args").exists())

    def prepare_replay(self):
        """Leave only the source run, as a paused automatic audit does."""
        staged_audit = Path(self.temp.name) / "staged-audit"
        staged_lifecycle = Path(self.temp.name) / "staged-lifecycle.json"
        lifecycle = self.source / "audit-lifecycle.json"
        self.audit.rename(staged_audit)
        lifecycle.rename(staged_lifecycle)
        self.write(self.source / "run-metrics.json", {"sessions": [
            {"execution_session_id": "session"}]})
        return {"SMOKE_TEST_STAGED_AUDIT": str(staged_audit), "SMOKE_TEST_AUDIT": str(self.audit),
                "SMOKE_TEST_STAGED_LIFECYCLE": str(staged_lifecycle),
                "SMOKE_TEST_LIFECYCLE": str(lifecycle)}

    def test_replays_source_when_automatic_audit_is_paused(self):
        result, _ = self.run_smoke(env=self.prepare_replay())
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("automatic audit is paused (#191)", result.stderr)
        self.assertEqual((self.root / "replay-args").read_text().splitlines(), [
            "audit", "replay", str(self.source), "--session", "session", "--project",
            str(self.root / "project")])
        self.assertIn("development-audit smoke passed", result.stdout)

    def test_replay_failure_fails_smoke(self):
        result, _ = self.run_smoke(env=dict(self.prepare_replay(), SMOKE_TEST_REPLAY_EXIT="42"))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("smoke: explicit audit replay failed", result.stderr)
        self.assertNotIn("smoke passed", result.stdout)

    def test_rejects_non_fixture_auditor(self):
        request = json.loads((self.audit / "request.json").read_text())
        request["auditor"] = {"cli": "claude", "model": "opus", "reasoning_effort": "high"}
        self.write(self.audit / "request.json", request)
        result, _ = self.run_smoke()
        self.assertNotEqual(result.returncode, 0, "smoke accepted a non-fixture auditor")
        self.assertIn("non-fixture auditor", result.stderr)
        self.assertIn("claude", result.stderr)
        self.assertNotIn("smoke passed", result.stdout)

    def test_fake_codex_reads_audit_prompt_from_stdin(self):
        result, _ = self.run_smoke()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        fake_codex = self.root / "bin/codex"
        output = self.root / "audit-response.json"
        response = subprocess.run([str(fake_codex), "exec", "--output-last-message", str(output), "-"],
                                  input="Investigate only reproducible Agent Runner defects",
                                  capture_output=True, text=True, timeout=5)
        self.assertEqual(response.returncode, 0, response.stderr)
        self.assertEqual(output.read_text().strip(), '{"candidates":[]}')

    def test_fake_codex_reads_large_value_prompt_from_stdin(self):
        result, _ = self.run_smoke()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        fake_codex = self.root / "bin/codex"
        output = self.root / "value-response.json"
        release = self.root / "release-audit"
        release.touch()
        protected_dir = self.root / "read-only"
        protected_dir.mkdir()
        protected_dir.chmod(0o555)
        self.addCleanup(lambda: protected_dir.chmod(0o755))
        protected = protected_dir / "protected-source.txt"
        package = {"batch_id": "value-001", "leaves": [
            {"skeleton": {"observation_id": "observation"}}]}
        prompt = ("You are judging workflow-step value\n" + "x" * 1500000
                  + "\n\n" + json.dumps(package))
        env = dict(os.environ, AUDIT_SMOKE_RELEASE=str(release),
                   AUDIT_SMOKE_PROTECTED=str(protected))
        response = subprocess.run([str(fake_codex), "exec", "--output-last-message", str(output), "-"],
                                  input=prompt, env=env, capture_output=True, text=True, timeout=5)
        self.assertEqual(response.returncode, 0, response.stderr)
        self.assertEqual(json.loads(output.read_text()), {"batch_id": "value-001", "observations": [{
            "observation_id": "observation", "overall_value": "medium",
            "change_effect": "intended", "unique_contribution": "unique",
            "downstream_evidence": "supporting", "confidence": "high",
            "evidence_coverage": "partial"}]})

    def test_waits_for_audit_state_after_lifecycle_completion(self):
        self.state["completed"] = False
        self.write(self.audit / "state.json", self.state)
        result, state = self.run_smoke(delayed_state=True)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertTrue(state["completed"], "smoke passed before audit state was terminal")

    def test_incomplete_audit_times_out_with_identities(self):
        self.state["completed"] = False
        self.write(self.audit / "state.json", self.state)
        result, _ = self.run_smoke()
        self.assertNotEqual(result.returncode, 0, "smoke accepted incomplete audit state")
        self.assertIn("source", result.stderr)
        self.assertIn("audit", result.stderr)

    def test_rejects_invalid_results_and_linkage(self):
        cases = {
            "malformed report": ("local-report.json", "{broken"),
            "empty observations": ("value-observations.json", {"schema_version": 1, "observations": []}),
            "wrong reciprocal source": ("state.json", dict(self.state, audit={"sourceRunId": "wrong"})),
            "wrong request session": ("request.json", {"execution_session_id": "wrong"}),
            "missing report": ("local-report.json", None),
            "invalid model output": ("model-output/value-001.json", {"error": "model failed"}),
        }
        for name, (relative, value) in cases.items():
            with self.subTest(name=name):
                path = self.audit / relative
                previous = path.read_bytes()
                try:
                    if value is None:
                        path.unlink()
                    elif isinstance(value, str):
                        path.write_text(value)
                    else:
                        self.write(path, value)
                    self.lifecycle["links"][0]["state"] = "started"
                    self.write(self.source / "audit-lifecycle.json", self.lifecycle)
                    (self.root / "release-audit").unlink(missing_ok=True)
                    result, _ = self.run_smoke()
                    self.assertNotEqual(result.returncode, 0, name + ": smoke falsely passed")
                    self.assertNotIn("smoke passed", result.stdout)
                finally:
                    path.write_bytes(previous)

    def test_nonterminal_lifecycle_times_out(self):
        result, _ = self.run_smoke(complete=False)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("artifacts", result.stderr)

if __name__ == "__main__":
    unittest.main()
