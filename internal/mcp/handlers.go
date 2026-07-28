package mcp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/bgrewell/donezo/internal/store"
)

// This file holds the tool handlers. Each resolves the target space to one
// the caller owns (foreign and unknown spaces both read as "space not
// found", so ids are not probeable), then calls the store layer directly —
// no HTTP loopback. Write handlers additionally require the space to be
// live (unarchived). Server-generated ids match the web's newId shape
// (prefix + 8 hex); LLM callers never mint ids.

// newID returns a fresh entity id: a readable prefix plus 8 hex chars,
// matching the frontend's newId() shape and the entity-id validation the
// REST layer enforces.
func newID(prefix string) (string, error) {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("mcp: generate id: %w", err)
	}
	return prefix + "-" + hex.EncodeToString(buf[:]), nil
}

// jsonText renders v as indented JSON for a tool's text result.
func jsonText(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("error encoding result: %v", err)
	}
	return string(b)
}

// decodeArgs unmarshals a tool's arguments; a non-object payload fails.
func decodeArgs(args json.RawMessage, dst any) bool {
	return json.Unmarshal(args, dst) == nil
}

// ownedSpace resolves spaceID to a space the caller owns. On failure it
// returns a caller-safe message: unknown and foreign spaces are both
// "space not found".
func (h *Handler) ownedSpace(ctx context.Context, c caller, spaceID string) (store.Space, string, bool) {
	if strings.TrimSpace(spaceID) == "" {
		return store.Space{}, "space_id is required", false
	}
	sp, err := h.core.GetSpace(ctx, spaceID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return store.Space{}, "space not found", false
		}
		h.logger.Printf("mcp get space %s: %v", spaceID, err)
		return store.Space{}, "internal error", false
	}
	if sp.UserID != c.user.ID {
		return store.Space{}, "space not found", false
	}
	return sp, "", true
}

// ownedLiveSpace resolves spaceID like ownedSpace and additionally requires
// it to be unarchived, so archived spaces are a real write barrier.
func (h *Handler) ownedLiveSpace(ctx context.Context, c caller, spaceID string) (store.Space, string, bool) {
	sp, msg, ok := h.ownedSpace(ctx, c, spaceID)
	if !ok {
		return store.Space{}, msg, false
	}
	if sp.ArchivedAt != nil {
		return store.Space{}, "space is archived — unarchive it to make changes", false
	}
	return sp, "", true
}

// storeErrText maps a store write error onto a caller-safe message.
func (h *Handler) storeErrText(kind string, err error) string {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return kind + " not found"
	case errors.Is(err, store.ErrDuplicateID):
		return kind + " id already exists"
	case errors.Is(err, store.ErrInvalidReference):
		return "projectId does not match an existing project"
	default:
		h.logger.Printf("mcp %s write: %v", kind, err)
		return "internal error"
	}
}

// containsFold reports a case-insensitive substring match, treating empty
// haystacks as non-matches (mirroring the web UI's search predicate).
func containsFold(hay, needleLower string) bool {
	return hay != "" && strings.Contains(strings.ToLower(hay), needleLower)
}

// anyContainsFold reports whether any element matches.
func anyContainsFold(hay []string, needleLower string) bool {
	for _, s := range hay {
		if containsFold(s, needleLower) {
			return true
		}
	}
	return false
}

// capItems caps a slice at maxItems, reporting whether it was truncated.
func capItems[T any](items []T) ([]T, bool) {
	if len(items) > maxItems {
		return items[:maxItems], true
	}
	return items, false
}

// ─── READ handlers ────────────────────────────────────────────────────────

// spaceView is the compact list_spaces row.
type spaceView struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Color    string `json:"color"`
	Archived bool   `json:"archived"`
}

func toolListSpaces(ctx context.Context, h *Handler, c caller, _ json.RawMessage) (string, bool) {
	all, err := h.core.ListSpaces(ctx)
	if err != nil {
		h.logger.Printf("mcp list spaces: %v", err)
		return "internal error", true
	}
	views := []spaceView{}
	for _, sp := range all {
		if sp.UserID != c.user.ID {
			continue
		}
		views = append(views, spaceView{ID: sp.ID, Name: sp.Name, Color: sp.Color, Archived: sp.ArchivedAt != nil})
	}
	views, truncated := capItems(views)
	out := map[string]any{"spaces": views, "count": len(views)}
	if truncated {
		out["note"] = fmt.Sprintf("showing the first %d spaces", maxItems)
	}
	return jsonText(out), false
}

