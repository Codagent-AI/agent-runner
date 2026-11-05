#!/bin/sh
# Decides whether Agent Validator bookkeeping (detect, baseline skip) runs for a
# run's selected validation workflow. Source this file for the functions, or run
# it with the validation_workflow value as its argument to print "run" or "skip".
#
#   empty (older callers)      run iff .validator/config.yml exists
#   agent-validator            run
#   auto                       run iff .validator/config.yml exists
#   anything else              skip
# The built-in references and their short names are treated alike.

validator_bookkeeping() {
  case "${1:-}" in
    ""|auto|builtin:core/validation-auto-v1.0.yaml)
      if [ -f .validator/config.yml ]; then printf 'run\n'; else printf 'skip\n'; fi
      ;;
    agent-validator|builtin:core/validation-agent-validator-v1.0.yaml)
      printf 'run\n'
      ;;
    *)
      printf 'skip\n'
      ;;
  esac
}

# Reads the optional validation_workflow string from a JSON script payload.
payload_validation_workflow() {
  if command -v jq >/dev/null 2>&1; then
    printf '%s' "$1" | jq -r '(.validation_workflow // "") | tostring'
  else
    PAYLOAD="$1" python3 -c 'import json, os; v = json.loads(os.environ["PAYLOAD"]).get("validation_workflow", ""); print(v if isinstance(v, str) else "")'
  fi
}

case "${0##*/}" in
  validator-bookkeeping.sh) validator_bookkeeping "${1:-}" ;;
esac
