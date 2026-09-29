#!/bin/sh
set -eu

python3 -c '
import json
import os
import sys
import tempfile

payload = json.load(sys.stdin)
directory = payload["session_dir"]
report = payload["report"]
if not isinstance(report, str) or not isinstance(directory, str) or not os.path.isdir(directory):
    sys.exit("ci-report-artifact: invalid report or session directory")
with tempfile.NamedTemporaryFile(mode="w", encoding="utf-8", prefix="ci-report-",
                                 suffix=".txt", dir=directory, delete=False) as artifact:
    artifact.write(report)
    sys.stdout.write(artifact.name)
'
