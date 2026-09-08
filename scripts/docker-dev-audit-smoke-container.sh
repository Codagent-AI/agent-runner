#!/usr/bin/env bash
# Runs inside the sandbox after sandbox-run.sh has built the opted-in binary.
set -euo pipefail

smoke_root="$(mktemp -d /artifacts/dev-audit-smoke.XXXXXX)"
project="$smoke_root/project"
fake_bin="$smoke_root/bin"
release="$smoke_root/release-audit"
protected="$project/protected-source.txt"
mkdir -p "$project/.agent-runner" "$fake_bin"
printf 'protected source\n' > "$protected"

cat > "$project/.agent-runner/config.yaml" <<'YAML'
profiles:
  default:
    agents:
      crosscheck:
        default_mode: autonomous
        cli: codex
        model: smoke-model
        effort: low
YAML

cat > "$fake_bin/codex" <<'FAKE'
#!/bin/sh
set -eu
prompt=""
output_path=""
want_output_path=0
for arg in "$@"; do
  if [ "$want_output_path" = 1 ]; then
    output_path="$arg"
    want_output_path=0
  elif [ "$arg" = "--output-last-message" ]; then
    want_output_path=1
  fi
  prompt="$arg"
done
case "$prompt" in
  "You are judging workflow-step value"*)
    while [ ! -f "$AUDIT_SMOKE_RELEASE" ]; do sleep 0.05; done
    if printf 'escaped\n' > "$AUDIT_SMOKE_PROTECTED"; then
      echo "smoke: Linux audit sandbox permitted a protected write" >&2
      exit 44
    fi
    result="$(python3 - "$prompt" <<'PY'
import json, sys
package = json.loads(sys.argv[1].rsplit("\n\n", 1)[1])
print(json.dumps({
    "batch_id": package["batch_id"],
    "observations": [{
        "observation_id": leaf["skeleton"]["observation_id"],
        "overall_value": "medium",
        "change_effect": "intended",
        "unique_contribution": "unique",
        "downstream_evidence": "supporting",
        "confidence": "high",
        "evidence_coverage": "partial"
    } for leaf in package["leaves"]]
}))
PY
)"
    ;;
  "Investigate only reproducible Agent Runner defects"*) result='{"candidates":[]}' ;;
  *) result='source fixture completed' ;;
esac
if [ -n "$output_path" ]; then printf '%s\n' "$result" > "$output_path"; fi
printf '%s\n' "{\"type\":\"thread.started\",\"thread_id\":\"smoke\"}"
printf '%s\n' "{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"$result\"}}"
printf '%s\n' '{"type":"turn.completed"}'
FAKE
chmod 755 "$fake_bin/codex"

export PATH="$fake_bin:$PATH" AUDIT_SMOKE_RELEASE="$release" AUDIT_SMOKE_PROTECTED="$protected" HOME="$smoke_root/home"
mkdir -p "$HOME"
cd "$project"
agent-runner --headless openspec:audit-smoke

source_dir="$(find "$HOME/.agent-runner/projects" -name audit-lifecycle.json -print -quit | xargs -r dirname)"
test -n "$source_dir" || { echo "smoke: source returned without a durable audit lifecycle" >&2; exit 1; }
python3 - "$source_dir/audit-lifecycle.json" <<'PY'
import json, sys
with open(sys.argv[1], encoding="utf-8") as stream: links = json.load(stream).get("links", [])
if len(links) != 1 or links[0].get("state") == "completed":
    raise SystemExit("smoke: source did not return while exactly one audit was active")
PY

touch "$release"
python3 "$(dirname -- "${BASH_SOURCE[0]}")/wait-dev-audit-smoke.py" \
  "$source_dir" "${AUDIT_SMOKE_TIMEOUT_SECONDS:-45}"
test "$(cat "$protected")" = "protected source" || { echo "smoke: protected source changed" >&2; exit 1; }
echo "development-audit smoke passed; artifacts retained in $smoke_root"
