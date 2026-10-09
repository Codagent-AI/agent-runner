#!/bin/sh
set -eu
payload=$(cat)
ARCHIVE_SCRIPT_DIR="$(dirname "$0")" EXTERNAL_ARCHIVE_PAYLOAD="$payload" python3 - <<'PY'
import json
import os
import re
import sys
import tempfile
from pathlib import Path

sys.dont_write_bytecode = True
sys.path.insert(0, os.environ['ARCHIVE_SCRIPT_DIR'])
from archive_snapshot import snapshot

try:
    p = json.loads(os.environ['EXTERNAL_ARCHIVE_PAYLOAD'])
    name = p['change_name']
    if not isinstance(name, str) or not re.fullmatch(r'[a-z0-9][a-z0-9-]*', name):
        raise ValueError('invalid change_name')
    root = Path(p['spec_root']).resolve(strict=True)
    session = Path(p['session_dir']).resolve(strict=True)
    record_path = session / 'output/archive-transition' / (name + '-external.json')
    record = json.loads(record_path.read_text())
    if (record.get('spec_root'), record.get('change_name'), record.get('run_id'), record.get('archive_absent_at_start')) != (str(root), name, session.name, True):
        raise ValueError(f'mismatched archive transition record: {record_path}')
    active = root / 'openspec/changes' / name
    if active.exists():
        raise ValueError(f'active change directory still exists: {active}')
    archives = list((root / 'openspec/changes/archive').glob('[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]-' + name))
    if len(archives) != 1 or not archives[0].is_dir():
        raise ValueError(f'expected exactly one new archive for {name}: {archives}')
    archive = archives[0]
    canonical, other = snapshot(root, archive)
    old = record['other']
    foreign = sorted(k for k in old.keys() | other.keys() if old.get(k) != other.get(k))
    if foreign:
        raise ValueError('files outside the archive and canonical specs changed: ' + ', '.join(foreign))
    old = record['canonical']
    record['canonical_changes'] = dict(
        added=sorted(canonical.keys() - old.keys()),
        modified=sorted(k for k in canonical.keys() & old.keys() if canonical[k] != old[k]),
        deleted=sorted(old.keys() - canonical.keys()))
    record['archive_dir'] = archive.relative_to(root).as_posix()
    fd, temporary = tempfile.mkstemp(dir=record_path.parent, suffix='.tmp')
    try:
        with os.fdopen(fd, 'w') as f:
            json.dump(record, f)
            f.flush()
            os.fsync(f.fileno())
        os.replace(temporary, record_path)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)
    print(f'Verified external archive: {record["archive_dir"]}')
except (OSError, ValueError, KeyError) as e:
    print(f'verify-archive-external: {e}', file=sys.stderr)
    sys.exit(1)
PY
