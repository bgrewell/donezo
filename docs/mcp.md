# Connecting an LLM to donezo (MCP)

donezo speaks the [Model Context Protocol](https://modelcontextprotocol.io)
over a single HTTP endpoint, `/mcp`, so you can wire a language model —
Claude Code, Claude Desktop, a managed agent, or any other MCP client —
straight into your own work memory. The model reads and writes the same
tasks, notes, captures, and activity you see in the app.

Each connection authenticates with a **per-user API token** you mint from
the app (avatar menu → **Connect your AI…**). A token acts as you: anything
the model does with it is attributed to your account. Treat tokens like
passwords.

A token is **account-wide, not space-scoped** — it reaches every space you
own, the same way you do when signed into the app. There is no such thing
as "the active space" over MCP: every tool call names the space it acts on
explicitly. This is deliberate, not a gap — it's what lets `capture_to_inbox`
drop a thought into your Personal space while you're deep in a work
conversation with the model. If you want a boundary between spaces, use
scope (below) or simply don't hand a token to a model you don't want
touching a given space's content at all.

## What the MCP server exposes

The server presents 27 tools built around how donezo is used day to day —
oriented on donezo's own model of work: an **activity** is a past fact (it
lands on the timeline), a **task** is a future possibility with a lifecycle,
and the **inbox** exists so capturing something never requires deciding what
it is first. The exact names, arguments, and JSON Schemas are published by
the server itself — every MCP client lists them on connect — so this table
is a summary, not the source of truth:

| Tool                  | Scope | What it does                                                                                  |
| --------------------- | ----- | ----------------------------------------------------------------------------------------------- |
| `list_spaces`         | read  | List the spaces you own. Call first to discover what's available.                              |
| `get_space_overview`  | read  | The orient call: every project with its status, current focus, and next action, plus open task and pending inbox counts. Use before acting in a space. |
| `get_project`         | read  | Full detail for one project — purpose, outcome, resume context, open tasks, recent activity.    |
| `search`              | read  | Full-text search across a space's projects, activities, tasks, notes, reminders, and inbox.     |
| `get_timeline`        | read  | Activities in a date range, chronological — for reflection: what actually happened.             |
| `list_inbox`          | read  | A space's pending raw captures — things captured but not yet classified.                        |
| `list_tasks`          | read  | Tasks in a space, optionally narrowed to one project or status. Defaults to open.               |
| `list_notes`          | read  | Notes in a space, optionally narrowed to one project.                                           |
| `list_reminders`      | read  | Reminders in a space, soonest first. Pending only unless you ask for done ones too.             |
| `capture_to_inbox`    | write | Zero-decision capture — the default verb when something should be remembered but you're not sure yet what it is. Works into **any** space you own, not just the active one. |
| `log_activity`        | write | Record a **past** fact on a project — appears on the timeline. Never for future work.            |
| `create_task`         | write | Create a task — a **future** possibility with a lifecycle (open → done). `details` holds anything too long for the title. |
| `complete_task`       | write | Mark a task done. By default also logs today's activity from the task title, mirroring the app's check-off flow. |
| `create_note`         | write | Create a durable reference note.                                                                |
| `create_reminder`     | write | Create a reminder that resurfaces at a specific time. `details` holds the longer version. Set `repeat_every` + `repeat_unit` (hour/day/week) to make it recur until marked done. |
| `create_project`      | write | Create a project — a stream of work with a purpose and an outcome.                              |
| `classify_inbox_item` | write | Atomically convert a pending inbox capture into a task, note, reminder, activity, or project.    |
| `convert_note`        | write | Turn an existing note into a task, reminder, or activity. The note is **removed** — unlike an inbox capture, which stays behind marked converted. Its body becomes the new item's details. |
| `dismiss_inbox_item`  | write | Close out a reviewed capture that needs no follow-up. The other half of triage.                 |
| `update_project`      | write | Update a project — its designations (next action, alternates, current focus, resume context, status) and descriptive fields (name, purpose, outcome, color, tags). Status is always a deliberate choice; the model never flips it automatically. |
| `update_task`         | write | Change a task's title, details, status, due date, project, or what it's waiting on.              |
| `update_note`         | write | Change a note's title, body, or project.                                                        |
| `update_activity`     | write | Correct a past fact already on the timeline — title, details, type, date, effort, project.       |
| `update_reminder`     | write | Reschedule a reminder, change its text, details or project, or mark it handled.                  |
| `delete_item`         | write | Move a task, note, reminder, activity, or inbox item to the **trash** — reversible, not destroyed. **Not** projects — see below. |
| `list_trash`          | read  | What is currently in the trash: what it was, when it went, and how many rows share its delete.  |
| `restore_item`        | write | Put a trashed item back, along with everything deleted alongside it. Projects included.          |

The 10 `read` tools work with either scope; the 17 `write` tools require a
`read_write` token. Which of these a token may call is governed by its
**scope** (below).

The write surface is deliberately close to parity with what you can do in
the app: a connected model that creates something can also correct it
afterwards, so a misclassification or a typo doesn't become permanent just
because it arrived over MCP.

### Short form and long form

Every item has a short field that should stay scannable — a task's `title`, a
reminder's `text` — and an optional `details` for everything else. Put one line
in the short one and the context in `details`; a paragraph crammed into a title
makes the list it appears in unreadable, which is what donezo's own backlog
looked like before this existed.

`classify_inbox_item` splits a multi-line capture for you: the first line
becomes the short field and the rest becomes `details`. `convert_note` puts the
note's body in `details`, so converting never loses anything.

### Which day "today" is

