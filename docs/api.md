# HTTP API reference

Technical reference for donezod's HTTP surface — REST and the MCP endpoint.
For *using* an AI over MCP, see [`mcp.md`](mcp.md); this page is the wire
contract for anyone calling the API directly (a client, a script, or the
donezo frontend itself).

## REST

`{id}` is a space id; unknown and foreign spaces both read as `404`.

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
| `POST /api/tokens`                                 | Any user: `{name, scope}` (`read_only`/`read_write`) → `201 {id, token, tokenPrefix, scope, name, createdAt}` — the MCP bearer token, plaintext **only here** |
| `GET /api/tokens`                                  | Any user: own tokens with `tokenPrefix`, `scope`, `createdAt`, `lastUsedAt`, `revokedAt`; never the token or its hash |
| `DELETE /api/tokens/{id}`                          | Any user: revoke own token → `204` (idempotent); another user's id is `404` |
| `GET /api/settings`                                | Any user: own preferences → `200 {settings}`. Never having saved one returns `{}`, not `404` |
| `PATCH /api/settings`                              | Any user: `{theme?, font?, fontSize?}` → `200 {settings}` (the full stored set). Omitted fields are left alone; `""` clears one so it follows the default again. Acts on the authenticated user only — there is no user id in the path |
| `GET /api/spaces`                                  | `{spaces}` — the requester's spaces                                    |
| `POST /api/spaces`                                 | `{name, color}` → `201 {space}`; id = name slug + random suffix        |
| `PATCH /api/spaces/{id}`                           | Any of `{name, color, position}` → `{space}`                           |
| `POST /api/spaces/{id}/archive` / `/unarchive`     | Stamp / clear `archivedAt` → `{space}`                                 |
| `GET /api/spaces/{id}/state`                       | Full space content (projects, activities, tasks, notes, reminders, inbox) |
| `POST /api/spaces/{id}/projects`                   | Create a project → `201`                                               |
| `PATCH /api/spaces/{id}/projects/{pid}`            | Any subset of mutable fields (incl. `nextAction`, `altNextActions`, `resumeContext`, `status`, `waitingOn`) |
| `DELETE /api/spaces/{id}/projects/{pid}`           | Transactional cascade → `200 {deleted}` with per-table counts: owned activities/tasks/notes are deleted; inbox suggestions and reminders survive with the project reference nulled |
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

**Mutation contract:** entity creates accept client-generated ids (1–64
chars of `a-z0-9-`, the frontend `newId()` shape) and answer `409` on a
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

### First run

Start with `--seed seed/seed.json` (creates user `ben` with *no
password* — it cannot log in yet), then `GET /api/auth/status` reports
`needsSetup: true` and `POST /api/auth/setup` with username `ben` claims
the seeded account by setting its password. Setup with a fresh username
on an unseeded data dir works the same way; once any user has a
password, setup answers `409`. The once-only invariant is enforced
atomically at the SQL layer, so concurrent setup requests racing on a
fresh instance produce exactly one owner. The owner is the instance
**admin**; databases created before roles existed are migrated in
place, promoting the first credentialed user to admin.

### Roles & invites

Every user is `admin` or `member` (`/api/auth/me` reports which;
non-admins get `403 {"error":"admin required"}` on admin endpoints). The
admin mints invite codes — `dz-XXXXX-XXXXX`, Crockford base32, ~50 bits
— via `POST /api/invites`. The plaintext code appears only in that one
`201`: the database stores its SHA-256 plus a display prefix, so codes
can be listed and revoked but never re-read. A new user redeems a code
with `POST /api/auth/register`, which atomically claims the invite (a
single guarded `UPDATE`, so two racing registrations with one code
produce exactly one account), creates the member and their first space
`main`, and issues a session. Codes are matched case-insensitively —
`dz-abcde-fghjk` claims `dz-ABCDE-FGHJK` — so a code survives being
retyped, not just pasted. Unknown, used, expired, and revoked codes all
fail with the same `403 {"error":"invalid or expired invite code"}` —
registration is not an invite-state oracle — while a taken username is
its own `409` that does not consume the code. That `409` is a
deliberate, bounded disclosure: it is visible only to someone the admin
already handed a valid code (anonymous login keeps its uniform `401`),
and every registration attempt — including the `409` — spends the
shared login/setup rate-limit budget.

### Security posture

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

## MCP (`/mcp`)

donezo exposes a [Model Context Protocol](https://modelcontextprotocol.io)
server at `POST /mcp` so a user's LLM (Claude Code, Claude Desktop, a
managed agent, any MCP client) can read and manage that user's donezo data.
For what it's like to *use*, see [`mcp.md`](mcp.md); this section is the
wire-level reference.

It is a **stateless Streamable HTTP** server built on the official
[MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk); `internal/mcp`
is a thin wrapper that adds donezo's bearer auth, scope enforcement, rate
limiting, and the curated tool surface.