// projectSummary is one project row in the space overview.
type projectSummary struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Status         string   `json:"status"`
	CurrentFocus   string   `json:"currentFocus"`
	NextAction     string   `json:"nextAction"`
	AltNextActions []string `json:"altNextActions"`
}

func toolGetSpaceOverview(ctx context.Context, h *Handler, c caller, args json.RawMessage) (string, bool) {
	var a struct {
		SpaceID string `json:"space_id"`
	}
	if !decodeArgs(args, &a) {
		return "invalid arguments", true
	}
	sp, msg, ok := h.ownedSpace(ctx, c, a.SpaceID)
	if !ok {
		return msg, true
	}
	projects, err := h.spaces.ListProjects(ctx, sp.ID)
	if err != nil {
		h.logger.Printf("mcp overview projects: %v", err)
		return "internal error", true
	}
	tasks, err := h.spaces.ListTasks(ctx, sp.ID)
	if err != nil {
		h.logger.Printf("mcp overview tasks: %v", err)
		return "internal error", true
	}
	inbox, err := h.spaces.ListInboxItems(ctx, sp.ID)
	if err != nil {
		h.logger.Printf("mcp overview inbox: %v", err)
		return "internal error", true
	}
	summaries := make([]projectSummary, 0, len(projects))
	for _, p := range projects {
		summaries = append(summaries, projectSummary{
			ID: p.ID, Name: p.Name, Status: p.Status, CurrentFocus: p.CurrentFocus,
			NextAction: p.NextAction, AltNextActions: p.AltNextActions,
		})
	}
	summaries, truncated := capItems(summaries)
	openTasks := 0
	for _, t := range tasks {
		if t.Status != "done" {
			openTasks++
		}
	}
	pendingInbox := 0
	for _, it := range inbox {
		if it.Status == "pending" {
			pendingInbox++
		}
	}
	out := map[string]any{
		"spaceId":           sp.ID,
		"spaceName":         sp.Name,
		"projects":          summaries,
		"projectCount":      len(projects),
		"openTaskCount":     openTasks,
		"pendingInboxCount": pendingInbox,
	}
	if truncated {
		out["note"] = fmt.Sprintf("showing the first %d projects of %d", maxItems, len(projects))
	}
	return jsonText(out), false
}

func toolGetProject(ctx context.Context, h *Handler, c caller, args json.RawMessage) (string, bool) {
	var a struct {
		SpaceID   string `json:"space_id"`
		ProjectID string `json:"project_id"`
	}
	if !decodeArgs(args, &a) {
		return "invalid arguments", true
	}
	sp, msg, ok := h.ownedSpace(ctx, c, a.SpaceID)
	if !ok {
		return msg, true
	}
	project, err := h.spaces.GetProject(ctx, sp.ID, a.ProjectID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return "project not found", true
		}
		h.logger.Printf("mcp get project: %v", err)
		return "internal error", true
	}
	tasks, err := h.spaces.ListTasks(ctx, sp.ID)
	if err != nil {
		h.logger.Printf("mcp get project tasks: %v", err)
		return "internal error", true
	}
	activities, err := h.spaces.ListActivities(ctx, sp.ID)
	if err != nil {
		h.logger.Printf("mcp get project activities: %v", err)
		return "internal error", true
	}
	openTasks := []store.TaskItem{}
	for _, t := range tasks {
		if t.ProjectID != nil && *t.ProjectID == project.ID && t.Status != "done" {
			openTasks = append(openTasks, t)
		}
	}
	openTasks, tasksTruncated := capItems(openTasks)
	projActs := []store.ActivityEntry{}
	for _, act := range activities {
		if act.ProjectID == project.ID {
			projActs = append(projActs, act)
		}
	}
	// Most recent first, then keep the last 10.
	sort.SliceStable(projActs, func(i, j int) bool { return projActs[i].Date > projActs[j].Date })
	if len(projActs) > 10 {
		projActs = projActs[:10]
	}
	out := map[string]any{
		"project":          project,
		"openTasks":        openTasks,
		"recentActivities": projActs,
	}
	if tasksTruncated {
		out["note"] = fmt.Sprintf("showing the first %d open tasks", maxItems)
	}
	return jsonText(out), false
}

