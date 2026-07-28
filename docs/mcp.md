# Connecting an LLM to donezo (MCP)

donezo speaks the [Model Context Protocol](https://modelcontextprotocol.io)
over a single HTTP endpoint, `/mcp`, so you can wire a language model —
Claude Code, Claude Desktop, a managed agent, or any other MCP client —
straight into your own work memory. The model reads and writes the same
tasks, notes, captures, and activity you see in the app.

Each connection authenticates with a **per-user API token** you mint from
the app (avatar menu → **Connect your AI…**). A token acts as you: anything
the model does with it is attributed to your account and scoped to your
spaces. Treat tokens like passwords.

## What the MCP server exposes

The server presents 14 tools built around how donezo is used day to day —
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
| `capture_to_inbox`    | write | Zero-decision capture — the default verb when something should be remembered but you're not sure yet what it is. Works into **any** space you own, not just the active one. |
| `log_activity`        | write | Record a **past** fact on a project — appears on the timeline. Never for future work.            |
| `create_task`         | write | Create a task — a **future** possibility with a lifecycle (open → done).                        |
| `complete_task`       | write | Mark a task done. By default also logs today's activity from the task title, mirroring the app's check-off flow. |
| `create_note`         | write | Create a durable reference note.                                                                |
| `create_reminder`     | write | Create a reminder that resurfaces at a specific time.                                           |
| `classify_inbox_item` | write | Atomically convert a pending inbox capture into a task, note, reminder, activity, or project.    |
| `update_project`      | write | Manage a project's designations — next action, alternates, current focus, resume context, status. Status is always a deliberate choice; the model never flips it automatically. |

The 6 `read` tools work with either scope; the 8 `write` tools require a
`read_write` token. Which of these a token may call is governed by its
**scope** (below).

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
claude mcp add --transport http donezo <your-donezo-url>/mcp \
  --header "Authorization: Bearer <token>"
```

After that, `claude mcp list` shows `donezo`, and the donezo tools are
available in your Claude Code sessions.

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
