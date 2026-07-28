-- core.db initial schema: cross-space registry only.
-- Space content never lives here; each space has its own database file.

CREATE TABLE users (
    id            INTEGER PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE,
    display_name  TEXT NOT NULL,
    password_hash TEXT NOT NULL DEFAULT '',
    created_at    TEXT NOT NULL
);

CREATE TABLE sessions (
    token_hash   TEXT PRIMARY KEY,
    user_id      INTEGER NOT NULL REFERENCES users (id),
    created_at   TEXT NOT NULL,
    expires_at   TEXT NOT NULL,
    last_seen_at TEXT
);

CREATE TABLE spaces (
    id          TEXT PRIMARY KEY,
    user_id     INTEGER NOT NULL REFERENCES users (id),
    name        TEXT NOT NULL,
    color       TEXT NOT NULL,
    position    INTEGER NOT NULL DEFAULT 0,
    archived_at TEXT,
    created_at  TEXT NOT NULL
);
