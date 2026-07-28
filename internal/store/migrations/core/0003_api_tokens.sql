-- Per-user API tokens for the MCP endpoint.
--
-- A token authenticates a user's LLM against /mcp with a bearer credential
-- instead of a session cookie. Like session tokens and invite codes, the
-- plaintext is shown exactly once (at creation): only its SHA-256
-- (token_hash) is stored, so a leaked core.db does not leak usable tokens.
-- token_prefix keeps the first characters of the rendered token so the
-- owner's listing can identify a token without being able to reconstruct
-- it. scope gates what the token may do: read the account, or read and
-- write.
CREATE TABLE api_tokens (
    id           TEXT PRIMARY KEY,
    user_id      INTEGER NOT NULL REFERENCES users (id),
    name         TEXT NOT NULL,
    token_hash   TEXT UNIQUE NOT NULL,
    token_prefix TEXT NOT NULL,
    scope        TEXT NOT NULL CHECK (scope IN ('read_only', 'read_write')),
    created_at   TEXT NOT NULL,
    last_used_at TEXT,
    revoked_at   TEXT
);

CREATE INDEX idx_api_tokens_user_id ON api_tokens (user_id);
