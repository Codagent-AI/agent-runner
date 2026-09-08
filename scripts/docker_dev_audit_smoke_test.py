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
        self.write(self.audit / "request.json", dict(identity, runner_source=provenance))
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
        for name, body in {"mktemp": 'printf "%s\\n" "$SMOKE_TEST_ROOT"',
                           "agent-runner": "exit 0"}.items():
            path = self.bin / name
            path.write_text("#!/bin/sh\n" + body + "\n")
            path.chmod(0o755)

    def write(self, path, value):
        staging = path.with_suffix(".new")
        staging.write_text(json.dumps(value))
        staging.replace(path)

    def run_smoke(self, complete=True, delayed_state=False):
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
                   SMOKE_TEST_ROOT=str(self.root), AUDIT_SMOKE_TIMEOUT_SECONDS="1")
        try:
            result = subprocess.run(["bash", str(SCRIPT)], env=env, capture_output=True,
                                    text=True, timeout=8)
            state_at_return = json.loads((self.audit / "state.json").read_text())
            return result, state_at_return
        finally:
            stop.set()
            thread.join()

    def test_valid_completed_audit_passes(self):
        result, _ = self.run_smoke()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

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
