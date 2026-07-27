---
name: gotcha-dev-auto-login
description: FIXED as of the roles/invites session (2026-07-26/27) — cmd/donezod/main.go now calls seed.EnsureDevUser to create the dev user row if missing, instead of hardcoding an unverified id=1. Kept for history; don't cite the old bug as current.
metadata:
  type: project
---

**Status: fixed, superseded.** As of a review on 2026-07-27 (branch
feature/backend-core, roles+invites session), `cmd/donezod/main.go`'s
`serve()` calls `seed.EnsureDevUser(ctx, core)` before wiring
`StaticAuthenticator`, and the README's security-posture section now
documents this: "on an unseeded data dir the dev user row is created at
startup so user-scoped writes like `POST /api/spaces` work without
`--seed`." The fix was already present on the branch before this
session started (not part of the roles/invites diff itself), so it must
have landed in an earlier commit. Verify `seed.EnsureDevUser` still
exists before citing this if a lot of time has passed.

Original bug description below, kept for context on what was wrong and
why the fix matters:

`cmd/donezod/main.go`'s `serve()` used to wire `--dev-auto-login` to
`api.StaticAuthenticator{User: store.User{ID: 1, Username: seed.Username,
DisplayName: seed.DisplayName}}` — a fixed user struct that was **never
checked against core.db**. If the data dir wasn't also seeded (via
`--seed <path>`), no row with id=1 existed in the `users` table.

Consequence, reproduced live on 2026-07-26: starting
`donezod --data-dir /tmp/x --dev-auto-login` (no `--seed`) and then
`POST /api/spaces` fails with a 500 ("internal error"), because
`CoreStore.CreateSpaceAtEnd` inserts a `spaces` row with
`user_id = 1`, and `spaces.user_id REFERENCES users(id)` has no
matching row — SQLite raises `SQLITE_CONSTRAINT_FOREIGNKEY`, which
`classifyConstraint` maps to `ErrInvalidReference`, but
`handleCreateSpace` doesn't special-case that (only `ErrDuplicateID` for
its retry loop), so it falls through to a generic 500. The real cause is
only visible in the server log:
`create space: store: create space "...": invalid reference: constraint
failed: FOREIGN KEY constraint failed`.

**Workaround for testing**: always pass `--seed seed/seed.json` (repo
root) together with `--dev-auto-login`; the seed creates user "ben" with
id=1 for real, matching `internal/seed/seed.go`'s `Username = "ben"`,
`DisplayName = "Ben"` constants (which is exactly what
`StaticAuthenticator` hardcodes — the two are meant to line up, but
nothing enforces it).

**Not a security issue** — `--dev-auto-login` is already gated to
`/tmp` data dirs or an explicit consent env var, and the documented
setup flow (`/api/auth/setup` → `SetupOwner`) always creates a real row,
so production auth is unaffected. It's a dev-workflow correctness bug:
the flag's own help text says "act as the seeded dev user" but nothing
guarantees seeding happened, and any write that touches a FK column tied
to the user (spaces.user_id) silently breaks with an unhelpful 500.

This surfaced specifically because `POST /api/spaces` is new — before
that endpoint existed, nothing ever inserted a row referencing the
dev-auto-login user id via HTTP, so the bug was latent.
