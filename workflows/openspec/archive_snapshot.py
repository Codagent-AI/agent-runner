"""Snapshot archive ownership without buffering whole files or walking excluded trees."""
import hashlib
import os
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
