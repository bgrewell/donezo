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
| `GET /api/llm`                                     | Any user: `{enabled, provider?, model?, prompts[]}` — whether this instance has a model configured. Prompts are listed either way |
| `POST /api/llm/rewrite`                            | Any user: `{promptId, text}` → `200 {text}`. `400` if the text exceeds 4000 characters (refused, never truncated), `503` when no model is configured, `502` when it cannot be reached or the reply was cut off, `504` on timeout, `429` past 20 calls / 5 min per user |
| `GET /api/instance`                                | Any user: `{version?}` — what this donezod is. `version` is **absent** when the operator runs `--hide-version`; the web UI shows it in the nav rail when present |
| `GET /api/settings`                                | Any user: own preferences → `200 {settings}`. Never having saved one returns `{}`, not `404` |
| `PATCH /api/settings`                              | Any user: `{theme?, font?, fontSize?, timezone?, welcomed?, tourDone?, dismissedHints?, resetOnboarding?}` → `200 {settings}` (the full stored set). Omitted fields are left alone; `""` clears an appearance one so it follows the default again. Onboarding fields merge one way — see below. Acts on the authenticated user only — there is no user id in the path |
| `GET /api/spaces`                                  | `{spaces}` — the requester's spaces                                    |
| `POST /api/spaces`                                 | `{name, color}` → `201 {space}`; id = name slug + random suffix        |
| `PATCH /api/spaces/{id}`                           | Any of `{name, color, position}` → `{space}`                           |
| `POST /api/spaces/{id}/archive` / `/unarchive`     | Stamp / clear `archivedAt` → `{space}`                                 |
| `GET /api/spaces/{id}/state`                       | Full space content (projects, activities, tasks, notes, reminders, inbox) |
| `GET /api/spaces/{id}/revision`                    | `{revision}` — a counter that moves whenever anything in the space changes. Answered from memory without touching the space database; this is the endpoint clients poll |
| `POST /api/spaces/{id}/projects`                   | Create a project → `201`                                               |
| `PATCH /api/spaces/{id}/projects/{pid}`            | Any subset of mutable fields (incl. `nextAction`, `altNextActions`, `resumeContext`, `status`, `waitingOn`) |
| `DELETE /api/spaces/{id}/projects/{pid}`           | Transactional cascade → `200 {deleted}` with per-table counts: owned activities/tasks/notes are deleted; inbox suggestions and reminders survive with the project reference nulled |
| `POST /api/spaces/{id}/activities`                 | Create an activity entry → `201`                                       |
| `PATCH /api/spaces/{id}/activities/{aid}`          | Partial update                                                         |
| `DELETE /api/spaces/{id}/activities/{aid}`         | Delete → `204`                                                         |
| `POST /api/spaces/{id}/tasks`                      | Create a task → `201`                                                  |
| `PATCH /api/spaces/{id}/tasks/{tid}`               | Partial update                                                         |
| `POST /api/spaces/{id}/notes`                      | Create a note → `201`                                                  |
| `PATCH /api/spaces/{id}/notes/{nid}`               | Partial update: `{title?, body?, projectId?, createdAt?}` → `200`. `projectId: null` detaches the note; an emptied `body` is allowed, matching the create route |
| `DELETE /api/spaces/{id}/notes/{nid}`              | Delete a note → `204`. A note owns nothing, so this is a plain delete rather than a cascade |
| `POST /api/spaces/{id}/notes/{nid}/convert`        | `{kind, task?\|reminder?\|activity?}` → `200 {note, <kind>}`. Atomically removes the note and inserts the target; `note` is the note as it was. `kind` is restricted to task/reminder/activity — note-to-note is an edit, note-to-project is not a sensible target. `409` on a duplicate target id, and the note stays put |
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

### Staying current

The client only ever learned about changes it made itself, so a second tab, another machine, or an LLM writing over MCP were invisible until a manual reload. `GET /api/spaces/{id}/revision` fixes that: a counter per space, compared against the one the client holds, with a full `GET .../state` read only when it moves.

**How the counter is maintained.** Two boundaries, both of which every write already passes through: HTTP middleware bumps it after a mutating request against `/api/spaces/{id}/...` returns 2xx, and `internal/mcp`'s tool adapter bumps it after a write tool succeeds. Hanging it off the store's ~30 mutating methods instead would work right up until somebody adds the thirty-first and forgets.

It keys on the **response**, not the request: a rejected write changes nothing, and bumping for it would make every open tab refetch identical state.

**Why not something derived from the database.** The space databases run with `SetMaxOpenConns(1)` — SQLite serializes writers and gains nothing from a bigger pool — so a connection dedicated to reading a change marker would hold the only connection and starve every write. On a single shared connection `PRAGMA data_version` never reflects that connection's own commits either. And only `projects` and `activities` carry `updated_at`, so there is no timestamp to take a maximum over.

