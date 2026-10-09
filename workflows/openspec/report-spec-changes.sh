#!/bin/sh
set -eu
payload=$(cat)
SPEC_REPORT_PAYLOAD="$payload" python3 - <<'PY'
import json
import os
import subprocess
import sys
from pathlib import Path

try:
    p = json.loads(os.environ['SPEC_REPORT_PAYLOAD'])
    root = Path(p['spec_root']).resolve(strict=True)
    prefix = subprocess.run(['git', '-C', str(root), 'rev-parse', '--show-prefix'], capture_output=True)
    if prefix.returncode == 0:
        prefix = prefix.stdout.decode().strip()
        status = subprocess.run(['git', '--no-optional-locks', '-C', str(root), 'status', '--porcelain=v1', '-z', '--untracked-files=all', '--', '.'], capture_output=True, check=True)
        entries = iter(status.stdout.decode().split('\0'))
        print('Uncommitted spec-root files (commit these separately):')
        for entry in entries:
            if not entry:
                continue
            code, path = entry[:2], entry[3:]
            paths = [path]
            if 'R' in code or 'C' in code:
                paths.append(next(entries))
            for path in paths:
                if path.startswith(prefix):
                    print(f'{code} {path[len(prefix):]}')
    else:
        print('Spec root is not under version control; commit these changes separately:')
        record_path = Path(p['session_dir']) / 'output/archive-transition' / (p['change_name'] + '-external.json')
        if record_path.exists():
            record = json.loads(record_path.read_text())
            if record.get('spec_root') != str(root) or record.get('change_name') != p['change_name']:
                raise ValueError('mismatched archive transition record')
            print(record.get('archive_dir', 'openspec/changes/' + p['change_name']))
            for kind, paths in record.get('canonical_changes', {}).items():
                for path in paths:
                    print(f'{kind}: {path}')
        else:
            print('openspec/changes/' + p['change_name'])
except (OSError, ValueError, KeyError, subprocess.CalledProcessError) as e:
    print(f'report-spec-changes: {e}', file=sys.stderr)
    sys.exit(1)
PY
