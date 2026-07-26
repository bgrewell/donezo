---
name: architecture-donezo-store
description: donezo's storage architecture — core.db registry plus one SQLite file per space, and the ownedSpace() authorization pattern used by every mutation handler.
metadata:
  type: project
---

donezo (github.com/bgrewell/donezo) is a personal task/project tracker.
Backend: `internal/store` (SQLite via modernc.org/sqlite, pure Go, no
cgo), `internal/api` (stdlib net/http with Go 1.22 method+pattern
routing, no router dependency).

**Storage split:**
- `core.db` (CoreStore): cross-space registry — users, sessions, spaces
  table. One long-lived `*sql.DB` with `MaxOpenConns(1)`.
- `<data-dir>/spaces/<id>.db` (SpaceStore): one file per space, holding
  that space's projects/activities/tasks/notes/reminders/inbox. No
  `space_id` column anywhere — **the file itself is the isolation
  boundary**. SpaceStore caches one `*sql.DB` per space id in a
  `map[string]*sql.DB` guarded by a mutex, opened+migrated lazily on
  first use via `db(ctx, spaceID)`.
- Every space DB connection also gets `MaxOpenConns(1)`/`MaxIdleConns(1)`
  — the documented reasoning (see store.go) is that SQLite serializes
  writers anyway, so one physical connection per file avoids
  SQLITE_BUSY churn from a real pool.

**Authorization pattern**: every space-scoped handler calls
`s.ownedSpace(w, r)` first (in `internal/api/server.go`), which resolves
`{id}` via `CoreStore.GetSpace` (a SQL lookup, not a filesystem access)
and checks `sp.UserID == user.ID`. Unknown space id and foreign-owned
space both answer plain 404 ("space not found") — ids are not
probeable. Handlers then use `sp.ID` (the DB-verified value), never the
raw path `{id}`, when calling into SpaceStore — so even if the path
value were attacker-controlled, it never reaches the filesystem/SQLite
layer un-verified. Space ids are also independently validated by
`store.ValidateSpaceID` (`^[a-z0-9][a-z0-9_-]{0,63}$`) inside
`SpaceStore.db()` as defense in depth before being used as a file name.

**Space id generation**: server-generated only, via
`internal/api/spaces.go: newSpaceID()` — slugified name (`slugifyName`,
max 40 chars) + `-` + random 8 hex chars, matching the frontend's
`newId()` shape. `handleCreateSpace` retries up to 3 times on
`ErrDuplicateID` from the random suffix.

**Sentinel errors**: see [[pattern-classify-constraint]].

**Wire contract**: Go struct field JSON tags are the frontend's
camelCase names, kept in exact sync with `web/src/domain/types.ts`
(enums, field names, optionality). `internal/store/types.go` documents
this explicitly. When reviewing store/api changes, diff enum unions in
`internal/api/validate.go` against `web/src/domain/types.ts` field by
field — this repo keeps them hand-synced, not generated, so drift is a
realistic risk (though as of 2026-07-26 they were exactly in sync across
all 7 enums: projectColors, projectStatuses, activityTypes,
activitySources, taskStatuses, itemKinds, inboxStatuses).

**Frontend mutation wiring**: `web/src/state/sync.ts` maps each
`AppAction` to exactly one API call; `web/src/state/AppStore.tsx`
defines the `AppAction` union. If a backend PATCH/DELETE endpoint has no
corresponding `AppAction` case in sync.ts, it's unreachable from the
actual app — check both sides when assessing "is this endpoint really
used yet" (e.g., as of 2026-07-26, `PATCH .../projects/{pid}` existed
server-side with no frontend caller yet — no `UPDATE_PROJECT` action
existed).
