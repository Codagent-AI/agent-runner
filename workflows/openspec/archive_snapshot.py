"""Archive ownership snapshots and atomic transition records."""
import hashlib
import json
import os
import tempfile
from pathlib import Path


def snapshot(root, excluded):
    canonical, other = {}, {}
    for directory, dirs, files in os.walk(root):
        base = Path(directory)
        dirs[:] = sorted(d for d in dirs if d != '.git' and base / d != excluded)
        for name in sorted(files):
            path = base / name
            if name == '.git' or path == excluded or not path.is_file():
                continue
            rel = path.relative_to(root)
            digest = hashlib.sha256()
            with path.open('rb') as source:
                for chunk in iter(lambda: source.read(1024 * 1024), b''):
                    digest.update(chunk)
            target = canonical if rel.parts[:2] == ('openspec', 'specs') else other
            target[rel.as_posix()] = digest.hexdigest()
    return canonical, other


def write_record(path, record, prefix=None):
    """Atomically replace a transition record after flushing its contents."""
    fd, temporary = tempfile.mkstemp(dir=path.parent, prefix=prefix, suffix='.tmp')
    try:
        with os.fdopen(fd, 'w') as f:
            json.dump(record, f)
            f.flush()
            os.fsync(f.fileno())
        os.replace(temporary, path)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)
