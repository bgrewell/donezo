-- Per-user preferences, stored as one JSON document per user.
--
-- Settings are a small but open-ended set (appearance today, an LLM
-- configuration next), so they live in a single JSON column rather than one
-- column per preference: adding a preference becomes a Go-side change with
-- no migration. This mirrors how the space databases store their own
-- structured values (tags, links, alt_next_actions) as JSON text.
--
-- The row is created on first write. A user who has never saved a
-- preference has no row and reads as defaults, so absence is a normal
-- resting state rather than an error.
--
-- ON DELETE CASCADE because core.db's DeleteUser (and the seed cleanup that
-- calls it) requires referencing rows to be gone before the user row can be
-- removed, and foreign keys are enforced.
CREATE TABLE user_settings (
    user_id    INTEGER PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    settings   TEXT NOT NULL DEFAULT '{}',
    updated_at TEXT NOT NULL
);
