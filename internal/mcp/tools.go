package mcp

import (
	"context"
	"encoding/json"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/bgrewell/donezo/internal/store"
)

// This file defines the curated tool surface and registers it with the SDK
// server. Every description is prescriptive about WHEN to use the tool and
// carries donezo's ontology, so the calling LLM reaches for the right verb.
// Input schemas are strict (additionalProperties:false, explicit required
// lists, enums mirroring web/src/domain/types.ts) and are handed to the SDK
// verbatim as raw JSON Schema (Tool.InputSchema accepts any value that
// marshals to a JSON schema object). Scope, rate limiting, and tools/list
// filtering live in the gate middleware (see mcp.go), not here.

// Enum unions mirrored from web/src/domain/types.ts (and internal/api's
// validate.go). The MCP layer validates against them before writing,
// because the store has no enum CHECK constraints for these columns.
var (
	activityTypes   = []string{"work", "research", "meeting", "decision", "blocker", "milestone"}
	projectStatuses = []string{"active", "waiting", "blocked", "paused", "completed", "cancelled"}
	projectColors   = []string{"blue", "green", "tan", "violet", "rose", "orange", "steel"}
	itemKinds       = []string{"task", "note", "reminder", "activity", "project"}
	taskStatuses    = []string{"open", "waiting", "someday", "done"}
	// deletableKinds is deliberately narrower than itemKinds: projects are
	// not deletable over MCP because deleting one cascades to every activity,
	// task, note and reminder it owns. That stays a web-app action.
	deletableKinds = []string{"task", "note", "reminder", "activity", "inbox_item"}
	// noteTargetKinds are what a note may be converted into, mirroring the
	// HTTP route. Note-to-note is an edit dressed up as a conversion
	// (update_note does that), and note-to-project is not a sensible target:
	// a note is content, not a stream of work.
	noteTargetKinds = []string{"task", "reminder", "activity"}
	// restorableKinds is wider than deletableKinds by one: a project can be
	// restored here even though it cannot be deleted here. Undoing a
	// destructive web-app action is not itself destructive, and refusing it
	// would leave an agent able to see a trashed project it cannot help with.
	restorableKinds = []string{"task", "note", "reminder", "activity", "inbox_item", "project"}
)

// maxItems caps how many rows a read tool returns; past it the result
// carries a truncation note so the LLM knows to narrow its request.
const maxItems = 50

// toolHandler executes one tool. It returns the text payload and whether
// that payload represents a tool-execution error (isError). Argument
// decoding and validation live inside the handler; protocol-level failures
// (unknown tool, malformed params) are handled by the dispatcher before a
// handler runs.
type toolHandler func(ctx context.Context, h *Handler, c caller, args json.RawMessage) (text string, isError bool)

// tool is one entry in the curated surface.
type tool struct {
	name        string
	title       string
	description string
	inputSchema map[string]any
	// write marks a mutating tool: it is listed and callable only for
	// read_write tokens.
	write   bool
	handler toolHandler
}

// tools is the ordered tool surface. Read tools come first, then writes.
var tools = buildTools()

// toolByName indexes tools for tools/call lookup.
var toolByName = func() map[string]tool {
	m := make(map[string]tool, len(tools))
	for _, t := range tools {
		m[t.name] = t
	}
	return m
}()

// registerTools adds every tool in the curated surface to the SDK server
// using raw JSON Schema, wrapping each handler so it reads the caller from
// the request context and reports its result in the SDK's shape. Every tool
// is registered on the single server; scope filtering of tools/list and the
// write-tool scope gate happen in the gate middleware.
func registerTools(server *mcpsdk.Server, h *Handler) {
	for _, t := range tools {
		server.AddTool(&mcpsdk.Tool{
			Name:        t.name,
			Title:       t.title,
			Description: t.description,
			InputSchema: t.inputSchema,
		}, h.adaptTool(t))
	}
}

