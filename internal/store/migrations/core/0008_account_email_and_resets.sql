-- Account email + password reset.
--
-- users.email is a recovery/contact address, collected at signup going forward
-- and NULL for accounts that predate it (existing members reset via a verified
-- email contact instead, until they have an account email). It is unique
-- case-insensitively among non-NULL values — an email identifies at most one
-- account, so a reset request for it is never ambiguous — while many accounts
-- may share the NULL "no email yet" state (a partial index allows that).
ALTER TABLE users ADD COLUMN email TEXT;

CREATE UNIQUE INDEX idx_users_email_nocase
    ON users (email COLLATE NOCASE)
    WHERE email IS NOT NULL;

-- Single-use, expiring password-reset tokens. Only the SHA-256 of the token is
-- stored, exactly like sessions and invites, so a leaked core.db cannot be
-- replayed as a reset link. used_at makes a token single-use: it is stamped
-- the moment the token is spent, and the spend is a guarded UPDATE so two
-- racing redemptions cannot both win. Rows cascade when the user is removed.
CREATE TABLE password_resets (
    token_hash TEXT PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    used_at    TEXT
);

CREATE INDEX idx_password_resets_user ON password_resets (user_id);
