-- Source for the redacted Cursor chat-store durability fixtures. Row shapes
-- were captured from a real ~/.cursor/chats/<workspace>/<chat>/store.db
-- (cursor-agent 2026.07.16-899851b); text and identifiers are redacted.
--
-- Three fixture databases are generated from these statements:
--   store.db              baseline section only
--   store-intermediate.db baseline + intermediate sections: in-progress
--                         mutations that must NOT count as committed-turn
--                         evidence (updated assistant text, a new assistant
--                         text row, a tool call, and an unrelated tool result)
--   store-committed.db    all sections: intermediate + the persisted
--                         completion-client tool result carrying the
--                         acceptance receipt
--                         3f6bb84a-97c5-4a02-b7f1-8a2f4f4e9d1c

-- Baseline (store.db)
CREATE TABLE blobs (id TEXT PRIMARY KEY, data BLOB);
CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT);
INSERT INTO blobs (id, data) VALUES
  ('system-baseline', CAST('{"role":"system","content":"redacted system prompt"}' AS BLOB)),
  ('user-baseline', CAST('{"role":"user","content":[{"type":"text","text":"work"}],"id":"user-1"}' AS BLOB)),
  ('assistant-baseline', CAST('{"role":"assistant","content":[{"type":"redacted-reasoning","data":"redacted","providerOptions":{"cursor":{"modelName":"composer-2.5"}}},{"type":"text","text":"baseline"}],"id":"assistant-1"}' AS BLOB));
INSERT INTO meta (key, value) VALUES ('version', '1');

-- Intermediate additions (store-intermediate.db)
UPDATE blobs SET data = CAST('{"role":"assistant","content":[{"type":"redacted-reasoning","data":"redacted","providerOptions":{"cursor":{"modelName":"composer-2.5"}}},{"type":"text","text":"baseline updated"}],"id":"assistant-1"}' AS BLOB) WHERE id = 'assistant-baseline';
INSERT INTO blobs (id, data) VALUES
  ('assistant-progress', CAST('{"role":"assistant","content":[{"type":"text","text":"still working"}],"id":"assistant-2"}' AS BLOB)),
  ('assistant-tool-call', CAST('{"role":"assistant","content":[{"type":"tool-call","toolCallId":"tool-2","toolName":"Read","args":{"path":"/repo/file.go"}}],"id":"assistant-3"}' AS BLOB)),
  ('tool-result-unrelated', CAST('{"role":"tool","content":[{"type":"tool-result","toolCallId":"tool-2","toolName":"Read","result":"redacted file contents"}],"id":"tool-2"}' AS BLOB));

-- Committed addition (store-committed.db): the completion client's persisted
-- shell exchange. Its recorded stdout contains the receipt printed after the
-- control server acknowledged the completion.
INSERT INTO blobs (id, data) VALUES
  ('tool-result-completion', CAST('{"role":"tool","content":[{"type":"tool-result","toolCallId":"tool-3","toolName":"Shell","result":"Exit code: 0\n\nCommand output:\n\n```\nagent-runner: step completion requested\nagent-runner completion receipt 3f6bb84a-97c5-4a02-b7f1-8a2f4f4e9d1c\n```\n"}],"id":"tool-3"}' AS BLOB)),
  ('assistant-committed', CAST('{"role":"assistant","content":[{"type":"redacted-reasoning","data":"redacted","providerOptions":{"cursor":{"modelName":"composer-2.5"}}},{"type":"text","text":"done"}],"id":"assistant-4"}' AS BLOB));