// adaptTool wraps a donezo toolHandler as an SDK mcp.ToolHandler. It resolves
// the caller stored in the request context by withAuth, passes the raw
// arguments straight through (each handler decodes and validates its own),
// and packages the (text, isError) result as a single text content block.
// Handler-level validation failures surface as isError results — never
// protocol errors — so the LLM can read the reason and self-correct.
func (h *Handler) adaptTool(t tool) mcpsdk.ToolHandler {
	return func(ctx context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		c, ok := callerFrom(ctx)
		if !ok {
			// withAuth guarantees a caller on every dispatched request; this
			// is a defensive guard, not a reachable path.
			return toolTextResult("authentication required", true), nil
		}
		args := req.Params.Arguments
		if len(args) == 0 {
			args = json.RawMessage("{}")
		}
		text, isErr := t.handler(ctx, h, c, args)
		// One choke point for "an LLM changed something": every tool is
		// registered through here, so a new write tool is covered without
		// anyone remembering to wire it up. Only successful writes count —
		// a refused call changed nothing.
		if t.write && !isErr && h.onWrite != nil {
			if spaceID := spaceIDFromArgs(args); spaceID != "" {
				h.onWrite(spaceID)
			}
		}
		return toolTextResult(text, isErr), nil
	}
}

// spaceIDFromArgs pulls the space id out of a tool's raw arguments. Every
// tool in the surface takes one — there is no implicit "active space" over
// MCP — so this needs no per-tool knowledge. An unparseable or absent id
// reports "", and the caller simply does not record a change.
func spaceIDFromArgs(args json.RawMessage) string {
	var probe struct {
		SpaceID string `json:"space_id"`
	}
	if err := json.Unmarshal(args, &probe); err != nil {
		return ""
	}
	return probe.SpaceID
}

// toolTextResult builds a tools/call result: one text content block plus the
// isError flag.
func toolTextResult(text string, isError bool) *mcpsdk.CallToolResult {
	return &mcpsdk.CallToolResult{
		Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: text}},
		IsError: isError,
	}
}

// ─── Schema builders ──────────────────────────────────────────────────────

// objectSchema builds a strict object JSON Schema.
func objectSchema(props map[string]any, required ...string) map[string]any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{
		"type":                 "object",
		"properties":           props,
		"required":             required,
		"additionalProperties": false,
	}
}

func strProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

func numProp(desc string) map[string]any {
	return map[string]any{"type": "number", "description": desc}
}

func boolProp(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}

func arrStrProp(desc string) map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": desc}
}

func enumProp(desc string, values []string) map[string]any {
	vs := make([]any, len(values))
	for i, v := range values {
		vs[i] = v
	}
	return map[string]any{"type": "string", "description": desc, "enum": vs}
}

// ─── Tool surface ─────────────────────────────────────────────────────────

