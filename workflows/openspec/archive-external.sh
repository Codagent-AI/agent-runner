#!/bin/sh
set -eu
payload=$(cat)
EXTERNAL_ARCHIVE_PAYLOAD="$payload" python3 - <<'PY'
import hashlib
import json
import os
import re
import subprocess
import sys
import tempfile
from pathlib import Path

try:
    p = json.loads(os.environ['EXTERNAL_ARCHIVE_PAYLOAD'])
    name = p['change_name']
    if not isinstance(name, str) or not re.fullmatch(r'[a-z0-9][a-z0-9-]*', name):
        raise ValueError('invalid change_name')
    raw_root = Path(p['spec_root'])
    if not raw_root.is_absolute():
        raise ValueError('spec_root must be absolute')
    root = raw_root.resolve(strict=True)
    if not (root / 'openspec').is_dir():
        raise ValueError(f'not an OpenSpec project: {root}')
    session = Path(p['session_dir']).resolve(strict=True)
    record_path = session / 'output/archive-transition' / (name + '-external.json')
    active = root / 'openspec/changes' / name
    archives = list((root / 'openspec/changes/archive').glob('*-' + name))
    if record_path.exists():
        record = json.loads(record_path.read_text())
        if (record.get('spec_root'), record.get('change_name'), record.get('run_id'), record.get('archive_absent_at_start')) != (str(root), name, session.name, True):
            raise ValueError(f'mismatched archive transition record: {record_path}')
    else:
        if archives:
            raise ValueError(f'pre-existing archive: {archives[0]}')
        if not active.is_dir():
            raise ValueError(f'missing active change directory: {active}')
        canonical, other = {}, {}
        for path in sorted(root.rglob('*')):
            rel = path.relative_to(root)
            if '.git' in rel.parts or rel == Path('openspec/changes') / name or active in path.parents or not path.is_file():
                continue
            target = canonical if rel.parts[:2] == ('openspec', 'specs') else other
            target[rel.as_posix()] = hashlib.sha256(path.read_bytes()).hexdigest()
        record = dict(spec_root=str(root), change_name=name, run_id=session.name,
                      archive_absent_at_start=True, canonical=canonical, other=other)
        record_path.parent.mkdir(parents=True, exist_ok=True)
        fd, temporary = tempfile.mkstemp(dir=record_path.parent, prefix=name + '-', suffix='.tmp')
        try:
            with os.fdopen(fd, 'w') as f:
                json.dump(record, f)
                f.flush()
                os.fsync(f.fileno())
            os.replace(temporary, record_path)
        finally:
            if os.path.exists(temporary):
                os.unlink(temporary)
    if active.exists():
        subprocess.run(['openspec', 'validate', '--type', 'change', name], cwd=root, check=True)
        subprocess.run(['openspec', 'archive', name, '--yes'], cwd=root, check=True)
    print(f'External archive transition recorded at {record_path}')
except (OSError, ValueError, KeyError, subprocess.CalledProcessError) as e:
    print(f'archive-external: {e}', file=sys.stderr)
    sys.exit(1)
PY