- **Transport.** Streamable HTTP over `POST /mcp`, stateless (one ephemeral
  session per request, no session ids); responses are a single
  `application/json` body. Requests must send `Content-Type: application/json`
  and an `Accept` listing both `application/json` and `text/event-stream` (as
  the transport requires). `GET /mcp` answers `405`. The protocol version is
  negotiated by the SDK — latest `2026-07-28`, also supporting `2025-11-25`,
  `2025-06-18`, `2025-03-26`, and `2024-11-05`; `initialize` echoes the
  client's requested version when supported. Request bodies are capped at
  1 MiB (`413` past it). The server advertises the `tools` capability (plus
  the SDK's default `logging`) and exposes `tools/list`, `tools/call`,
  `initialize`/`server/discover`, `ping`, and `notifications/initialized`.
- **Auth.** `Authorization: Bearer dzmcp-…` only — validated against the
  `api_tokens` table (SHA-256 of the token; revoked tokens are rejected).
  Session cookies are **not** accepted, so `/mcp` has no CSRF surface.
  Missing, malformed, unknown, and revoked tokens all answer `401` with
  `WWW-Authenticate: Bearer`. The `Bearer` scheme match is case-insensitive
  per RFC 7235. `tools/call` is rate-limited to 120 calls per minute per
  token — checked before the tool name is even looked up, so a probing
  call against an unknown tool still costs budget — and each authenticated
  call refreshes the token's `lastUsedAt` (throttled to once per minute).
  `DisableLocalhostProtection` is conditioned on `--trust-proxy`: off (SDK
  DNS-rebinding guard active) for a direct-exposure instance with no
  reverse proxy, on for a proxied one, where the guard would otherwise
  reject every legitimate request.

**Token flow.** Mint a token from the session API (`POST /api/tokens`, any
authenticated user — not admin-gated); the plaintext is shown once and
stored only as a hash. Tokens are `dzmcp-` followed by 26 Crockford base32
symbols (~130 bits); the first 12 characters are kept as a display prefix.
The frontend "Connect your AI…" dialog wraps mint/list/revoke. A token is
account-wide, not scoped to one space — see [`mcp.md`](mcp.md) for what
that means in practice.

**Scopes.** A token is `read_only` or `read_write`, fixed at creation.
`tools/list` returns **only the tools the scope permits** — a `read_only`
token sees the nine read tools; a `read_write` token sees all twenty-four. A
write call made with a `read_only` token is refused with a clear `isError`
tool result (never a silent no-op).

**Tool surface.** Curated and workflow-shaped; every description tells the
model *when* to reach for it. Ids are server-generated (callers never mint
them), lists are capped at 50 with a truncation note, and each tool resolves
its `space_id` to a space the caller owns (foreign/unknown spaces read as
"space not found"; writes also require the space to be live).

| Tool | Scope | Purpose |
| ---- | ----- | ------- |
| `list_spaces` | read | Discover your spaces (call first). |
| `get_space_overview` | read | The orient call: projects with status/focus/next-action, plus open-task and pending-inbox counts. |
| `get_project` | read | Full project incl. `resumeContext`, open tasks, last 10 activities. |
| `search` | read | Case-insensitive substring across projects/activities/tasks/notes/reminders/inbox (same matching as the web UI). |
| `get_timeline` | read | Activities in a date range, chronological — for reflection. |
| `list_inbox` | read | Pending raw captures. |
| `list_tasks` | read | Tasks, optionally filtered by `project_id` and `status` (defaults to `open`). |
| `list_notes` | read | Notes, optionally filtered by `project_id`. |
| `list_reminders` | read | Reminders sorted by `remindAt`; pending unless `include_done`. |
| `capture_to_inbox` | write | Zero-decision capture into any owned space — the default when classification is uncertain. |
| `log_activity` | write | Record a PAST fact on a project (timeline); never for future work. |
| `create_task` | write | A FUTURE possibility with a lifecycle. |
| `complete_task` | write | Mark done; with `log_activity` (default true) also logs today's activity from the task title. |
| `create_note` | write | Durable reference text. |
| `create_reminder` | write | A time-bound nudge (`remind_at` ISO datetime). |
| `create_project` | write | A stream of work; only `name` required, `color` defaults to blue and `status` to active. |
| `classify_inbox_item` | write | Atomically convert a pending capture into a task/note/reminder/activity/project. |
| `dismiss_inbox_item` | write | Mark a pending capture `dismissed` (kept, not deleted); errors if it is already triaged. |
| `update_project` | write | Designations (`nextAction`, `altNextActions`, `currentFocus`, `resumeContext`, `status`, `waitingOn`) and descriptive fields (`name`, `purpose`, `outcome`, `color`, `tags`). |
| `update_task` | write | `title`, `status`, `due`, `project_id`, `waiting_on`; empty string clears an optional field. |
| `update_note` | write | `title`, `body`, `project_id`; empty `project_id` detaches. |
| `update_activity` | write | `title`, `details`, `type`, `date`, `effort_hours` (0 clears), `project_id`. |
| `update_reminder` | write | `text`, `remind_at`, `done`, `project_id`. |
| `delete_item` | write | Permanent delete by `kind` (`task`/`note`/`reminder`/`activity`/`inbox_item`) + `item_id`. Projects are refused with an explanation — their delete cascades and stays a web-app action. |

The exact names, arguments, and JSON Schemas are published by the server
itself over `tools/list` — every MCP client reads them on connect — so
this table is a summary, not the source of truth. The server also sends a
scope-aware `instructions` string at `initialize`, which is what a
connected model actually reads first; see [`mcp.md`](mcp.md) for what it
says.

For client setup (Claude Code, Claude Desktop, Managed Agents) see
[`mcp.md`](mcp.md#setup).
