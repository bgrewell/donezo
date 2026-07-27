-- Roles + invite-code registration.
--
-- users gains a role: 'admin' (the instance owner, who may mint invites)
-- or 'member' (everyone who joins through an invite code). Databases
-- created before this migration predate roles, so the backfill promotes
-- the lowest-id credentialed user — the account first-run setup created —
-- to admin; when nobody has completed setup yet, the lowest-id user is
-- promoted instead (setup claims that row). On an empty users table the
-- UPDATE matches nothing and first-run setup assigns admin itself.

ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'member'
    CHECK (role IN ('admin', 'member'));

UPDATE users SET role = 'admin' WHERE id = COALESCE(
    (SELECT MIN(id) FROM users WHERE password_hash <> ''),
    (SELECT MIN(id) FROM users)
);

-- Invite codes. The plaintext code is shown exactly once, at creation:
-- only its SHA-256 (code_hash) is stored, so a leaked database does not
-- leak claimable codes. code_prefix keeps the first characters of the
-- rendered code so the admin list can identify an invite without being
-- able to reconstruct it.
CREATE TABLE invites (
    id          TEXT PRIMARY KEY,
    code_hash   TEXT UNIQUE NOT NULL,
    code_prefix TEXT NOT NULL,
    created_by  INTEGER NOT NULL REFERENCES users (id),
    created_at  TEXT NOT NULL,
    expires_at  TEXT NOT NULL,
    used_by     INTEGER REFERENCES users (id),
    used_at     TEXT,
    revoked_at  TEXT
);
