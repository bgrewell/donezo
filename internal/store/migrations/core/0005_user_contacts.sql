-- Where a person's reminders can reach them (#52).
--
-- Reminders have always been in-app only, which means they arrive when you
-- are already looking at the thing that would have reminded you. This is the
-- other end: an address or number a reminder can be delivered to.
--
-- A destination is UNVERIFIED until its owner proves they can receive at it.
-- That is not ceremony — without it, a signed-in user could point donezo at
-- somebody else's phone and have the instance text a stranger on a schedule,
-- which is a spam cannon with a nice UI. verified_at NULL means nothing is
-- ever sent there.
--
-- The challenge is stored hashed, like every other secret in this database.
-- SHA-256 rather than argon2 because a six-digit code is not a password: it
-- lives for minutes, survives five wrong guesses, and proves control of an
-- address rather than an identity. Attempt limiting and expiry are what make
-- it safe; the hash only stops a leaked core.db from being replayed.
--
-- ON DELETE CASCADE for the same reason user_settings has it: DeleteUser
-- requires referencing rows to be gone, and foreign keys are enforced.
CREATE TABLE user_contacts (
    id              TEXT PRIMARY KEY,
    user_id         INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    channel         TEXT NOT NULL,
    address         TEXT NOT NULL,
    label           TEXT NOT NULL DEFAULT '',
    verified_at     TEXT,
    code_hash       TEXT NOT NULL DEFAULT '',
    code_expires_at TEXT,
    code_attempts   INTEGER NOT NULL DEFAULT 0,
    code_sent_at    TEXT,
    created_at      TEXT NOT NULL,

    -- One row per destination. Adding the same number twice would otherwise
    -- deliver every reminder to it twice.
    UNIQUE (user_id, channel, address)
);

CREATE INDEX idx_user_contacts_user_id ON user_contacts (user_id);
