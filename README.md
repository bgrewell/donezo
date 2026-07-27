# donezo

Personal work memory and attention system. Captures tasks, reminders, notes,
and completed work with minimal friction; recovers context after
interruptions; shows where effort actually went on a project-by-time
timeline (**Project Pulse**).

Monorepo:

- `web/` — frontend: React 18 + TypeScript + Vite + Tailwind CSS, built on the
  [@grewelltech/console](https://github.com/grewelltech/design-system) design
  system.
- Go backend **donezod** (`cmd/donezod`, `internal/`) — SQLite-backed API
  server. One SQLite file per space; `core.db` holds the users/spaces
  registry and sessions. Cookie-session authentication with argon2id
  passwords (phase 2).

## Development

### Frontend (`web/`)

The dev server runs as a **systemd user service** with hot reload, so the app
is always live at <http://localhost:5173>:

```sh
systemctl --user status donezo-dev     # health
journalctl --user -u donezo-dev -f     # logs
systemctl --user restart donezo-dev    # bounce after dependency changes
```

The unit file lives at `deploy/donezo-dev.service` (installed to
`~/.config/systemd/user/`). Manual alternative: `cd web && npm run dev`.

Other scripts (run inside `web/`):

```sh
npm run typecheck        # tsc --noEmit
npm run build            # typecheck + production build
npm run peek -- "#/timeline" .peek/t.png [--theme slate] [--full]
                         # Playwright screenshot of the running app
```

### Backend (donezod)

```sh
make build               # build web + bin/donezod
make test                # go test ./...
make lint                # gofmt + golangci-lint + go vet
make seed-json           # regenerate seed/seed.json from web mock data
bin/donezod --port 8787 --data-dir ~/.local/share/donezo --seed seed/seed.json
```

A systemd user unit for the backend dev loop lives at
`deploy/donezod-dev.service` (runs `bin/donezod` on port 8787 with data
under `~/.local/share/donezo-dev`; install to `~/.config/systemd/user/`
like `donezo-dev.service`).

Flags (each also has a `DONEZOD_*` env fallback): `--port`, `--data-dir`,
`--seed`, `--trust-proxy` (trust proxy headers: the **last**
`X-Forwarded-For` hop — the one the proxy itself appended — keys rate
limiting, and `X-Forwarded-Proto: https` marks session cookies `Secure`;
set it only when donezod sits directly behind a reverse proxy, since
without one those headers are attacker-controlled and are ignored), and
`--dev-auto-login` (see below).

API surface (`{id}` is a space id; unknown and foreign spaces both read
as `404`):

| Endpoint                                          | What it does                                                          |
| ------------------------------------------------- | --------------------------------------------------------------------- |
| `GET /api/healthz`                                 | Liveness — public                                                      |
| `GET /api/auth/status`                             | `{needsSetup, authenticated}` — public                                 |
| `POST /api/auth/setup`                             | First-run: create the owner + session; `409` after                     |
| `POST /api/auth/login`                             | `{username, password}` → session cookie + `{user}`                     |
| `POST /api/auth/logout`                            | Delete the session, expire the cookie                                  |
| `POST /api/auth/register`                          | `{code, username, displayName?, password}` → member account + `main` space + session |
| `GET /api/auth/me`                                 | `{user}` (includes `role`) or `401`                                    |
| `POST /api/invites`                                | Admin: `{expiresInDays?}` (default 7, capped 90) → `201 {invite}` with the code — shown **only here** |
| `GET /api/invites`                                 | Admin: all invites with derived `status` (`active`/`used`/`expired`/`revoked`) + usernames; never the code |
| `DELETE /api/invites/{id}`                         | Admin: revoke → `204` (idempotent)                                     |
| `GET /api/spaces`                                  | `{spaces}` — the requester's spaces                                    |
| `POST /api/spaces`                                 | `{name, color}` → `201 {space}`; id = name slug + random suffix        |
| `PATCH /api/spaces/{id}`                           | Any of `{name, color, position}` → `{space}`                           |
| `POST /api/spaces/{id}/archive` / `/unarchive`     | Stamp / clear `archivedAt` → `{space}`                                 |
| `GET /api/spaces/{id}/state`                       | Full space content (projects, activities, tasks, notes, reminders, inbox) |
| `POST /api/spaces/{id}/projects`                   | Create a project → `201`                                               |
| `PATCH /api/spaces/{id}/projects/{pid}`            | Any subset of mutable fields (incl. `nextAction`, `altNextActions`, `resumeContext`, `status`, `waitingOn`) |
| `POST /api/spaces/{id}/activities`                 | Create an activity entry → `201`                                       |
| `PATCH /api/spaces/{id}/activities/{aid}`          | Partial update                                                         |
| `DELETE /api/spaces/{id}/activities/{aid}`         | Delete → `204`                                                         |
| `POST /api/spaces/{id}/tasks`                      | Create a task → `201`                                                  |
| `PATCH /api/spaces/{id}/tasks/{tid}`               | Partial update                                                         |
| `POST /api/spaces/{id}/notes`                      | Create a note → `201`                                                  |
| `POST /api/spaces/{id}/reminders`                  | Create a reminder → `201`                                              |
| `PATCH /api/spaces/{id}/reminders/{rid}`           | Partial update                                                         |
| `POST /api/spaces/{id}/inbox`                      | Capture a raw item → `201` (works against any owned space — the cross-space capture path) |
| `PATCH /api/spaces/{id}/inbox/{iid}`               | Partial update                                                         |
| `POST /api/spaces/{id}/inbox/{iid}/convert`        | `{kind, task?\|note?\|reminder?\|activity?\|project?}` → atomically mark converted + insert; answers `{inbox, <kind>}` |

Mutation contract: entity creates accept client-generated ids (1–64 chars
of `a-z0-9-`, the frontend `newId()` shape) and answer `409` on a
duplicate; enums, dates (`yyyy-MM-dd`), and datetimes are validated
against `web/src/domain/types.ts` with field-specific `400` messages;
`PATCH` bodies may send any subset of mutable fields (unknown fields are
a `400`, `null` clears a clearable field); responses use the same JSON
shapes as `GET .../state`. Multi-step writes (convert, patches) are
transactional. Content mutations require the space to be unarchived —
writes into an archived space answer `409` (reads, rename, and
archive/unarchive still work), so archiving is a real write barrier, not
just something the UI hides.

Everything else under `/api/` requires a session; only `/api/healthz` and
`/api/auth/*` are public.

**First run:** start with `--seed seed/seed.json` (creates user `ben` with
*no password* — it cannot log in yet), then `GET /api/auth/status` reports
`needsSetup: true` and `POST /api/auth/setup` with username `ben` claims
the seeded account by setting its password. Setup with a fresh username on
an unseeded data dir works the same way; once any user has a password,
setup answers `409`. The once-only invariant is enforced atomically at the
SQL layer, so concurrent setup requests racing on a fresh instance produce
exactly one owner. The owner is the instance **admin**; databases created
before roles existed are migrated in place, promoting the first
credentialed user to admin.

**Roles & invites:** every user is `admin` or `member` (`/api/auth/me`
reports which; non-admins get `403 {"error":"admin required"}` on admin
endpoints). The admin mints invite codes — `dz-XXXXX-XXXXX`, Crockford
base32, ~50 bits — via `POST /api/invites`. The plaintext code appears
only in that one `201`: the database stores its SHA-256 plus a display
prefix, so codes can be listed and revoked but never re-read. A new user
redeems a code with `POST /api/auth/register`, which atomically claims
the invite (a single guarded `UPDATE`, so two racing registrations with
one code produce exactly one account), creates the member and their
first space `main`, and issues a session. Codes are matched
case-insensitively — `dz-abcde-fghjk` claims `dz-ABCDE-FGHJK` — so a
code survives being retyped, not just pasted. Unknown, used, expired,
and revoked codes all fail with the same `403 {"error":"invalid or
expired invite code"}` — registration is not an invite-state oracle —
while a taken username is its own `409` that does not consume the code.
That `409` is a deliberate, bounded disclosure: it is visible only to
someone the admin already handed a valid code (anonymous login keeps
its uniform `401`), and every registration attempt — including the
`409` — spends the shared login/setup rate-limit budget.

Security posture:

- Passwords: argon2id (64 MiB, t=1, p=4), PHC-encoded so parameters can
  evolve; verification is constant-time; minimum length 10; decoded
  parameters are capped (256 MiB) so a tampered row cannot turn
  verification into a memory bomb.
- Sessions: 32 random bytes in a `donezo_session` cookie (`HttpOnly`,
  `SameSite=Lax`, `Path=/`, `Secure` over TLS or — only with
  `--trust-proxy` — behind an `X-Forwarded-Proto: https` proxy); only
  the SHA-256 of the token is stored; 30-day absolute expiry; expired
  sessions swept hourly.
- Login/setup rate limit: 10 attempts per 5 minutes per client IP
  (`429` + `Retry-After`); IPv6 clients are aggregated by /64 so
  rotating addresses inside one allocation doesn't reset the budget.
- Uniform `401` for unknown user vs wrong password, with equalized
  argon2 work on both paths (no username oracle, by text or by timing).
- `--dev-auto-login` **disables authentication** (every request acts as
  the seeded dev user; on an unseeded data dir the dev user row is
  created at startup so user-scoped writes like `POST /api/spaces` work
  without `--seed`). It exists solely for frontend dev/tests and is
  refused unless the data dir is under `/tmp` or
  `DONEZOD_I_KNOW_WHAT_IM_DOING=1` is set. Never expose such an instance.

## Design system

GTech Console is consumed as a source `file:` dependency on `../design-system`
(the sibling clone of grewelltech/design-system, relative to the repo root —
declared as `file:../../design-system` in `web/package.json`, where `file:`
paths resolve relative to `web/`). Its tokens (`--gtc-*`), Tailwind preset,
and components are the base layer; donezo adds `--dz-*` tokens (project color
ramp, shell metrics) in `src/styles/app.css`.

Improvements made here that generalize (new tokens, component tweaks) should
be committed back to the design-system repo to keep it current.

### Theming

Every component references CSS variables only. A theme is a set of variable
overrides keyed by `data-theme` on `<html>` (see `src/styles/themes.css`),
applied by `ThemeProvider` and persisted to localStorage. Built-in themes:
`console` (default), `slate`, `paper`, and `blossom`. The Appearance menu
also carries two more axes, applied the same way: font set (`plex` default,
`inter`, `system`) and text size (`small`, `medium` default, `large`).
Adding a theme = adding a CSS block + one registry entry in
`src/lib/themes.ts`.

## Structure

```
cmd/donezod/     backend entry point
internal/
  api/           HTTP layer (stdlib net/http, Go 1.22 routing)
  auth/          argon2id passwords, session tokens/authenticator, rate limiter, sweeper
  config/        flags/env configuration
  seed/          seed.json import
  store/         core store (users/sessions/spaces) + space store (per-space SQLite)
seed/            committed seed.json (regenerate: make seed-json)
web/
  src/
    domain/      types + mock dataset (the only place data lives)
    state/       AppStore (reducer + actions), selectors, ThemeProvider
    lib/         time math (date-fns), ids, project colors, theme registry
    components/
      shell/     nav rail, top bar, inspector, quick capture, app shell
      ui/        Radix primitives styled to GTech Console (tooltip, popover, menu)
      common/    project marks, status badges, activity-type metadata
    views/       one directory per primary view (Timeline is Project Pulse)
  scripts/       peek.mjs (screenshots), export-seed.mjs (seed.json generator)
```

Views are hash-routed (`#/timeline`, `#/projects/loom`, …) so they are
bookmarkable and screenshot-addressable.
