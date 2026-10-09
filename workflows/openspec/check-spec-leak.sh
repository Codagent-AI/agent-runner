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
    patterns = [re.compile(r'(?<![\w./~-])' + re.escape(root.rstrip('/')) + r'(?![\w.-])') for root in roots]
    default = subprocess.run(['git', 'symbolic-ref', '--quiet', 'refs/remotes/origin/HEAD'], capture_output=True, text=True)
    candidates = [default.stdout.strip()] if default.returncode == 0 else ['origin/main', 'origin/master', 'main', 'master']
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
    pr = subprocess.run(['gh', 'pr', 'view', '--json', 'number'], capture_output=True, text=True)
    if pr.returncode == 0:
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