func toolSearch(ctx context.Context, h *Handler, c caller, args json.RawMessage) (string, bool) {
	var a struct {
		SpaceID string `json:"space_id"`
		Query   string `json:"query"`
	}
	if !decodeArgs(args, &a) {
		return "invalid arguments", true
	}
	sp, msg, ok := h.ownedSpace(ctx, c, a.SpaceID)
	if !ok {
		return msg, true
	}
	q := strings.ToLower(strings.TrimSpace(a.Query))
	if q == "" {
		return "query must not be empty", true
	}
	st, err := h.spaces.State(ctx, sp.ID)
	if err != nil {
		h.logger.Printf("mcp search state: %v", err)
		return "internal error", true
	}

	projects := []store.Project{}
	for _, p := range st.Projects {
		if containsFold(p.Name, q) || containsFold(p.Purpose, q) || containsFold(p.CurrentFocus, q) || anyContainsFold(p.Tags, q) {
			projects = append(projects, p)
		}
	}
	activities := []store.ActivityEntry{}
	for _, act := range st.Activities {
		if containsFold(act.Title, q) || containsFold(act.Details, q) || anyContainsFold(act.Tags, q) {
			activities = append(activities, act)
		}
	}
	sort.SliceStable(activities, func(i, j int) bool { return activities[i].Date > activities[j].Date })
	tasks := []store.TaskItem{}
	for _, t := range st.Tasks {
		if containsFold(t.Title, q) || (t.WaitingOn != nil && containsFold(*t.WaitingOn, q)) {
			tasks = append(tasks, t)
		}
	}
	notes := []store.NoteItem{}
	for _, n := range st.Notes {
		if containsFold(n.Title, q) || containsFold(n.Body, q) {
			notes = append(notes, n)
		}
	}
	reminders := []store.Reminder{}
	for _, rem := range st.Reminders {
		if containsFold(rem.Text, q) {
			reminders = append(reminders, rem)
		}
	}
	inbox := []store.InboxItem{}
	for _, it := range st.Inbox {
		if containsFold(it.Raw, q) {
			inbox = append(inbox, it)
		}
	}

	truncated := false
	projects, t1 := capItems(projects)
	activities, t2 := capItems(activities)
	tasks, t3 := capItems(tasks)
	notes, t4 := capItems(notes)
	reminders, t5 := capItems(reminders)
	inbox, t6 := capItems(inbox)
	truncated = t1 || t2 || t3 || t4 || t5 || t6

	out := map[string]any{
		"query":      a.Query,
		"projects":   projects,
		"activities": activities,
		"tasks":      tasks,
		"notes":      notes,
		"reminders":  reminders,
		"inbox":      inbox,
	}
	if truncated {
		out["note"] = fmt.Sprintf("some result groups were truncated to %d items; refine your query for more", maxItems)
	}
	return jsonText(out), false
}

func toolGetTimeline(ctx context.Context, h *Handler, c caller, args json.RawMessage) (string, bool) {
	var a struct {
		SpaceID  string `json:"space_id"`
		FromDate string `json:"from_date"`
		ToDate   string `json:"to_date"`
	}
	if !decodeArgs(args, &a) {
		return "invalid arguments", true
	}
	if !validDate(a.FromDate) || !validDate(a.ToDate) {
		return "from_date and to_date must be yyyy-MM-dd dates", true
	}
	if a.FromDate > a.ToDate {
		return "from_date must not be after to_date", true
	}
	sp, msg, ok := h.ownedSpace(ctx, c, a.SpaceID)
	if !ok {
		return msg, true
	}
	activities, err := h.spaces.ListActivities(ctx, sp.ID)
	if err != nil {
		h.logger.Printf("mcp timeline: %v", err)
		return "internal error", true
	}
	inRange := []store.ActivityEntry{}
	for _, act := range activities {
		if act.Date >= a.FromDate && act.Date <= a.ToDate {
			inRange = append(inRange, act)
		}
	}
	sort.SliceStable(inRange, func(i, j int) bool { return inRange[i].Date < inRange[j].Date })
	inRange, truncated := capItems(inRange)
	out := map[string]any{
		"from":       a.FromDate,
		"to":         a.ToDate,
		"activities": inRange,
		"count":      len(inRange),
	}
	if truncated {
		out["note"] = fmt.Sprintf("showing the first %d activities in range", maxItems)
	}
	return jsonText(out), false
}

