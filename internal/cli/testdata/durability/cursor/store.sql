CREATE TABLE blobs (id TEXT PRIMARY KEY, data BLOB);
CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT);
INSERT INTO blobs (id, data) VALUES
  ('system-baseline', CAST('{"role":"system","content":"redacted system prompt"}' AS BLOB)),
  ('user-baseline', CAST('{"role":"user","content":[{"type":"text","text":"work"}],"id":"user-1"}' AS BLOB)),
  ('assistant-baseline', CAST('{"role":"assistant","content":[{"type":"redacted-reasoning","data":"redacted","providerOptions":{"cursor":{"modelName":"composer-2.5"}}},{"type":"text","text":"baseline"}],"id":"assistant-1"}' AS BLOB));
INSERT INTO meta (key, value) VALUES ('version', '1');
