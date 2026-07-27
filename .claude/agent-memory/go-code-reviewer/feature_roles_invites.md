---
name: feature-roles-invites
description: Security review notes on donezo's roles + invite-code registration feature (migration 0002, internal/store/invites.go, internal/api/invites.go, internal/auth/invite.go) — what's solid, what's a real gap.
metadata:
  type: project
---

Reviewed 2026-07-27 on branch feature/backend-core (uncommitted working
tree). This is a well-built, well-tested feature — table-driven store
and API tests, a real concurrent-goroutine double-claim-race test
(`TestAuthRegisterDoubleClaimRace`, 8 racers via `sync.WaitGroup`), a
dedicated no-logging test (`TestInviteCodeNeverLogged`), and a real
v1-schema-fixture migration upgrade test
(`TestCoreMigrationUpgradeFromV1`) covering "credentialed user exists",
"nobody credentialed yet", and "empty users table". `go test -race
-count=1 ./internal/...`, `gofmt -l`, and `golangci-lint run ./...` were
all clean. Live curl probing against a scratch `/tmp` instance
confirmed the double-claim race (exactly 1 winner of 8), admin-guard
(401 anon / 403 member on all three `/api/invites*` routes), and the
case-sensitivity finding below.

**Core mechanism**: `usableInviteGuard` (`used_at IS NULL AND
revoked_at IS NULL AND expires_at > ?`) is embedded directly in the
claim `UPDATE` inside `RegisterInvitedUser`'s transaction — same pattern
as `SetupOwner`'s `noCredentialedUserGuard`. This makes the check+claim
atomic at the SQL layer regardless of Go-level concurrency, and
`core.db`'s `MaxOpenConns(1)` serializes writers as defense in depth on
top of that. Good pattern to expect/require for any future
"claim exactly once" feature in this codebase.

**Real gap found (medium)**: `compensateRegistration` (internal/api/invites.go)
unwinds a committed `RegisterInvitedUser` when the follow-up
`EnsureSpace` (create the space's content db file) fails. It does three
separate calls — `ReleaseInvite` → `DeleteSpace` → `DeleteUser` — each
gating the next (correct FK order: null `invites.used_by` before
deleting the space row, then the user row). But if step 1 succeeds and
step 2 or 3 then fails, the invite is claimable again (released) while
the original half-registered user+space rows still exist — a second
claimant can then create a *second* account off the same code, breaking
the "exactly one account per invite" invariant the rest of the design
works hard to guarantee. This isn't purely theoretical: `EnsureSpace`
failing (can't create the new space's SQLite file) and a subsequent
core.db `DELETE` failing are both plausible under the same root cause
(disk exhaustion on the data dir), since both need to write WAL/journal
data. No test exercises this path (would need a faulty/failing
`SpaceStore` injected into `Server`). Worth re-checking if this code
changes, or flagging again if a similar multi-step compensation flow
gets added elsewhere.

**Real gap found (low)**: invite codes are documented as human-retype-
friendly ("dz-XXXXX-XXXXX...so a code survives being read aloud or
retyped", `internal/auth/invite.go`) but `HashInviteCode` hashes the raw
string with no case-folding, and nothing uppercases user input before
hashing (`RegisterScreen.tsx` only uppercases via CSS
`text-transform`, not the actual submitted value). Confirmed live: POSTing
a lowercased valid code gets the generic "invalid or expired invite
code" 403. A legitimate user who retypes a code in lowercase is
silently locked out with no hint the case is the problem.

**Real gap found (low)**: `RegisterInvitedUser`'s claim UPDATE runs
before the username-uniqueness INSERT, so a 409 "username is already
taken" doesn't burn the invite (confirmed by
`TestAuthRegisterUsernameTaken`, "The failed attempt must not burn the
invite"). That's the intended behavior for legitimate retry-after-typo
UX, but it also means anyone holding one valid, unclaimed invite code
can repeatedly probe arbitrary usernames via 409/403 without spending
the code — a username-existence oracle gated only by "has one invite
code," not "is an admin." Given usernames aren't treated as secret
elsewhere in the app and the precondition is already a trusted invite,
this is low severity, but it's a real, not-fully-closed oracle worth
knowing about if the threat model ever tightens.

**Not a bug — verified defense in depth**: `RegisterInvitedUser`
hashes the password with argon2 *before* validating the invite code
(builder's own note), which — like `handleAuthLogin`'s dummy-hash
branch — equalizes response timing between valid and invalid codes so
total request time doesn't leak code state. Correctly reasoned, matches
the login pattern.

See also [[architecture-donezo-store]], [[pattern-classify-constraint]],
[[gotcha-dev-auto-login]] (the dev-auto-login fix was already on this
branch, unrelated to this diff, discovered incidentally while reading
the README's security-posture section).