func toolListInbox(ctx context.Context, h *Handler, c caller, args json.RawMessage) (string, bool) {
	var a struct {
		SpaceID string `json:"space_id"`
	}
	if !decodeArgs(args, &a) {
		return "invalid arguments", true
	}
	sp, msg, ok := h.ownedSpace(ctx, c, a.SpaceID)
	if !ok {
		return msg, true
	}
	items, err := h.spaces.ListInboxItems(ctx, sp.ID)
	if err != nil {
		h.logger.Printf("mcp list inbox: %v", err)
		return "internal error", true
	}
	pending := []store.InboxItem{}
	for _, it := range items {
		if it.Status == "pending" {
			pending = append(pending, it)
		}
	}
	pending, truncated := capItems(pending)
	out := map[string]any{"inbox": pending, "count": len(pending)}
	if truncated {
		out["note"] = fmt.Sprintf("showing the first %d pending items", maxItems)
	}
	return jsonText(out), false
}

// ─── WRITE handlers ───────────────────────────────────────────────────────

// optString returns a pointer to v when non-empty, else nil, for optional
// nullable columns.
func optString(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

// truncateRunes returns up to n runes of s (for deriving default titles).
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func toolCaptureToInbox(ctx context.Context, h *Handler, c caller, args json.RawMessage) (string, bool) {
	var a struct {
		SpaceID            string `json:"space_id"`
		Text               string `json:"text"`
		SuggestedKind      string `json:"suggested_kind"`
		SuggestedProjectID string `json:"suggested_project_id"`
	}
	if !decodeArgs(args, &a) {
		return "invalid arguments", true
	}
	if strings.TrimSpace(a.Text) == "" {
		return "text is required", true
	}
	kind := a.SuggestedKind
	if kind == "" {
		kind = "note"
	}
	if !oneOf(kind, itemKinds) {
		return "suggested_kind must be one of " + strings.Join(itemKinds, ", "), true
	}
	sp, msg, ok := h.ownedLiveSpace(ctx, c, a.SpaceID)
	if !ok {
		return msg, true
	}
	id, err := newID("inb")
	if err != nil {
		h.logger.Printf("mcp capture: %v", err)
		return "internal error", true
	}
	created, err := h.spaces.CreateInboxItem(ctx, sp.ID, store.InboxItem{
		ID:                 id,
		Raw:                a.Text,
		CapturedAt:         h.nowRFC3339(),
		SuggestedKind:      kind,
		SuggestedProjectID: optString(a.SuggestedProjectID),
		Status:             "pending",
	})
	if err != nil {
		return h.storeErrText("inbox item", err), true
	}
	return jsonText(map[string]any{"captured": created, "spaceId": sp.ID}), false
}

func toolLogActivity(ctx context.Context, h *Handler, c caller, args json.RawMessage) (string, bool) {
	var a struct {
		SpaceID     string   `json:"space_id"`
		ProjectID   string   `json:"project_id"`
		Title       string   `json:"title"`
		Details     string   `json:"details"`
		Type        string   `json:"type"`
		Date        string   `json:"date"`
		EffortHours *float64 `json:"effort_hours"`
	}
	if !decodeArgs(args, &a) {
		return "invalid arguments", true
	}
	if strings.TrimSpace(a.ProjectID) == "" {
		return "project_id is required", true
	}
	if strings.TrimSpace(a.Title) == "" {
		return "title is required", true
	}
	actType := a.Type
	if actType == "" {
		actType = "work"
	}
	if !oneOf(actType, activityTypes) {
		return "type must be one of " + strings.Join(activityTypes, ", "), true
	}
	date := a.Date
	if date == "" {
		date = h.today()
	} else if !validDate(date) {
		return "date must be a yyyy-MM-dd date", true
	}
	sp, msg, ok := h.ownedLiveSpace(ctx, c, a.SpaceID)
	if !ok {
		return msg, true
	}
	id, err := newID("act")
	if err != nil {
		h.logger.Printf("mcp log activity: %v", err)
		return "internal error", true
	}
	created, err := h.spaces.CreateActivity(ctx, sp.ID, store.ActivityEntry{
		ID:          id,
		ProjectID:   a.ProjectID,
		Date:        date,
		Type:        actType,
		Title:       a.Title,
		Details:     a.Details,
		EffortHours: a.EffortHours,
		Source:      "manual",
		Tags:        []string{},
		Links:       []store.ActivityLink{},
	})
	if err != nil {
		return h.storeErrText("activity", err), true
	}
	return jsonText(map[string]any{"activity": created}), false
}

func toolCreateTask(ctx context.Context, h *Handler, c caller, args json.RawMessage) (string, bool) {
	var a struct {
		SpaceID   string `json:"space_id"`
		Title     string `json:"title"`
		ProjectID string `json:"project_id"`
		Due       string `json:"due"`
	}
	if !decodeArgs(args, &a) {
		return "invalid arguments", true
	}
	if strings.TrimSpace(a.Title) == "" {
		return "title is required", true
	}
	if a.Due != "" && !validDate(a.Due) {
		return "due must be a yyyy-MM-dd date", true
	}
	sp, msg, ok := h.ownedLiveSpace(ctx, c, a.SpaceID)
	if !ok {
		return msg, true
	}
	id, err := newID("tsk")
	if err != nil {
		h.logger.Printf("mcp create task: %v", err)
		return "internal error", true
	}
	created, err := h.spaces.CreateTask(ctx, sp.ID, store.TaskItem{
		ID:        id,
		ProjectID: optString(a.ProjectID),
		Title:     a.Title,
		Status:    "open",
		Due:       optString(a.Due),
		CreatedAt: h.today(),
	})
	if err != nil {
		return h.storeErrText("task", err), true
	}
	return jsonText(map[string]any{"task": created}), false
}

func toolCompleteTask(ctx context.Context, h *Handler, c caller, args json.RawMessage) (string, bool) {
	var a struct {
		SpaceID     string `json:"space_id"`
		TaskID      string `json:"task_id"`
		LogActivity *bool  `json:"log_activity"`
	}
	if !decodeArgs(args, &a) {
		return "invalid arguments", true
	}
	if strings.TrimSpace(a.TaskID) == "" {
		return "task_id is required", true
	}
	sp, msg, ok := h.ownedLiveSpace(ctx, c, a.SpaceID)
	if !ok {
		return msg, true
	}
	updated, err := h.spaces.PatchTask(ctx, sp.ID, a.TaskID, func(t *store.TaskItem) error {
		t.Status = "done"
		return nil
	})
	if err != nil {
		return h.storeErrText("task", err), true
	}
	logAct := a.LogActivity == nil || *a.LogActivity
	out := map[string]any{"task": updated}
	switch {
	case !logAct:
		out["loggedActivity"] = false
	case updated.ProjectID == nil || *updated.ProjectID == "":
		out["loggedActivity"] = false
		out["note"] = "task has no project, so no activity was logged (an activity needs a project)"
	default:
		id, err := newID("act")
		if err != nil {
			h.logger.Printf("mcp complete task: %v", err)
			out["loggedActivity"] = false
			out["note"] = "task completed, but logging the activity failed"
			break
		}
		activity, err := h.spaces.CreateActivity(ctx, sp.ID, store.ActivityEntry{
			ID:        id,
			ProjectID: *updated.ProjectID,
			Date:      h.today(),
			Type:      "work",
			Title:     updated.Title,
			Details:   "",
			Source:    "manual",
			Tags:      []string{},
			Links:     []store.ActivityLink{},
		})
		if err != nil {
			// The completion already committed; report the log failure but
			// do not fail the whole call.
			h.logger.Printf("mcp complete task: log activity: %v", err)
			out["loggedActivity"] = false
			out["note"] = "task completed, but logging the activity failed"
			break
		}
		out["loggedActivity"] = true
		out["activity"] = activity
	}
	return jsonText(out), false
}

func toolCreateNote(ctx context.Context, h *Handler, c caller, args json.RawMessage) (string, bool) {
	var a struct {
		SpaceID   string `json:"space_id"`
		Body      string `json:"body"`
		Title     string `json:"title"`
		ProjectID string `json:"project_id"`
	}
	if !decodeArgs(args, &a) {
		return "invalid arguments", true
	}
	if strings.TrimSpace(a.Body) == "" {
		return "body is required", true
	}
	title := a.Title
	if title == "" {
		title = truncateRunes(a.Body, 60)
	}
	sp, msg, ok := h.ownedLiveSpace(ctx, c, a.SpaceID)
	if !ok {
		return msg, true
	}
	id, err := newID("note")
	if err != nil {
		h.logger.Printf("mcp create note: %v", err)
		return "internal error", true
	}
	created, err := h.spaces.CreateNote(ctx, sp.ID, store.NoteItem{
		ID:        id,
		ProjectID: optString(a.ProjectID),
		Title:     title,
		Body:      a.Body,
		CreatedAt: h.today(),
	})
	if err != nil {
		return h.storeErrText("note", err), true
	}
	return jsonText(map[string]any{"note": created}), false
}

func toolCreateReminder(ctx context.Context, h *Handler, c caller, args json.RawMessage) (string, bool) {
	var a struct {
		SpaceID   string `json:"space_id"`
		Text      string `json:"text"`
		RemindAt  string `json:"remind_at"`
		ProjectID string `json:"project_id"`
	}
	if !decodeArgs(args, &a) {
		return "invalid arguments", true
	}
	if strings.TrimSpace(a.Text) == "" {
		return "text is required", true
	}
	if !validDateTime(a.RemindAt) {
		return "remind_at must be an ISO datetime like 2026-07-28T09:00:00", true
	}
	sp, msg, ok := h.ownedLiveSpace(ctx, c, a.SpaceID)
	if !ok {
		return msg, true
	}
	id, err := newID("rem")
	if err != nil {
		h.logger.Printf("mcp create reminder: %v", err)
		return "internal error", true
	}
	created, err := h.spaces.CreateReminder(ctx, sp.ID, store.Reminder{
		ID:        id,
		Text:      a.Text,
		RemindAt:  a.RemindAt,
		ProjectID: optString(a.ProjectID),
	})
	if err != nil {
		return h.storeErrText("reminder", err), true
	}
	return jsonText(map[string]any{"reminder": created}), false
}

// classifyArgs is the classify_inbox_item argument set: a superset of the
// per-kind fields, with the handler picking those the chosen kind needs.
type classifyArgs struct {
	SpaceID   string `json:"space_id"`
	InboxID   string `json:"inbox_id"`
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Text      string `json:"text"`
	RemindAt  string `json:"remind_at"`
	ProjectID string `json:"project_id"`
	Due       string `json:"due"`
	Name      string `json:"name"`
	Color     string `json:"color"`
	Type      string `json:"type"`
}

func toolClassifyInboxItem(ctx context.Context, h *Handler, c caller, args json.RawMessage) (string, bool) {
	var a classifyArgs
	if !decodeArgs(args, &a) {
		return "invalid arguments", true
	}
	if strings.TrimSpace(a.InboxID) == "" {
		return "inbox_id is required", true
	}
	if !oneOf(a.Kind, itemKinds) {
		return "kind must be one of " + strings.Join(itemKinds, ", "), true
	}
	sp, msg, ok := h.ownedLiveSpace(ctx, c, a.SpaceID)
	if !ok {
		return msg, true
	}
	// The raw text seeds the defaults for the converted item.
	item, err := h.spaces.GetInboxItem(ctx, sp.ID, a.InboxID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return "inbox item not found", true
		}
		h.logger.Printf("mcp classify get inbox: %v", err)
		return "internal error", true
	}
	raw := item.Raw

	conv, vmsg, okc := h.buildConversion(a, raw)
	if !okc {
		return vmsg, true
	}
	if _, err := h.spaces.ConvertInboxItem(ctx, sp.ID, a.InboxID, conv); err != nil {
		return h.storeErrText("inbox item", err), true
	}
	return jsonText(map[string]any{"kind": conv.Kind, "created": conversionPayload(conv), "inboxId": a.InboxID}), false
}

