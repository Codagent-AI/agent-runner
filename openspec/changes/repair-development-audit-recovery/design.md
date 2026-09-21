# Design

Unsafe optional notes are omitted with a reason-only diagnostic; structural
validation remains fatal. Delivery state has an independent reporting warning,
so completion cannot erase it. Reconciliation acts only on a durable automatic
`reserved` link and uses its original audit identity. The recovery script scans
only recorded lifecycle links for real Git project roots, defaults to dry-run,
and runs one recovery at a time.

GitHub issue bodies are supplied to `gh` through `--body-file -`, which makes
stdin the explicit body source. The issue-repair tool reads only locally
recorded created findings, then edits an issue only when GitHub confirms it is
an `[auto-audit]` issue whose body remains the historical `-` placeholder.
