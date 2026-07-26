---
name: testing-conventions-donezo
description: donezo's Go test style — table-driven tests with t.Parallel() everywhere, the tt:=tt loopvar-capture comment convention, and the newTestServer/newTestSpaceStore fixture shapes.
metadata:
  type: project
---

Test quality in this repo is consistently high and worth treating as the
baseline to hold new PRs to, not just a nice-to-have:

- Almost every test is table-driven (`tests := []struct{...}`) with
  `t.Parallel()` called both on the outer test func and inside each
  `t.Run` subtest closure.
- Loop variable capture uses the explicit idiom
  `tt := tt // capture for parallel subtests (golangci-lint predates Go
  1.22 loopvar)` before `t.Run(tt.name, func(t *testing.T) { t.Parallel()
  ... })` — even though Go 1.22 fixed loop-var capture, they keep the
  explicit copy + comment repo-wide (module is `go 1.22.4`). Flag it as a
  style nit if a new test omits this pattern while sibling tests in the
  same file have it — consistency, not correctness.
- API tests (`internal/api/*_test.go`) build a full `*Server` via
  `newTestServer(t)` (in `server_test.go`): temp dir, fixed clock
  (`fixedClock` → 2026-07-26T12:00:00Z), two users ("ben" owns space
  "sandbox" with project "loom"; "other" owns space "private"),
  `StaticAuthenticator{User: ben}`. Every foreign-space-isolation test
  case targets `/api/spaces/private/...` and expects 404 "space not
  found". New mutation endpoints should always get a same-shaped
  "create/patch/delete in foreign space is 404" case — the existing
  suite (`mutations_test.go`) does this for every entity type.
- Store tests use `newTestSpaceStore(t)` / `newTestCoreStore(t)` +
  `testSpace` constant + `fixedNow` constant + `mustCreateProject`
  helper (`mutate_test.go`, `space_test.go`). `ptr(v)` is the generic
  pointer-of helper used throughout for optional fields.
- Error-path assertions consistently use `errors.Is(err, wantErr)`
  rather than string matching, except where the PR intentionally checks
  the exact user-facing message (`wantInBody` field in API table tests).
- Atomicity/rollback tests always re-fetch state after a failure to
  assert nothing partially committed (see
  `TestConvertInboxItem/failure rolls the whole conversion back` and its
  API-level twin in `mutations_test.go`) — this is the right pattern to
  expect for any new transactional store method.

`golangci-lint run ./...` (v1.55.2) and `gofmt -l .` are clean on this
branch as of 2026-07-26; `go test -race ./...` passes in full (~10s for
the whole module).