// buildConversion assembles the store.Conversion for a classify call,
// generating the target id and filling defaults from the raw capture.
func (h *Handler) buildConversion(a classifyArgs, raw string) (store.Conversion, string, bool) {
	switch a.Kind {
	case "task":
		if a.Due != "" && !validDate(a.Due) {
			return store.Conversion{}, "due must be a yyyy-MM-dd date", false
		}
		id, err := newID("tsk")
		if err != nil {
			return store.Conversion{}, "internal error", false
		}
		title := a.Title
		if title == "" {
			title = raw
		}
		return store.Conversion{Kind: "task", Task: &store.TaskItem{
			ID: id, ProjectID: optString(a.ProjectID), Title: title, Status: "open",
			Due: optString(a.Due), CreatedAt: h.today(),
		}}, "", true
	case "note":
		id, err := newID("note")
		if err != nil {
			return store.Conversion{}, "internal error", false
		}
		title := a.Title
		if title == "" {
			title = truncateRunes(raw, 60)
		}
		body := a.Body
		if body == "" {
			body = raw
		}
		return store.Conversion{Kind: "note", Note: &store.NoteItem{
			ID: id, ProjectID: optString(a.ProjectID), Title: title, Body: body, CreatedAt: h.today(),
		}}, "", true
	case "reminder":
		if !validDateTime(a.RemindAt) {
			return store.Conversion{}, "remind_at is required for kind reminder (ISO datetime)", false
		}
		id, err := newID("rem")
		if err != nil {
			return store.Conversion{}, "internal error", false
		}
		text := a.Text
		if text == "" {
			text = raw
		}
		return store.Conversion{Kind: "reminder", Reminder: &store.Reminder{
			ID: id, Text: text, RemindAt: a.RemindAt, ProjectID: optString(a.ProjectID),
		}}, "", true
	case "activity":
		if strings.TrimSpace(a.ProjectID) == "" {
			return store.Conversion{}, "project_id is required for kind activity", false
		}
		actType := a.Type
		if actType == "" {
			actType = "work"
		}
		if !oneOf(actType, activityTypes) {
			return store.Conversion{}, "type must be one of " + strings.Join(activityTypes, ", "), false
		}
		id, err := newID("act")
		if err != nil {
			return store.Conversion{}, "internal error", false
		}
		title := a.Title
		if title == "" {
			title = raw
		}
		return store.Conversion{Kind: "activity", Activity: &store.ActivityEntry{
			ID: id, ProjectID: a.ProjectID, Date: h.today(), Type: actType, Title: title,
			Details: "", Source: "capture", Tags: []string{}, Links: []store.ActivityLink{},
		}}, "", true
	default: // project
		color := a.Color
		if color == "" {
			color = "blue"
		}
		if !oneOf(color, projectColors) {
			return store.Conversion{}, "color must be one of " + strings.Join(projectColors, ", "), false
		}
		id, err := newID("proj")
		if err != nil {
			return store.Conversion{}, "internal error", false
		}
		name := a.Name
		if name == "" {
			name = truncateRunes(raw, 60)
		}
		return store.Conversion{Kind: "project", Project: &store.Project{
			ID: id, Name: name, Color: color, Status: "active",
			AltNextActions: []string{}, Tags: []string{},
		}}, "", true
	}
}