// buildTools assembles the curated surface. It is a function (not a
// literal) so schema builders can compose the property maps.
func buildTools() []tool {
	return []tool{
		// READ ──────────────────────────────────────────────────────────
		{
			name:  "list_spaces",
			title: "List spaces",
			description: "List the spaces (workspaces) you own, with id, name, color, and archived flag. " +
				"Call this FIRST in any session: every other tool needs a space_id, and space ids are not guessable.",
			inputSchema: objectSchema(map[string]any{}),
			handler:     toolListSpaces,
		},
		{
			name:  "get_space_overview",
			title: "Get space overview",
			description: "The orient call: use it before acting in a space. Returns every project with its " +
				"status, currentFocus, nextAction, and altNextActions, plus the space's open-task count and " +
				"pending-inbox count. Read this to understand what is in motion before creating or logging anything.",
			inputSchema: objectSchema(map[string]any{
				"space_id": strProp("The space to summarize (from list_spaces)."),
			}, "space_id"),
			handler: toolGetSpaceOverview,
		},
		{
			name:  "get_project",
			title: "Get project detail",
			description: "Full detail for one project: purpose, outcome, currentFocus, nextAction, " +
				"altNextActions, status, waitingOn, and resumeContext, plus its open tasks and its 10 most " +
				"recent activities. Use it to resume work on a specific project.",
			inputSchema: objectSchema(map[string]any{
				"space_id":   strProp("The space the project lives in."),
				"project_id": strProp("The project to fetch (from get_space_overview)."),
			}, "space_id", "project_id"),
			handler: toolGetProject,
		},
		{
			name:  "search",
			title: "Search a space",
			description: "Full-text search across a space's projects, activities, tasks, notes, reminders, and " +
				"inbox (case-insensitive substring, the same matching the donezo web UI uses). Use it to find " +
				"something when you do not know which project or entity holds it.",
			inputSchema: objectSchema(map[string]any{
				"space_id": strProp("The space to search."),
				"query":    strProp("The text to look for."),
			}, "space_id", "query"),
			handler: toolSearch,
		},
		{
			name:  "get_timeline",
			title: "Get timeline",
			description: "Activities in a date range, chronological — for reflection: what actually happened. " +
				"Activities are PAST facts; this is how you review a period. Dates are yyyy-MM-dd.",
			inputSchema: objectSchema(map[string]any{
				"space_id":  strProp("The space to read."),
				"from_date": strProp("Start of the range, inclusive, yyyy-MM-dd."),
				"to_date":   strProp("End of the range, inclusive, yyyy-MM-dd."),
			}, "space_id", "from_date", "to_date"),
			handler: toolGetTimeline,
		},
		{
			name:  "list_inbox",
			title: "List inbox",
			description: "The space's pending raw captures — things captured but not yet classified. Use it to " +
				"triage: review pending items, then classify_inbox_item to turn each into a task, note, " +
				"reminder, activity, or project.",
			inputSchema: objectSchema(map[string]any{
				"space_id": strProp("The space whose inbox to list."),
			}, "space_id"),
			handler: toolListInbox,
		},
		{
			name:  "list_tasks",
			title: "List tasks",
			description: "Tasks in a space — FUTURE work with a lifecycle. Optionally narrow to one project or " +
				"one status. Defaults to open tasks across the whole space; pass status to see done/waiting/someday. " +
				"Use it to find a task's id before updating or completing it.",
			inputSchema: objectSchema(map[string]any{
				"space_id":   strProp("The space to read."),
				"project_id": strProp("Optional project to narrow to."),
				"status":     enumProp("Optional status filter (defaults to open).", taskStatuses),
			}, "space_id"),
			handler: toolListTasks,
		},
		{
			name:  "list_notes",
			title: "List notes",
			description: "Notes in a space — durable reference text, not actions. Optionally narrow to one " +
				"project. Use it to find a note's id before updating or deleting it.",
			inputSchema: objectSchema(map[string]any{
				"space_id":   strProp("The space to read."),
				"project_id": strProp("Optional project to narrow to."),
			}, "space_id"),
			handler: toolListNotes,
		},
		{
			name:  "list_reminders",
			title: "List reminders",
			description: "Reminders in a space, soonest first. Pending only by default; pass include_done to see " +
				"ones already marked done. Use it to find a reminder's id before updating or deleting it.",
			inputSchema: objectSchema(map[string]any{
				"space_id":     strProp("The space to read."),
				"include_done": boolProp("Include reminders already marked done (default false)."),
			}, "space_id"),
			handler: toolListReminders,
		},

		// WRITE ─────────────────────────────────────────────────────────
		{
			name:  "capture_to_inbox",
			title: "Capture to inbox",
			description: "Zero-decision capture: the DEFAULT verb when the user mentions something to remember " +
				"and you are not certain how to classify it. Drops raw text into a space's inbox to triage " +
				"later. Works into ANY space you own. Prefer this over guessing between task/note/activity.",
			write: true,
			inputSchema: objectSchema(map[string]any{
				"space_id":             strProp("The space to capture into."),
				"text":                 strProp("The raw text to remember."),
				"suggested_kind":       enumProp("Optional hint at what this should become (defaults to note).", itemKinds),
				"suggested_project_id": strProp("Optional hint at the related project id."),
			}, "space_id", "text"),
			handler: toolCaptureToInbox,
		},
		{
			name:  "log_activity",
			title: "Log activity",
			description: "Record a PAST fact that happened on a project — it appears on the timeline. Use this " +
				"for work already done, decisions made, meetings held. NEVER use it for future or intended work " +
				"(use create_task for that). Requires an existing project_id.",
			write: true,
			inputSchema: objectSchema(map[string]any{
				"space_id":     strProp("The space the project lives in."),
				"project_id":   strProp("The project this happened on (must already exist)."),
				"title":        strProp("Short description of what happened."),
				"details":      strProp("Optional longer detail."),
				"type":         enumProp("Optional activity type (defaults to work).", activityTypes),
				"date":         strProp("Optional yyyy-MM-dd date it happened (defaults to today)."),
				"effort_hours": numProp("Optional rough effort in hours."),
			}, "space_id", "project_id", "title"),
			handler: toolLogActivity,
		},
		{
			name:  "create_task",
			title: "Create task",
			description: "Create a task — a FUTURE possibility with a lifecycle (open → done). Use it for work " +
				"that might happen. For something that already happened, use log_activity instead. project_id is " +
				"optional (unlinked tasks are allowed). Keep the short field short — one scannable line — and put anything longer in details. A paragraph crammed into a title makes the list it appears in unreadable.",
			write: true,
			inputSchema: objectSchema(map[string]any{
				"space_id":   strProp("The space to create the task in."),
				"title":      strProp("What needs doing — one short line."),
				"details":    strProp("Optional longer detail: context, links, acceptance criteria."),
				"project_id": strProp("Optional project to attach the task to (must exist if given)."),
				"due":        strProp("Optional due date, yyyy-MM-dd."),
			}, "space_id", "title"),
			handler: toolCreateTask,
		},
		{
			name:  "complete_task",
			title: "Complete task",
			description: "Mark a task done. When log_activity is true (the default), it also records today's " +
				"activity from the task's title on the task's project — mirroring the web UI's check-off flow. " +
				"If the task has no project, only the completion happens (an activity needs a project).",
			write: true,
			inputSchema: objectSchema(map[string]any{
				"space_id":     strProp("The space the task lives in."),
				"task_id":      strProp("The task to complete."),
				"log_activity": boolProp("Also log an activity from the task title (default true)."),
			}, "space_id", "task_id"),
			handler: toolCompleteTask,
		},
		{
			name:  "create_note",
			title: "Create note",
			description: "Create a note — durable reference text. Use it for information worth keeping that is " +
				"not itself an action. title is optional (it defaults to the start of the body). project_id is optional.",
			write: true,
			inputSchema: objectSchema(map[string]any{
				"space_id":   strProp("The space to create the note in."),
				"body":       strProp("The note body."),
				"title":      strProp("Optional title (defaults to the start of the body)."),
				"project_id": strProp("Optional project to attach the note to (must exist if given)."),
			}, "space_id", "body"),
			handler: toolCreateNote,
		},
		{
			name:  "create_reminder",
			title: "Create reminder",
			description: "Create a reminder that resurfaces at a specific time. Use it for time-bound nudges. " +
				"remind_at is an ISO datetime like 2026-07-28T09:00:00. To make it recurring — re-reminding on a " +
				"schedule until it is marked done — set repeat_every and repeat_unit (e.g. 1 + day for daily).",
			write: true,
			inputSchema: objectSchema(map[string]any{
				"space_id":     strProp("The space to create the reminder in."),
				"text":         strProp("What to be reminded about — one short line."),
				"details":      strProp("Optional longer detail: why it matters, what to have to hand."),
				"remind_at":    strProp("When to resurface, ISO datetime (e.g. 2026-07-28T09:00:00)."),
				"project_id":   strProp("Optional project to attach the reminder to (must exist if given)."),
				"repeat_every": numProp("Optional recurrence interval count (>= 1). Set with repeat_unit to repeat until done."),
				"repeat_unit":  enumProp("Optional recurrence unit; required with repeat_every.", store.RepeatUnits),
			}, "space_id", "text", "remind_at"),
			handler: toolCreateReminder,
		},
		{
			name:  "classify_inbox_item",
			title: "Classify inbox item",
			description: "Atomically convert a pending inbox capture into a structured item — task, note, " +
				"reminder, activity, or project — mirroring the web UI's convert action. The inbox item is marked " +
				"converted and the new item is created together. Supply only the fields the chosen kind needs " +
				"(e.g. remind_at for reminder, project_id for activity); missing fields default from the raw text.",
			write: true,
			inputSchema: objectSchema(map[string]any{
				"space_id":   strProp("The space the inbox item lives in."),
				"inbox_id":   strProp("The pending inbox item to convert."),
				"kind":       enumProp("What to turn it into.", itemKinds),
				"title":      strProp("Optional title (task/note/activity)."),
				"body":       strProp("Optional body (note)."),
				"text":       strProp("Optional text (reminder)."),
				"remind_at":  strProp("Reminder time, ISO datetime (required when kind is reminder)."),
				"project_id": strProp("Project id (required when kind is activity; optional for task/note/reminder)."),
				"due":        strProp("Optional due date for a task, yyyy-MM-dd."),
				"name":       strProp("Optional project name (kind project)."),
				"color":      enumProp("Optional project color (kind project, defaults to blue).", projectColors),
				"type":       enumProp("Optional activity type (kind activity, defaults to work).", activityTypes),
			}, "space_id", "inbox_id", "kind"),
			handler: toolClassifyInboxItem,
		},
		{
			name:  "convert_note",
			title: "Convert note",
			description: "Turn an existing note into a task, reminder, or activity — for when something filed " +
				"as reference turns out to be work. Unlike classify_inbox_item, the source does NOT survive: the " +
				"note is removed and the new item created together, so use it only when the note has become the " +
				"other thing rather than merely prompting it. Everything defaults from the note — its title " +
				"becomes the task title, reminder text, or activity title, its body becomes the new item's " +
				"details, and its project carries over. Nothing is lost. A note cannot become another note (that " +
				"is update_note) or a project.",
			write: true,
			inputSchema: objectSchema(map[string]any{
				"space_id":   strProp("The space the note lives in."),
				"note_id":    strProp("The note to convert (from list_notes or search)."),
				"kind":       enumProp("What to turn the note into.", noteTargetKinds),
				"title":      strProp("Optional title (task/activity); defaults to the note's title."),
				"text":       strProp("Optional text (reminder); defaults to the note's title."),
				"remind_at":  strProp("Reminder time, ISO datetime (required when kind is reminder)."),
				"project_id": strProp("Project id; defaults to the note's project (an activity needs one either way)."),
				"due":        strProp("Optional due date for a task, yyyy-MM-dd."),
				"type":       enumProp("Optional activity type (kind activity, defaults to work).", activityTypes),
				"details":    strProp("Optional details; defaults to the note's body."),
			}, "space_id", "note_id", "kind"),
			handler: toolConvertNote,
		},
		{
			name:  "update_project",
			title: "Update project",
			description: "Update a project: its designations (nextAction, altNextActions, currentFocus, " +
				"resumeContext, status, waitingOn) and its descriptive fields (name, purpose, outcome, color, tags). " +
				"Use it to set the single next concrete action or to move status. Only the fields you pass change; " +
				"status is set manually here (never inferred).",
			write: true,
			inputSchema: objectSchema(map[string]any{
				"space_id":         strProp("The space the project lives in."),
				"project_id":       strProp("The project to update."),
				"next_action":      strProp("The single next concrete action."),
				"alt_next_actions": arrStrProp("Up to a couple of alternate next actions."),
				"current_focus":    strProp("The current thread being pulled."),
				"resume_context":   strProp("Context note for resuming after an interruption."),
				"status":           enumProp("Project status (set manually).", projectStatuses),
				"waiting_on":       strProp("Who/what is being waited on (empty string clears it)."),
				"name":             strProp("Project name."),
				"purpose":          strProp("Why this stream of work exists."),
				"outcome":          strProp("What done looks like."),
				"color":            enumProp("Project color.", projectColors),
				"tags":             arrStrProp("Tags on the project (replaces the existing set)."),
			}, "space_id", "project_id"),
			handler: toolUpdateProject,
		},
		{
			name:  "create_project",
			title: "Create project",
			description: "Create a project — a stream of work with a purpose and an outcome. Use it when the user " +
				"is starting something substantial enough to gather activities and tasks under it. For a one-off " +
				"action, create_task is enough. Only name is required; the rest can be filled in later.",
			write: true,
			inputSchema: objectSchema(map[string]any{
				"space_id":      strProp("The space to create the project in."),
				"name":          strProp("Project name."),
				"purpose":       strProp("Optional: why this stream of work exists."),
				"outcome":       strProp("Optional: what done looks like."),
				"color":         enumProp("Optional color (defaults to blue).", projectColors),
				"current_focus": strProp("Optional: the current thread being pulled."),
				"next_action":   strProp("Optional: the single next concrete action."),
				"tags":          arrStrProp("Optional tags."),
			}, "space_id", "name"),
			handler: toolCreateProject,
		},
		{
			name:  "update_task",
			title: "Update task",
			description: "Change an existing task: its title, details, status, due date, project, or what it is " +
				"waiting on. Use it to correct or re-scope a task, or to move an over-long title into details. To " +
				"simply mark one done, prefer complete_task — it also logs the matching activity. Only the fields " +
				"you pass change.",
			write: true,
			inputSchema: objectSchema(map[string]any{
				"space_id":   strProp("The space the task lives in."),
				"task_id":    strProp("The task to update (from list_tasks or get_project)."),
				"title":      strProp("What needs doing — one short line."),
				"details":    strProp("Longer detail (empty string clears it)."),
				"status":     enumProp("Task status.", taskStatuses),
				"due":        strProp("Due date, yyyy-MM-dd (empty string clears it)."),
				"project_id": strProp("Project to attach to (empty string detaches it)."),
				"waiting_on": strProp("Who/what this task is waiting on (empty string clears it)."),
			}, "space_id", "task_id"),
			handler: toolUpdateTask,
		},
		{
			name:  "update_note",
			title: "Update note",
			description: "Change an existing note's title, body, or project. Use it to correct or expand a note " +
				"rather than creating a second one. Only the fields you pass change.",
			write: true,
			inputSchema: objectSchema(map[string]any{
				"space_id":   strProp("The space the note lives in."),
				"note_id":    strProp("The note to update (from list_notes or search)."),
				"title":      strProp("Note title."),
				"body":       strProp("Note body."),
				"project_id": strProp("Project to attach to (empty string detaches it)."),
			}, "space_id", "note_id"),
			handler: toolUpdateNote,
		},
		{
			name:  "update_activity",
			title: "Update activity",
			description: "Correct a PAST fact already on the timeline: its title, details, type, date, effort, or " +
				"project. Use it to fix a mis-logged activity — not to record something new (use log_activity). " +
				"Only the fields you pass change.",
			write: true,
			inputSchema: objectSchema(map[string]any{
				"space_id":     strProp("The space the activity lives in."),
				"activity_id":  strProp("The activity to update (from get_timeline or get_project)."),
				"title":        strProp("Short description of what happened."),
				"details":      strProp("Longer detail."),
				"type":         enumProp("Activity type.", activityTypes),
				"date":         strProp("The yyyy-MM-dd date it happened."),
				"effort_hours": numProp("Rough effort in hours (0 clears it)."),
				"project_id":   strProp("The project it belongs to."),
			}, "space_id", "activity_id"),
			handler: toolUpdateActivity,
		},
		{
			name:  "update_reminder",
			title: "Update reminder",
			description: "Change an existing reminder's text, details, time, project, or done flag. Use it to " +
				"reschedule a reminder, mark one handled, or move an over-long text into details. Only the fields " +
				"you pass change.",
			write: true,
			inputSchema: objectSchema(map[string]any{
				"space_id":    strProp("The space the reminder lives in."),
				"reminder_id": strProp("The reminder to update (from list_reminders)."),
				"text":        strProp("What to be reminded about — one short line."),
				"details":     strProp("Longer detail (empty string clears it)."),
				"remind_at":   strProp("When to resurface, ISO datetime (e.g. 2026-07-28T09:00:00)."),
				"done":        boolProp("Whether the reminder has been handled."),
				"project_id":  strProp("Project to attach to (empty string detaches it)."),
			}, "space_id", "reminder_id"),
			handler: toolUpdateReminder,
		},
		{
			name:  "dismiss_inbox_item",
			title: "Dismiss inbox item",
			description: "Mark a pending inbox capture dismissed — it was reviewed and needs no follow-up. This is " +
				"the other half of triage: classify_inbox_item turns a capture into something, dismiss_inbox_item " +
				"closes it out without creating anything. The capture itself is kept.",
			write: true,
			inputSchema: objectSchema(map[string]any{
				"space_id": strProp("The space the inbox item lives in."),
				"inbox_id": strProp("The pending inbox item to dismiss."),
			}, "space_id", "inbox_id"),
			handler: toolDismissInboxItem,
		},
		{
			name:  "delete_item",
			title: "Delete item",
			description: "Move a task, note, reminder, activity, or inbox item to the trash. Use it for things " +
				"created by mistake — to close out real work use complete_task, and to clear a reviewed capture " +
				"use dismiss_inbox_item. It leaves every view immediately but is NOT destroyed: list_trash shows " +
				"it and restore_item puts it back, until the instance's retention window (30 days by default) " +
				"expires. Still worth confirming with the user, but a mistake here is recoverable. Deleting a " +
				"PROJECT is not available here — that cascades to everything the project owns and stays a web-app " +
				"action.",
			write: true,
			inputSchema: objectSchema(map[string]any{
				"space_id": strProp("The space the item lives in."),
				"kind":     enumProp("What kind of item to delete.", deletableKinds),
				"item_id":  strProp("The id of the item to delete."),
			}, "space_id", "kind", "item_id"),
			handler: toolDeleteItem,
		},
		{
			name:  "list_trash",
			title: "List trash",
			description: "Everything currently in the space's trash: what it was, when it was deleted, and how " +
				"many rows share its delete. Use it to find something that was removed by mistake — including by " +
				"you — before restoring it. Deleting a project trashes the content it owns in the same batch, so " +
				"a batchSize above 1 means restoring brings all of it back together.",
			inputSchema: objectSchema(map[string]any{
				"space_id": strProp("The space whose trash to list."),
			}, "space_id"),
			handler: toolListTrash,
		},
		{
			name:  "restore_item",
			title: "Restore item",
			description: "Put a trashed item back. It returns to every view exactly as it was. Restoring names " +
				"one item but brings back everything deleted alongside it — restoring a project restores the " +
				"activities, tasks and notes that went with it, and only those: something deleted separately " +
				"beforehand stays in the trash. Get ids from list_trash.",
			write: true,
			inputSchema: objectSchema(map[string]any{
				"space_id": strProp("The space the item was deleted in."),
				"kind":     enumProp("What kind of item to restore.", restorableKinds),
				"item_id":  strProp("The id of the trashed item (from list_trash)."),
			}, "space_id", "kind", "item_id"),
			handler: toolRestoreItem,
		},
	}
}

// oneOf reports whether v is in allowed.
func oneOf(v string, allowed []string) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}

// validDate reports whether v is a yyyy-MM-dd date.
func validDate(v string) bool {
	_, err := time.Parse("2006-01-02", v)
	return err == nil
}

// validDateTime reports whether v is an ISO datetime, with or without a
// zone (mirroring the web UI's local datetimes).
func validDateTime(v string) bool {
	for _, layout := range []string{"2006-01-02T15:04:05", time.RFC3339} {
		if _, err := time.Parse(layout, v); err == nil {
			return true
		}
	}
	return false
}
