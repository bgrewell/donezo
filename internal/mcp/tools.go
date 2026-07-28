package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/bgrewell/donezo/internal/store"
)

// This file defines the curated tool surface and the tools/list and
// tools/call dispatch. Every description is prescriptive about WHEN to use
// the tool and carries donezo's ontology, so the calling LLM reaches for
// the right verb. Input schemas are strict (additionalProperties:false,
// explicit required lists, enums mirroring web/src/domain/types.ts).

// Enum unions mirrored from web/src/domain/types.ts (and internal/api's
// validate.go). The MCP layer validates against them before writing,
// because the store has no enum CHECK constraints for these columns.
var (
	activityTypes   = []string{"work", "research", "meeting", "decision", "blocker", "milestone"}
	projectStatuses = []string{"active", "waiting", "blocked", "paused", "completed", "cancelled"}
	projectColors   = []string{"blue", "green", "tan", "violet", "rose", "orange", "steel"}
	itemKinds       = []string{"task", "note", "reminder", "activity", "project"}
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

// handleToolsList returns the tools the caller's scope permits: read_only
// tokens see only the read tools, read_write tokens see all. Pagination is
// not used — the surface is small enough to return whole.
func (h *Handler) handleToolsList(w http.ResponseWriter, req rpcRequest, c caller) {
	readWrite := c.scope == store.ScopeReadWrite
	list := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		if t.write && !readWrite {
			continue
		}
		list = append(list, map[string]any{
			"name":        t.name,
			"title":       t.title,
			"description": t.description,
			"inputSchema": t.inputSchema,
		})
	}
	h.writeRPCResult(w, req.ID, map[string]any{"tools": list})
}

// handleToolsCall validates and runs a tool. Unknown tools are a protocol
// error (-32602, matching the spec's example); rate-limit, scope, and
// handler-validation failures are returned as isError tool results so the
// LLM can read the reason and adjust.
func (h *Handler) handleToolsCall(w http.ResponseWriter, r *http.Request, req rpcRequest, c caller) {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	// Rate-limit before any other check: an unknown tool name or malformed
	// params must still cost budget, or the ceiling is trivially free to
	// probe around.
	if over, secs := h.rateLimited(c.tokenID); over {
		h.writeRPCResult(w, req.ID, toolResult(fmt.Sprintf(
			"Rate limit exceeded (%d tool calls per minute per token). Wait about %d seconds and retry.",
			mcpToolCallLimit, secs), true))
		return
	}
	if len(req.Params) == 0 || json.Unmarshal(req.Params, &params) != nil || params.Name == "" {
		h.writeRPCError(w, http.StatusOK, req.ID, codeInvalidParams, "tools/call requires a tool name")
		return
	}
	t, ok := toolByName[params.Name]
	if !ok {
		h.writeRPCError(w, http.StatusOK, req.ID, codeInvalidParams, "Unknown tool: "+params.Name)
		return
	}
	if t.write && c.scope != store.ScopeReadWrite {
		h.writeRPCResult(w, req.ID, toolResult(
			"This tool requires a read_write token. Your token is read_only, so it cannot make changes.", true))
		return
	}
	args := params.Arguments
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	text, isErr := t.handler(r.Context(), h, c, args)
	h.writeRPCResult(w, req.ID, toolResult(text, isErr))
}

// toolResult builds a tools/call result: one text content block plus the
// isError flag.
func toolResult(text string, isError bool) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": isError,
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
				"optional (unlinked tasks are allowed).",
			write: true,
			inputSchema: objectSchema(map[string]any{
				"space_id":   strProp("The space to create the task in."),
				"title":      strProp("What needs doing."),
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
				"remind_at is an ISO datetime like 2026-07-28T09:00:00.",
			write: true,
			inputSchema: objectSchema(map[string]any{
				"space_id":   strProp("The space to create the reminder in."),
				"text":       strProp("What to be reminded about."),
				"remind_at":  strProp("When to resurface, ISO datetime (e.g. 2026-07-28T09:00:00)."),
				"project_id": strProp("Optional project to attach the reminder to (must exist if given)."),
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
			name:  "update_project",
			title: "Update project designations",
			description: "Manage a project's designations: nextAction, altNextActions, currentFocus, " +
				"resumeContext, status, and waitingOn. Use it to set the single next concrete action or to move " +
				"status. Only the fields you pass change; status is set manually here (never inferred).",
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
			}, "space_id", "project_id"),
			handler: toolUpdateProject,
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
