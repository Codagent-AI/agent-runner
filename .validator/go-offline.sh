#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -eq 0 ]; then
  echo "usage: .validator/go-offline.sh <command> [args...]" >&2
  exit 64
fi

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "$script_dir/.." && pwd)"
validator_cache="$root/.validator/cache"
project_modcache="$validator_cache/go/pkg/mod"

mkdir -p "$validator_cache/go-build" "$validator_cache/go" "$project_modcache"

default_modcache="$(GOTOOLCHAIN=local go env GOMODCACHE 2>/dev/null || true)"

if [ ! -d "$project_modcache/cache/download" ] &&
  [ -n "$default_modcache" ] &&
  [ "$default_modcache" != "$project_modcache" ] &&
  [ -d "$default_modcache/cache/download" ]; then
  mkdir -p "$project_modcache/cache"
  if command -v rsync >/dev/null 2>&1; then
    rsync -a -- "$default_modcache/cache/download/" "$project_modcache/cache/download/"
  else
    cp -R "$default_modcache/cache/download" "$project_modcache/cache/download"
  fi
fi

export GOTOOLCHAIN=local
export GOPROXY=off
export GOSUMDB=off
export GOCACHE="$validator_cache/go-build"
export GOPATH="$validator_cache/go"
export GOMODCACHE="$project_modcache"

exec "$@"
