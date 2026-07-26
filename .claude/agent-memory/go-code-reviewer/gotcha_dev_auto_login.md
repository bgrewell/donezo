---
name: gotcha-dev-auto-login
description: donezod's --dev-auto-login flag hardcodes user id=1 via StaticAuthenticator without verifying that row actually exists in core.db — breaks any FK-backed write (e.g. POST /api/spaces) unless --seed also ran.
metadata:
  type: project
---

`cmd/donezod/main.go`'s `serve()` wires `--dev-auto-login` to
`api.StaticAuthenticator{User: store.User{ID: 1, Username: seed.Username,
DisplayName: seed.DisplayName}}` — a fixed user struct that is **never
checked against core.db**. If the data dir wasn't also seeded (via
`--seed <path>`), no row with id=1 exists in the `users` table.

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
