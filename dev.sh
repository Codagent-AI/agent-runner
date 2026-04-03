#!/usr/bin/env bash
set -euo pipefail

cmd="${1:-}"
shift || true

case "$cmd" in
  run)
    go run ./cmd/agent-runner "$@"
    ;;
  validate)
    go run ./cmd/agent-runner -validate "$@"
    ;;
  resume)
    go run ./cmd/agent-runner -resume "$@"
    ;;
  plan)
    go run ./cmd/agent-runner workflows/plan-change.yaml "$@"
    ;;
  implement)
    go run ./cmd/agent-runner workflows/implement-change.yaml "$@"
    ;;
  *)
    echo "Usage: ./dev <command> [args...]"
    echo ""
    echo "Commands:"
    echo "  run         Run agent-runner"
    echo "  validate    Validate a workflow file"
    echo "  resume      Resume an interrupted workflow"
    echo "  plan        Run plan-change workflow"
    echo "  implement   Run implement-change workflow"
    echo ""
    echo "All flags are passed through directly, e.g.:"
    echo "  ./dev resume -session plan-change-2026-04-03T23-19-18-552111Z"
    exit 1
    ;;
esac
