---
name: note-connection-cache-growth
description: SpaceStore's per-space *sql.DB cache never evicts entries — confirmed by live testing to cost ~3 file descriptors per space (WAL mode) for the life of the process, with no LRU/idle-timeout bound.
metadata:
  type: project
---

`internal/store/space.go`'s `SpaceStore.conns map[string]*sql.DB` is
populated by `db(ctx, spaceID)` on first use and only ever cleared by
`Close()` (full server shutdown). There is no eviction: no LRU, no idle
timeout, no cap on map size. `handleCreateSpace` (internal/api/spaces.go)
calls `EnsureSpace` synchronously right after creating the registry row,
so **every space created immediately and permanently pins an open
connection** for the rest of the process's life, whether or not the
space is ever touched again.

Measured live on 2026-07-26 (donezod built from feature/backend-core,
`--dev-auto-login --seed seed/seed.json`): creating 50 spaces via
`POST /api/spaces` grew the process's open FD count from 22 to 172 —
roughly 3 FDs per space, consistent with SQLite WAL mode's three files
per db (`<id>.db`, `<id>.db-wal`, `<id>.db-shm`).

This is an accepted architectural tradeoff for a single-user personal
app (few spaces expected in practice), and the file's own doc comment
defends the one-long-lived-connection-per-file choice on correctness
grounds (avoiding SQLITE_BUSY churn from a real pool) — but it says
nothing about bounding the *number* of cached files. Worth re-flagging
in any future review if usage patterns change (e.g. bulk space import,
multi-tenant deployment, or very long-lived processes), or if a
`DeleteSpace` / space-archival path is added that should also evict the
cached connection and unlink the space's db file.
