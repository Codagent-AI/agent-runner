#!/bin/sh
set -eu
payload=$(cat)
SPEC_LEAK_PAYLOAD="$payload" python3 - <<'PY'
import json
import os
import re
import subprocess
import sys
from pathlib import Path

def git(*args):
    return subprocess.run(['git', *args], capture_output=True, text=True, check=True).stdout.strip()

try:
    p = json.loads(os.environ['SPEC_LEAK_PAYLOAD'])
    roots = {p['spec_root'], str(Path(p['spec_root']).resolve(strict=True))}
    raw = p.get('spec_root_input', '')
    if Path(raw).is_absolute() or (raw.startswith('~/') and len(raw) > 2):
        roots.add(raw)
    roots.discard('')
    patterns = [re.compile(r'(?<![\w.~-])' + re.escape(root.rstrip('/')) + r'(?![\w-]|\.\w)') for root in roots]
    # Mirror core:validate-feature-branch: GitHub's default branch when a remote
    # exists, else init.defaultBranch, then the conventional names.
    candidates = []
    default = subprocess.run(['git', 'symbolic-ref', '--quiet', 'refs/remotes/origin/HEAD'], capture_output=True, text=True)
    if default.returncode == 0:
        candidates.append(default.stdout.strip())
    if git('remote'):
        hosted = subprocess.run(['gh', 'repo', 'view', '--json', 'defaultBranchRef', '--jq', '.defaultBranchRef.name'], capture_output=True, text=True)
        name = hosted.stdout.strip() if hosted.returncode == 0 else ''
        if name:
            candidates += ['origin/' + name, name]
    configured = subprocess.run(['git', 'config', 'init.defaultBranch'], capture_output=True, text=True)
    if configured.returncode == 0 and configured.stdout.strip():
        candidates.append(configured.stdout.strip())
    candidates += ['origin/main', 'origin/master', 'main', 'master']
    base = None
    for ref in candidates:
        result = subprocess.run(['git', 'merge-base', ref, 'HEAD'], capture_output=True, text=True)
        if result.returncode == 0:
            base = result.stdout.strip()
            break
    if not base:
        raise ValueError('cannot determine merge base with default branch')
    locations = []
    def leaks(text):
        return any(pattern.search(text) for pattern in patterns)
    for sha in git('rev-list', base + '..HEAD').splitlines():
        if leaks(git('show', '-s', '--format=%B', sha)):
            locations.append('commit ' + sha)
    paths = subprocess.run(['git', 'diff', '--name-only', '-z', base + '..HEAD'], capture_output=True, check=True).stdout.decode().split('\0')
    for path in paths:
        if path and (leaks(path) or leaks(git('diff', '--no-ext-diff', '--no-textconv', base + '..HEAD', '--', path))):
            locations.append('file ' + path)
    # Without a remote there is no PR to scan. With one, only gh's "no pull
    # requests found" means no PR; any other failure must not pass the guard.
    pr = subprocess.run(['gh', 'pr', 'view', '--json', 'number'], capture_output=True, text=True) if git('remote') else None
    if pr is not None and pr.returncode != 0 and 'no pull requests found' not in pr.stderr:
        raise ValueError('cannot read PR: ' + pr.stderr.strip())
    if pr is not None and pr.returncode == 0:
        details = subprocess.run(['gh', 'pr', 'view', '--json', 'title,body'], capture_output=True, text=True, check=True)
        data = json.loads(details.stdout)
        for field in ('title', 'body'):
            if leaks(data.get(field, '')):
                locations.append('PR ' + field)
    if locations:
        raise ValueError('external spec path leaked in: ' + ', '.join(locations))
    print('No external spec path found in branch commits, diff or PR text.')
except (OSError, ValueError, KeyError, subprocess.CalledProcessError) as e:
    print(f'check-spec-leak: {e}', file=sys.stderr)
    sys.exit(1)
PY
