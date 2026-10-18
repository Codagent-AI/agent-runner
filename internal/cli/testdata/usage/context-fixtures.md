The Claude context stream covers repeated message IDs, cache categories, a child
conversation, a synthetic assistant, a compaction boundary, and the result's model
window. The metadata regression test additionally covers the nested cache creation
and service-tier fields observed in real Claude output on 2026-10-18.

The Codex context rollout retains the per-response payload shape observed in a
local Codex 0.160+ rollout on 2026-10-18. Identifiers and counts are sanitized. The
sequence includes a prior invocation, a main response, a compaction response joined
through compaction_response_id, another thread, and a synthetic token_count. The
private-home integration test derives interrupted and old-format cases from it.
