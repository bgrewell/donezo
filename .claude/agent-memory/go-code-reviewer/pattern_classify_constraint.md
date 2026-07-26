---
name: pattern-classify-constraint
description: donezo's classifyConstraint sentinel-error convention for mapping SQLite constraint codes to ErrDuplicateID/ErrInvalidReference — and where the retrofit missed a spot.
metadata:
  type: project
---

`internal/store/store.go` defines `classifyConstraint(err error) error`,
which type-asserts to `*sqlite.Error` (the modernc.org/sqlite driver,
imported under the name `sqlite`/`sqlite3` specifically so
`sqlite3.SQLITE_CONSTRAINT_*` codes are inspectable) and maps:
- `SQLITE_CONSTRAINT_PRIMARYKEY` / `_UNIQUE` → wraps `ErrDuplicateID`
- `SQLITE_CONSTRAINT_FOREIGNKEY` → wraps `ErrInvalidReference`

The convention: **every** insert/update helper in `internal/store` is
supposed to pass its exec error through `classifyConstraint` before
wrapping with `fmt.Errorf(...: %w)`, so the API layer's
`writeStoreError` (in `internal/api/mutations.go`) can turn these into
409/400 instead of an opaque 500.

**Known gap as of 2026-07-26** (feature/backend-core branch, mutation-API
session): the classify-everywhere retrofit touched every
insert/execUpdate* pair in `internal/store/space.go` (Create/Update for
Project, Activity, Task, Reminder, InboxItem — including extracting
`execUpdate*` helpers shared with the new `Patch*` methods in
`patch.go`) **except `UpdateNote`**, which still does a raw
`fmt.Errorf("...: %w", n.ID, err)` with no classification. Also,
`CoreStore.PatchSpace`'s UPDATE statement (new in that session) was
never wrapped in `classifyConstraint` either — though currently
harmless there since the columns it writes (name/color/position/
archived_at) can't trigger a PK/UNIQUE/FK violation given the schema.

**Lesson for future reviews**: when a commit message/PR claims "all
insert/update paths now classify" (or similar sweeping claims), don't
take it at face value — grep every `ExecContext` call inside
`internal/store/*.go` that isn't already wrapped, and diff against
`git diff <base>...<branch>` to see exactly which call sites were
touched vs. left alone. The asymmetry is easy to miss because the
sibling `insertNote` **was** updated (it's classified), making
`UpdateNote`'s omission look like an oversight rather than a deliberate
choice — there's no comment explaining why notes update differently
from every other entity's update path.

See also [[architecture-donezo-store]].
