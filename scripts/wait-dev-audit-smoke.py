#!/usr/bin/env python3
"""Wait for the product-owned smoke's audit and verify its expected local result."""
import json
from pathlib import Path
import sys
import time

def require(condition, message):
    if not condition:
        raise ValueError(message)

def read_object(path):
    with path.open(encoding="utf-8") as stream:
        result = json.load(stream)
    require(isinstance(result, dict), f"{path.name} must contain a JSON object")
    return result

def matching_identity(record, identity, label):
    require(all(record.get(key) == value for key, value in identity.items()),
            f"{label} has mismatched source/audit/session identity")

def verify_results(source, audit_dir, link):
    identity = {"source_run_id": source.name, "audit_run_id": audit_dir.name,
                "execution_session_id": link["execution_session_id"]}
    source_state = read_object(source / "state.json")
    require(source_state.get("runId") == source.name and source_state.get("completed") is True,
            "source did not preserve successful completion")
    links = source_state.get("audit", {}).get("links", [])
    require(len(links) == 1 and links[0].get("auditRunId") == audit_dir.name
            and links[0].get("executionSessionId") == identity["execution_session_id"]
            and links[0].get("state") == "completed", "source state linkage is incomplete")
    state = read_object(audit_dir / "state.json")
    require(state.get("runId") == audit_dir.name and state.get("runKind") == "audit",
            "linked state is not the expected audit")
    metadata = state.get("audit", {})
    require(metadata.get("sourceRunId") == source.name
            and metadata.get("sourceExecutionSessionId") == identity["execution_session_id"],
            "audit reciprocal linkage is incorrect")
    require(not metadata.get("warning") and not link.get("warning"),
            "audit completed with an execution warning")
    request = read_object(audit_dir / "request.json")
    matching_identity(request, identity, "request")
    provenance = request.get("runner_source", {})
    require(provenance.get("launch_root") == "/agent-runner-source"
            and provenance.get("coverage") == "complete" and provenance.get("verified") is True,
            "Runner source is not the verified mounted source")

    model_values = read_object(audit_dir / "model-output/value-001.json")
    require(model_values.get("batch_id") == "value-001", "missing value model batch")
    judgments = model_values.get("observations")
    require(isinstance(judgments, list) and len(judgments) == 1, "missing value model observation")
    values = read_object(audit_dir / "value-observations.json")
    require(values.get("schema_version") == 1 and not values.get("diagnostics"),
            "value validation did not succeed")
    observations = values.get("observations")
    require(isinstance(observations, list) and len(observations) == 1,
            "expected one validated source-step observation")
    observation = observations[0]
    matching_identity(observation, identity, "observation")
    require(observation.get("step_id") == "source-agent" and observation.get("observation_id"),
            "observation does not identify the source fixture step")
    require(judgments[0].get("observation_id") == observation["observation_id"],
            "model result and validated observation disagree")
    expected = {"overall_value": "medium", "change_effect": "intended",
                "unique_contribution": "unique", "downstream_evidence": "supporting",
                "confidence": "high", "evidence_coverage": "partial"}
    for key, value in expected.items():
        require(observation.get(key) == value and judgments[0].get(key) == value,
                f"invalid smoke judgment: {key}")
    correctness = read_object(audit_dir / "model-output/correctness.json")
    require(correctness.get("candidates") == [], "correctness model did not return empty candidates")
    report = read_object(audit_dir / "local-report.json")
    matching_identity(report, identity, "local report")
    require(report.get("schema_version") == "step_value_v1" and report.get("values") == values,
            "local report lacks the validated values")
    require(report.get("runner_source") == provenance, "report provenance differs from request")
    findings = report.get("correctness", {})
    require(findings.get("schema_version") == "correctness_v1"
            and findings.get("findings") == [] and not findings.get("diagnostics"),
            "correctness validation did not succeed")
    require(report.get("destination", {}).get("state") == "unusable"
            and report.get("delivery_state") == "pending", "expected local reporting warning only")

def wait_for_audit(source, timeout):
    deadline = time.monotonic() + timeout
    audit_id = "unavailable"
    last_error = "waiting for linked audit"
    while True:
        try:
            lifecycle = read_object(source / "audit-lifecycle.json")
            require(lifecycle.get("source_run_id") == source.name, "lifecycle source identity mismatch")
            links = lifecycle.get("links", [])
            require(len(links) == 1, "expected exactly one linked audit")
            link = links[0]
            audit_id = link.get("audit_run_id", "")
            require(audit_id and Path(audit_id).name == audit_id and audit_id not in (".", ".."),
                    "invalid audit identity")
            require(link.get("execution_session_id"), "missing execution-session identity")
            if link.get("state") == "failed":
                raise RuntimeError(f"audit={audit_id} launch failed: {link.get('warning', '')}")
            audit_dir = source.parent / audit_id
            state = read_object(audit_dir / "state.json")
            if (link.get("state") == "completed" and state.get("completed") is True
                    and state.get("audit", {}).get("lifecycleState") == "completed"):
                verify_results(source, audit_dir, link)
                return
            last_error = "lifecycle and audit state are not both terminal"
        except (OSError, ValueError, KeyError, TypeError, AttributeError) as error:
            # State and reporting files are published separately and atomically.
            # Bounded retries cover temporary absence without accepting invalid output.
            last_error = str(error)
        if time.monotonic() >= deadline:
            raise RuntimeError(f"audit={audit_id}; deadline expired: {last_error}")
        time.sleep(min(0.05, max(0, deadline - time.monotonic())))

def main():
    source = Path(sys.argv[1])
    try:
        timeout = float(sys.argv[2])
        require(0 < timeout <= 3600, "timeout must be between 0 and 3600 seconds")
        wait_for_audit(source, timeout)
    except (ValueError, RuntimeError) as error:
        print(f"smoke: source={source.name}; {error}; "
              f"audit linkage and artifacts retained in {source}", file=sys.stderr)
        return 1
    return 0

if __name__ == "__main__":
    sys.exit(main())