A tool that dates something without being told a date — `log_activity`,
`create_task`, `create_note`, `complete_task`, `classify_inbox_item` — uses
**your** calendar day, not the server's. The zone comes from your account,
which the web app fills in from the browser on load; an instance-wide
`--timezone` covers an account that has only ever connected over MCP.

This matters more than it sounds. Until [#39](https://github.com/bgrewell/donezo/issues/39)
the server used UTC, so anything an agent wrote after 17:00 Pacific was
filed on tomorrow, and the same action taken in the browser and over MCP
landed on different days. If you have never opened the web app in the
browser you actually work in, open it once — that is what teaches donezo
where you are.

A date you pass explicitly is always used as given.

### What this doesn't do

MCP is a tool surface for **doing work**, not for administering the
account. It cannot create, rename, archive, or delete a space; delete a
project (only the web app's typed-name confirmation can); or manage
tokens, invites, or other users. If you ask a connected model for one of
these, a well-behaved model will say so plainly and point you at the app
rather than improvise a workaround — that instruction is built into what
the server tells the model at connection time.

Project deletion is held back because it cascades to every activity,
task, note, and reminder the project owns — enough of a decision that it
should not be one tool call away, even now that it is reversible.

**Deleting is no longer destructive.** Since [#16](https://github.com/bgrewell/donezo/issues/16),
`delete_item` moves a row to the trash: it leaves every view at once, but
`list_trash` shows it and `restore_item` puts it back, until the
instance's retention window (30 days by default) expires. A model is
still told to confirm before deleting, and to prefer `complete_task` for
finished work or `dismiss_inbox_item` for a reviewed capture — but a
mistake is now recoverable rather than final.

`restore_item` accepts projects even though `delete_item` does not.
Undoing a destructive action is not itself destructive, and refusing it
would leave a connected model able to *see* a trashed project it cannot
help you with.

## Token scopes

When you generate a token you choose one scope:

- **Read & write** (`read_write`) — the model can both read your account and
  create or change tasks, notes, reminders, captures, and activity. Use this
  when you want the model to actually manage work on your behalf.
- **Read only** (`read_only`) — the model can orient and read, but every
  write is refused server-side. Good for exploratory or advisory use where
  you don't want the model changing anything.

Scope is fixed at creation. To change it, revoke the token and generate a
new one.

## Setup

In every client below, replace `<your-donezo-url>` with the address you use
to reach donezo (for example `https://todo.example.com`) and `<token>` with
the token shown once when you generate it. The **Connect your AI…** dialog
pre-fills both into a ready-to-paste Claude Code command for you.

### Claude Code

One command registers donezo as an HTTP MCP server with your token on the
`Authorization` header:

```sh
claude mcp add -s user --transport http donezo <your-donezo-url>/mcp \
  --header "Authorization: Bearer <token>"
```

After that, `claude mcp list` shows `donezo`, and the donezo tools are
available in your Claude Code sessions.

`-s user` registers it for your whole user account rather than one project
directory (`--scope` defaults to `local`, which is per-project). That matches
what a donezo token actually is: it belongs to you and reaches every space you
own, not to whichever repository you happened to run the command in. Drop the
flag if you deliberately want donezo available in one project only.

### Claude Desktop

Add donezo as a **custom connector**:

1. Open **Settings → Connectors → Add custom connector**.
2. Set the remote MCP server URL to `<your-donezo-url>/mcp`.
3. Supply the authorization header `Authorization: Bearer <token>`.

Restart Claude Desktop if the tools don't appear immediately.

### Managed Agents (Claude Managed Agents)

Managed Agents keep credentials in a **vault** rather than on the agent
definition, so the token never lives in the agent config:

1. Declare the MCP server on the agent (no auth here):

   ```json
   {
     "mcp_servers": [
       { "type": "url", "name": "donezo", "url": "<your-donezo-url>/mcp" }
     ],
     "tools": [{ "type": "mcp_toolset", "mcp_server_name": "donezo" }]
   }
   ```

2. Store the donezo token as a **`static_bearer`** vault credential keyed by
   the same server URL (`<your-donezo-url>/mcp`). Anthropic matches the
   credential to the server by URL and injects it on every call.

3. Attach the vault to each session via `vault_ids`.

The agent then calls the donezo tools with your token supplied automatically
and never exposed inside the sandbox.

### Other MCP clients

Any MCP client that supports the **Streamable HTTP** transport with a custom
header works the same way: point it at `<your-donezo-url>/mcp` and send
`Authorization: Bearer <token>`.

## Security notes

- **Tokens act as you.** A token carries your identity and reaches only your
  spaces — the same content you see in the app. Anything the model does with
  it is your action, attributed to your account.
- **Treat tokens like passwords.** The full token is shown exactly once, at
  generation; donezo stores only a hash and can never show it again. If you
  didn't copy it, revoke it and generate another.
- **Revoke on device loss or exposure.** If a laptop, token, or client
  config is lost or shared by mistake, revoke that token from the
  **Connect your AI…** dialog. Revocation takes effect immediately; existing
  connections stop working on their next call. Name your tokens per device
  ("claude code on laptop") so you know which to revoke.
- **Prefer read-only for exploratory use.** If you only want the model to
  read and advise, generate a `read_only` token so a stray write can't change
  your data.
- **One token per client.** Give each device or client its own token so you
  can revoke one without disturbing the others, and so `last used` in the
  token list tells you what's still active.
- **Rate limits are generous, not absent.** Each token is capped at 120 tool
  calls per minute — far more than an interactive conversation needs, but
  enough to stop a runaway or looping agent from hammering your data. If a
  call is limited, the model gets back a plain-text result saying how long
  to wait; it should tell you rather than silently retrying.