**It is in-memory and per-process.** A donezod restart returns every counter to zero, which reads as a change and costs each connected client one refetch. That is the harmless direction to fail in: a spurious refetch costs a request, a missed one leaves the screen quietly wrong.

The web client polls every 4s while the tab is visible, stops entirely when it is hidden, and skips a refresh while its own writes are still in flight — a server read taken mid-write can predate it and would visibly roll the user's own change back. Nothing streams, so there is no long-lived connection to keep alive through a reverse proxy. Server-sent events would cut latency to well under a second and remain the obvious next step; this is the version that earns that complexity first.

### Settings and onboarding progress

`user_settings` holds one JSON document per user, so adding a preference needs
no migration. Two kinds of field live there and they behave differently on
`PATCH`:

- **Appearance** (`theme`, `font`, `fontSize`) is last-write-wins. It is a
  preference, and the most recent deliberate choice should stand.
- **Onboarding progress** (`welcomed`, `tourDone`, `dismissedHints`) merges
  **one way**: flags only move `false → true`, and dismissed hints accumulate.
  Sending `welcomed: false` is not an error and does not clear it.

The asymmetry is the point. Progress is not a preference but a record that
something already happened, and it is written by every browser the user opens.
A browser with empty local state — a new machine, a private window — would
otherwise push that emptiness over a server that knows better and resurrect the
welcome dialog everywhere. The web client also declines to write before it has
read, but the rule belongs on the server too: settings are reachable by
anything holding a session, and a monotonic field should not be walkable
backwards by a caller that simply does not know any better.

`resetOnboarding: true` is the one way progress moves back, clearing all three.
It is a separate explicit intent rather than "set the flags to false" precisely
so that a reset can never be something a stale client does by accident. It is
applied last, so a patch that combines it with progress flags still ends up
reset rather than depending on field order. It does not touch appearance.

`dismissedHints` is bounded — at most 128 stored ids of 64 characters each —
since the ids come from the client and an unbounded list is a way to inflate
one user's document.

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

## Language model (optional)

donezo can be pointed at a language model to power small conveniences — today, tidying up a quick capture. **It is entirely optional**: with nothing configured the endpoints report themselves unavailable, the web UI omits the affordance, and everything else behaves identically. Model features are a flourish, never a step in a flow.

Configuration is **instance-wide** and read from the environment at startup — every user of one donezo shares one model. Per-user models are deliberately out of scope for now: that would mean storing a recoverable API key per user, and donezo has no encrypted-secret storage (every other secret it holds is one-way hashed). Keeping the key in the environment means it needs none.

| Variable | Required | Meaning |
| -------- | -------- | ------- |
| `DONEZOD_LLM_PROVIDER` | to enable anything | `anthropic` or `openai-compatible`. Unset leaves model features off |
| `DONEZOD_LLM_BASE_URL` | for `openai-compatible` | The endpoint, e.g. `http://localhost:11434/v1`. Optional for `anthropic` (points at a gateway instead of the default API) |
| `DONEZOD_LLM_MODEL` | for `openai-compatible` | Model name. Defaults to `claude-opus-5` for `anthropic` |
| `DONEZOD_LLM_API_KEY` | for `anthropic` | **Environment only — there is no flag.** A key passed as an argument is visible in the host's process list to every user |

`--llm-provider`, `--llm-base-url`, and `--llm-model` exist as flags too; the API key does not, by design.

**Which day it is.** A calendar date — an activity's `date`, a task's `due`, a `createdAt` — means "the day it was where the person was". An instant (`createdAt`/`updatedAt` timestamps, `capturedAt`) does not, and stays UTC.

The web app resolves dates in the browser's zone, so it has always been right. Writes that arrive without a browser — an agent over MCP — have to be told, and until they were, the server used UTC: every entry logged after 17:00 Pacific landed on tomorrow, and the browser and MCP disagreed about the date for that whole window ([#39](https://github.com/bgrewell/donezo/issues/39)).

So the browser reports its zone (`Intl.DateTimeFormat().resolvedOptions().timeZone`) into `settings.timezone` on load — nobody has to find a setting, and it follows the user between machines and across a move. A name that this host cannot resolve is refused at `PATCH` time rather than stored and silently ignored; `""` clears it.

`--timezone` / `DONEZOD_TIMEZONE` is the fallback for a user who has only ever connected over MCP and so has never had a browser report one. It defaults to the **host's** zone, not UTC, which is right for the usual case of one person running donezod where they are. **Set it explicitly when the host runs somewhere else, as a container almost always does** — a UTC container puts an evening's work on tomorrow. An unusable name fails at startup. donezod embeds a copy of the IANA database, so a slim image without `tzdata` still resolves zones.

**Showing the running version.** The web UI puts the build in the nav rail, above the collapse control, so "is this the build I just deployed?" is answerable by looking rather than by checking the release page. `--hide-version` / `DONEZOD_HIDE_VERSION=1` switches it off by omitting the field from `GET /api/instance` entirely — a client then cannot tell a hidden version from a server too old to report one. It is worth switching off once an instance is public: an exact build number is of more use to somebody looking up which exploits apply than to the people using it.