// conversionPayload returns the created entity carried by a conversion.
func conversionPayload(conv store.Conversion) any {
	switch conv.Kind {
	case "task":
		return conv.Task
	case "note":
		return conv.Note
	case "reminder":
		return conv.Reminder
	case "activity":
		return conv.Activity
	default:
		return conv.Project
	}
}

func toolUpdateProject(ctx context.Context, h *Handler, c caller, args json.RawMessage) (string, bool) {
	var a struct {
		SpaceID        string    `json:"space_id"`
		ProjectID      string    `json:"project_id"`
		NextAction     *string   `json:"next_action"`
		AltNextActions *[]string `json:"alt_next_actions"`
		CurrentFocus   *string   `json:"current_focus"`
		ResumeContext  *string   `json:"resume_context"`
		Status         *string   `json:"status"`
		WaitingOn      *string   `json:"waiting_on"`
	}
	if !decodeArgs(args, &a) {
		return "invalid arguments", true
	}
	if strings.TrimSpace(a.ProjectID) == "" {
		return "project_id is required", true
	}
	if a.Status != nil && !oneOf(*a.Status, projectStatuses) {
		return "status must be one of " + strings.Join(projectStatuses, ", "), true
	}
	sp, msg, ok := h.ownedLiveSpace(ctx, c, a.SpaceID)
	if !ok {
		return msg, true
	}
	updated, err := h.spaces.PatchProject(ctx, sp.ID, a.ProjectID, func(p *store.Project) error {
		if a.NextAction != nil {
			p.NextAction = *a.NextAction
		}
		if a.AltNextActions != nil {
			p.AltNextActions = *a.AltNextActions
		}
		if a.CurrentFocus != nil {
			p.CurrentFocus = *a.CurrentFocus
		}
		if a.ResumeContext != nil {
			p.ResumeContext = *a.ResumeContext
		}
		if a.Status != nil {
			p.Status = *a.Status
		}
		if a.WaitingOn != nil {
			p.WaitingOn = optString(*a.WaitingOn)
		}
		return nil
	})
	if err != nil {
		return h.storeErrText("project", err), true
	}
	return jsonText(map[string]any{"project": updated}), false
}