**Providers.** `anthropic` calls the Claude API through the official Go SDK. `openai-compatible` speaks `POST /v1/chat/completions`, which is what local runtimes serve — Ollama, LM Studio, llama.cpp's server, vLLM — as well as most hosted gateways. That is the one to use to keep everything on your own machine:

```sh
DONEZOD_LLM_PROVIDER=openai-compatible \
DONEZOD_LLM_BASE_URL=http://localhost:11434/v1 \
DONEZOD_LLM_MODEL=llama3 \
  donezod
```

A misconfiguration is refused at startup rather than on every request — naming a provider without what it needs, or naming a model with no provider at all, fails fast with a message saying which variable is missing.

**Limits.** One round trip is bounded at 30s (under donezod's 60s write timeout, so a slow model fails rather than appearing to hang), and each user may make 20 model calls per 5 minutes. The throttle is applied before the model is called, so a client stuck in a loop costs nothing upstream.

Text longer than 4000 characters is **refused with a 400, never truncated**, and a reply the model cut off at its own output ceiling is reported as a failure rather than returned. Both follow from the same rule: the caller replaces the user's own words with the reply, so a partial result would destroy the rest of what it was asked to tidy.

### Tuning the prompts

How much a prompt should change someone's words is a matter of taste, and taste is not worth a rebuild — nor is it the same taste for everyone on an instance. So a prompt is tunable at two levels, and one part of it is not tunable at all.

#### Body and core

Every prompt is a **body** plus a **core**, joined body-first so the core has the final word.

The **body** says what the rewrite should do and how far it should go. That is the taste part, and it is what any override replaces.

The **core** holds the two guarantees that stop a rewrite being *harmful* rather than merely not to taste:

- the note's own text is content to tidy, **not a request addressed to the model**
- the reply is the rewritten text and **nothing else**

Neither is optional, and neither is an operator's or a user's to drop. The captured text is untrusted input and every caller writes the reply back over the person's own words — so a prompt missing the first makes capture an injection path, and one missing the second lets model commentary be saved as if the user had typed it. The core is appended to whatever the body ends up being, by every route.

#### Instance-wide, on disk

Under `prompts/` inside the data directory (`/var/lib/donezo/prompts` by default):

| File | Role |
| ---- | ---- |
| `<id>.default.txt` | The **body** donezo ships. **Rewritten on every start** and never read back, so it stays visible next to your override and keeps up with upgrades — edits here are lost |
| `<id>.core.txt` | The **core**, for reference. Also rewritten every start and never read back: it is there so what gets appended is visible rather than a surprise in a request log |
| `<id>.txt` | Optional. Your replacement for that prompt's **body**. Absent by default |

Create `<id>.txt` (copy the `.default.txt` next to it and edit) and restart donezod; the log names any prompt it is running from disk:

```
donezod: prompt overrides in effect from /var/lib/donezo/prompts: polish-capture
```

A file holding only whitespace is treated as absent rather than sent as an empty instruction, one over 64 KiB is refused, and a file named for a prompt that does not exist is ignored. None of these stop the server: a prompt directory that cannot be read is logged and the built-in wording is used, because an unwritable disk is not a reason to stop serving.

#### Per user, in settings

Each user can also keep their own body, stored in `user_settings` under `prompts` and edited from **Tune the polish prompt…** in the account menu. It takes precedence over the instance's, so the resolution order is:

```
the user's own body  →  <id>.txt on disk  →  the shipped body        (+ core, always)
```

`PATCH /api/settings` with `{"prompts": {"<id>": "..."}}` saves one; an empty value clears it, which is how someone returns to the instance's wording rather than pinning themselves to today's default. An unknown prompt id is refused rather than stored — a typo would otherwise be saved and silently never used, which looks exactly like the feature not working. One body is capped at 4000 characters, since it is sent on every call.

`GET /api/llm` returns, per prompt, the `body` in effect for that user, the `default` it falls back to, the read-only `core`, and whether it is `customized` — enough for a settings UI to render an editor, offer a reset, and show the fixed part instead of hiding it.

The prompt ids are the ones `GET /api/llm` lists. Today that is one:

| Id | What it does |
| -- | ------------ |
| `polish-capture` | Fixes spelling, grammar, word order and flow in a hastily typed capture. It is meant to rewrite clumsy sentences, not just move commas — while holding the meaning, the concrete details, and the writer's voice fixed. If a rewrite ever reads like someone else wrote it, that is the thing to tune |

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
token sees the nine read tools; a `read_write` token sees all twenty-five. A
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
| `convert_note` | write | Convert a note into a `task`/`reminder`/`activity`, deleting the note. Fields default from the note; its body reaches an activity's `details` and is otherwise lost. |
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
