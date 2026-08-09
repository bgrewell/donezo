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
	"time"

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
		// Details is searched for the same reason an activity's is: this is
		// where long-form text now lives, and update_task actively tells
		// callers to move it here.
		if containsFold(t.Title, q) || containsFold(t.Details, q) ||
			(t.WaitingOn != nil && containsFold(*t.WaitingOn, q)) {
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
		if containsFold(rem.Text, q) || containsFold(rem.Details, q) {
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

// matchesProject reports whether an entity's optional project pointer
// matches the requested filter. An empty filter matches everything.
func matchesProject(projectID *string, want string) bool {
	if want == "" {
		return true
	}
	return projectID != nil && *projectID == want
}

func toolListTasks(ctx context.Context, h *Handler, c caller, args json.RawMessage) (string, bool) {
	var a struct {
		SpaceID   string `json:"space_id"`
		ProjectID string `json:"project_id"`
		Status    string `json:"status"`
	}
	if !decodeArgs(args, &a) {
		return "invalid arguments", true
	}
	status := a.Status
	if status == "" {
		status = "open"
	}
	if !oneOf(status, taskStatuses) {
		return "status must be one of " + strings.Join(taskStatuses, ", "), true
	}
	sp, msg, ok := h.ownedSpace(ctx, c, a.SpaceID)
	if !ok {
		return msg, true
	}
	all, err := h.spaces.ListTasks(ctx, sp.ID)
	if err != nil {
		h.logger.Printf("mcp list tasks: %v", err)
		return "internal error", true
	}
	matched := []store.TaskItem{}
	for _, t := range all {
		if t.Status == status && matchesProject(t.ProjectID, a.ProjectID) {
			matched = append(matched, t)
		}
	}
	matched, truncated := capItems(matched)
	out := map[string]any{"tasks": matched, "count": len(matched), "status": status}
	if truncated {
		out["note"] = fmt.Sprintf("showing the first %d tasks", maxItems)
	}
	return jsonText(out), false
}

func toolListNotes(ctx context.Context, h *Handler, c caller, args json.RawMessage) (string, bool) {
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
	all, err := h.spaces.ListNotes(ctx, sp.ID)
	if err != nil {
		h.logger.Printf("mcp list notes: %v", err)
		return "internal error", true
	}
	matched := []store.NoteItem{}
	for _, n := range all {
		if matchesProject(n.ProjectID, a.ProjectID) {
			matched = append(matched, n)
		}
	}
	matched, truncated := capItems(matched)
	out := map[string]any{"notes": matched, "count": len(matched)}
	if truncated {
		out["note"] = fmt.Sprintf("showing the first %d notes", maxItems)
	}
	return jsonText(out), false
}

func toolListReminders(ctx context.Context, h *Handler, c caller, args json.RawMessage) (string, bool) {
	var a struct {
		SpaceID     string `json:"space_id"`
		IncludeDone bool   `json:"include_done"`
	}
	if !decodeArgs(args, &a) {
		return "invalid arguments", true
	}
	sp, msg, ok := h.ownedSpace(ctx, c, a.SpaceID)
	if !ok {
		return msg, true
	}
	all, err := h.spaces.ListReminders(ctx, sp.ID)
	if err != nil {
		h.logger.Printf("mcp list reminders: %v", err)
		return "internal error", true
	}
	matched := []store.Reminder{}
	for _, r := range all {
		if !a.IncludeDone && r.Done != nil && *r.Done {
			continue
		}
		matched = append(matched, r)
	}
	sort.SliceStable(matched, func(i, j int) bool { return matched[i].RemindAt < matched[j].RemindAt })
	matched, truncated := capItems(matched)
	out := map[string]any{"reminders": matched, "count": len(matched)}
	if truncated {
		out["note"] = fmt.Sprintf("showing the first %d reminders", maxItems)
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
		CapturedAt:         h.nowLocal(h.callerLocation(ctx, c)),
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
	if a.Date != "" && !validDate(a.Date) {
		return "date must be a yyyy-MM-dd date", true
	}
	sp, msg, ok := h.ownedLiveSpace(ctx, c, a.SpaceID)
	if !ok {
		return msg, true
	}
	date := a.Date
	if date == "" {
		date = h.today(h.callerLocation(ctx, c))
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
		Details   string `json:"details"`
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
		Details:   a.Details,
		Status:    "open",
		Due:       optString(a.Due),
		CreatedAt: h.today(h.callerLocation(ctx, c)),
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
			Date:      h.today(h.callerLocation(ctx, c)),
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
		CreatedAt: h.today(h.callerLocation(ctx, c)),
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
		Details   string `json:"details"`
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
		Details:   a.Details,
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
	Details   string `json:"details"`
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

	conv, vmsg, okc := h.buildConversion(a, raw, h.callerLocation(ctx, c))
	if !okc {
		return vmsg, true
	}
	if _, err := h.spaces.ConvertInboxItem(ctx, sp.ID, a.InboxID, conv); err != nil {
		return h.storeErrText("inbox item", err), true
	}
	return jsonText(map[string]any{"kind": conv.Kind, "created": conversionPayload(conv), "inboxId": a.InboxID}), false
}

// convertNoteArgs is the convert_note argument set. Separate from
// classifyArgs because the two conversions take different fields: a note
// carries a body and a project of its own, and cannot become a note or a
// project, so the name/color/body fields classify needs have no meaning here.
type convertNoteArgs struct {
	SpaceID   string `json:"space_id"`
	NoteID    string `json:"note_id"`
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	Text      string `json:"text"`
	RemindAt  string `json:"remind_at"`
	ProjectID string `json:"project_id"`
	Due       string `json:"due"`
	Type      string `json:"type"`
	Details   string `json:"details"`
}

func toolConvertNote(ctx context.Context, h *Handler, c caller, args json.RawMessage) (string, bool) {
	var a convertNoteArgs
	if !decodeArgs(args, &a) {
		return "invalid arguments", true
	}
	if strings.TrimSpace(a.NoteID) == "" {
		return "note_id is required", true
	}
	// Checked against the narrower set, with a message naming what a note
	// can actually become — "note" and "project" are the two an LLM is most
	// likely to try, and the generic list would not explain the refusal.
	if !oneOf(a.Kind, noteTargetKinds) {
		return "kind must be one of " + strings.Join(noteTargetKinds, ", ") +
			" — a note cannot become another note (use update_note) or a project", true
	}
	sp, msg, ok := h.ownedLiveSpace(ctx, c, a.SpaceID)
	if !ok {
		return msg, true
	}
	// The note seeds every default, so an under-specified call carries the
	// note's own content across instead of quietly destroying it.
	note, err := h.spaces.GetNote(ctx, sp.ID, a.NoteID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return "note not found", true
		}
		h.logger.Printf("mcp convert note: get: %v", err)
		return "internal error", true
	}
	projectID := a.ProjectID
	if projectID == "" && note.ProjectID != nil {
		projectID = *note.ProjectID
	}
	// The note's body becomes the target's details. Every target has somewhere
	// to keep it now — before #44 a task or a reminder did not, and converting
	// a note into one destroyed the body outright.
	details := a.Details
	if details == "" {
		details = note.Body
	}
	conv, vmsg, okc := h.buildConversion(classifyArgs{
		Kind: a.Kind, Title: a.Title, Details: details, Text: a.Text,
		RemindAt: a.RemindAt, ProjectID: projectID, Due: a.Due, Type: a.Type,
	}, note.Title, h.callerLocation(ctx, c))
	if !okc {
		return vmsg, true
	}
	if conv.Activity != nil {
		// Source is manual, not capture: this came from a note somebody wrote
		// and then reclassified, not from the capture buffer.
		conv.Activity.Source = "manual"
	}
	if _, err := h.spaces.ConvertNote(ctx, sp.ID, a.NoteID, conv); err != nil {
		return h.storeErrText("note", err), true
	}
	return jsonText(map[string]any{
		"kind": conv.Kind, "created": conversionPayload(conv),
		"noteId": a.NoteID, "noteRemoved": true,
	}), false
}

// splitCapture divides a raw capture into a scannable short form and the rest.
//
// A capture is very often a first line followed by context, and before tasks
// and reminders had anywhere to put context, all of it became the title —
// which is how this repo's own backlog ended up with paragraph-length titles.
// The break has to be an explicit newline the person actually typed: guessing
// a sentence boundary inside one long line would be inventing structure the
// author did not give.
func splitCapture(raw string) (short, long string) {
	trimmed := strings.TrimSpace(raw)
	before, after, found := strings.Cut(trimmed, "\n")
	if !found {
		return trimmed, ""
	}
	return strings.TrimSpace(before), strings.TrimSpace(after)
}

// buildConversion assembles the store.Conversion for a classify call,
// generating the target id and filling defaults from the raw capture. loc is
// the caller's zone, in which every date it defaults is resolved.
func (h *Handler) buildConversion(a classifyArgs, raw string, loc *time.Location) (store.Conversion, string, bool) {
	switch a.Kind {
	case "task":
		if a.Due != "" && !validDate(a.Due) {
			return store.Conversion{}, "due must be a yyyy-MM-dd date", false
		}
		id, err := newID("tsk")
		if err != nil {
			return store.Conversion{}, "internal error", false
		}
		title, rest := splitCapture(raw)
		details := a.Details
		if details == "" {
			details = rest
		}
		if a.Title != "" {
			title = a.Title
		}
		return store.Conversion{Kind: "task", Task: &store.TaskItem{
			ID: id, ProjectID: optString(a.ProjectID), Title: title, Details: details,
			Status: "open", Due: optString(a.Due), CreatedAt: h.today(loc),
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
			ID: id, ProjectID: optString(a.ProjectID), Title: title, Body: body, CreatedAt: h.today(loc),
		}}, "", true
	case "reminder":
		if !validDateTime(a.RemindAt) {
			return store.Conversion{}, "remind_at is required for kind reminder (ISO datetime)", false
		}
		id, err := newID("rem")
		if err != nil {
			return store.Conversion{}, "internal error", false
		}
		text, rest := splitCapture(raw)
		details := a.Details
		if details == "" {
			details = rest
		}
		if a.Text != "" {
			text = a.Text
		}
		return store.Conversion{Kind: "reminder", Reminder: &store.Reminder{
			ID: id, Text: text, Details: details, RemindAt: a.RemindAt,
			ProjectID: optString(a.ProjectID),
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
		title, rest := splitCapture(raw)
		details := a.Details
		if details == "" {
			details = rest
		}
		if a.Title != "" {
			title = a.Title
		}
		return store.Conversion{Kind: "activity", Activity: &store.ActivityEntry{
			ID: id, ProjectID: a.ProjectID, Date: h.today(loc), Type: actType, Title: title,
			Details: details, Source: "capture", Tags: []string{}, Links: []store.ActivityLink{},
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
		Name           *string   `json:"name"`
		Purpose        *string   `json:"purpose"`
		Outcome        *string   `json:"outcome"`
		Color          *string   `json:"color"`
		Tags           *[]string `json:"tags"`
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
	if a.Color != nil && !oneOf(*a.Color, projectColors) {
		return "color must be one of " + strings.Join(projectColors, ", "), true
	}
	if a.Name != nil && strings.TrimSpace(*a.Name) == "" {
		return "name cannot be empty", true
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
		if a.Name != nil {
			p.Name = *a.Name
		}
		if a.Purpose != nil {
			p.Purpose = *a.Purpose
		}
		if a.Outcome != nil {
			p.Outcome = *a.Outcome
		}
		if a.Color != nil {
			p.Color = *a.Color
		}
		if a.Tags != nil {
			p.Tags = *a.Tags
		}
		return nil
	})
	if err != nil {
		return h.storeErrText("project", err), true
	}
	return jsonText(map[string]any{"project": updated}), false
}

func toolCreateProject(ctx context.Context, h *Handler, c caller, args json.RawMessage) (string, bool) {
	var a struct {
		SpaceID      string   `json:"space_id"`
		Name         string   `json:"name"`
		Purpose      string   `json:"purpose"`
		Outcome      string   `json:"outcome"`
		Color        string   `json:"color"`
		CurrentFocus string   `json:"current_focus"`
		NextAction   string   `json:"next_action"`
		Tags         []string `json:"tags"`
	}
	if !decodeArgs(args, &a) {
		return "invalid arguments", true
	}
	if strings.TrimSpace(a.Name) == "" {
		return "name is required", true
	}
	color := a.Color
	if color == "" {
		color = "blue"
	}
	if !oneOf(color, projectColors) {
		return "color must be one of " + strings.Join(projectColors, ", "), true
	}
	sp, msg, ok := h.ownedLiveSpace(ctx, c, a.SpaceID)
	if !ok {
		return msg, true
	}
	id, err := newID("proj")
	if err != nil {
		h.logger.Printf("mcp create project: %v", err)
		return "internal error", true
	}
	tags := a.Tags
	if tags == nil {
		tags = []string{}
	}
	created, err := h.spaces.CreateProject(ctx, sp.ID, store.Project{
		ID:             id,
		Name:           a.Name,
		Color:          color,
		Purpose:        a.Purpose,
		Outcome:        a.Outcome,
		CurrentFocus:   a.CurrentFocus,
		NextAction:     a.NextAction,
		AltNextActions: []string{},
		Status:         "active",
		Tags:           tags,
	})
	if err != nil {
		return h.storeErrText("project", err), true
	}
	return jsonText(map[string]any{"project": created}), false
}

func toolUpdateTask(ctx context.Context, h *Handler, c caller, args json.RawMessage) (string, bool) {
	var a struct {
		SpaceID   string  `json:"space_id"`
		TaskID    string  `json:"task_id"`
		Title     *string `json:"title"`
		Details   *string `json:"details"`
		Status    *string `json:"status"`
		Due       *string `json:"due"`
		ProjectID *string `json:"project_id"`
		WaitingOn *string `json:"waiting_on"`
	}
	if !decodeArgs(args, &a) {
		return "invalid arguments", true
	}
	if strings.TrimSpace(a.TaskID) == "" {
		return "task_id is required", true
	}
	if a.Title != nil && strings.TrimSpace(*a.Title) == "" {
		return "title cannot be empty", true
	}
	if a.Status != nil && !oneOf(*a.Status, taskStatuses) {
		return "status must be one of " + strings.Join(taskStatuses, ", "), true
	}
	// An empty due clears the date; anything else must be a real date.
	if a.Due != nil && *a.Due != "" && !validDate(*a.Due) {
		return "due must be a yyyy-MM-dd date", true
	}
	sp, msg, ok := h.ownedLiveSpace(ctx, c, a.SpaceID)
	if !ok {
		return msg, true
	}
	updated, err := h.spaces.PatchTask(ctx, sp.ID, a.TaskID, func(t *store.TaskItem) error {
		if a.Title != nil {
			t.Title = *a.Title
		}
		if a.Details != nil {
			t.Details = *a.Details
		}
		if a.Status != nil {
			t.Status = *a.Status
		}
		if a.Due != nil {
			t.Due = optString(*a.Due)
		}
		if a.ProjectID != nil {
			t.ProjectID = optString(*a.ProjectID)
		}
		if a.WaitingOn != nil {
			t.WaitingOn = optString(*a.WaitingOn)
		}
		return nil
	})
	if err != nil {
		return h.storeErrText("task", err), true
	}
	return jsonText(map[string]any{"task": updated}), false
}

func toolUpdateNote(ctx context.Context, h *Handler, c caller, args json.RawMessage) (string, bool) {
	var a struct {
		SpaceID   string  `json:"space_id"`
		NoteID    string  `json:"note_id"`
		Title     *string `json:"title"`
		Body      *string `json:"body"`
		ProjectID *string `json:"project_id"`
	}
	if !decodeArgs(args, &a) {
		return "invalid arguments", true
	}
	if strings.TrimSpace(a.NoteID) == "" {
		return "note_id is required", true
	}
	if a.Title != nil && strings.TrimSpace(*a.Title) == "" {
		return "title cannot be empty", true
	}
	if a.Body != nil && strings.TrimSpace(*a.Body) == "" {
		return "body cannot be empty", true
	}
	sp, msg, ok := h.ownedLiveSpace(ctx, c, a.SpaceID)
	if !ok {
		return msg, true
	}
	// PatchNote (not Get + Update) so the read and the write share one
	// transaction: a concurrent update_note on the same id would otherwise
	// be rewritten from this handler's stale snapshot, silently reverting
	// whichever field the other call changed.
	updated, err := h.spaces.PatchNote(ctx, sp.ID, a.NoteID, func(n *store.NoteItem) error {
		if a.Title != nil {
			n.Title = *a.Title
		}
		if a.Body != nil {
			n.Body = *a.Body
		}
		if a.ProjectID != nil {
			n.ProjectID = optString(*a.ProjectID)
		}
		return nil
	})
	if err != nil {
		return h.storeErrText("note", err), true
	}
	return jsonText(map[string]any{"note": updated}), false
}

func toolUpdateActivity(ctx context.Context, h *Handler, c caller, args json.RawMessage) (string, bool) {
	var a struct {
		SpaceID     string   `json:"space_id"`
		ActivityID  string   `json:"activity_id"`
		Title       *string  `json:"title"`
		Details     *string  `json:"details"`
		Type        *string  `json:"type"`
		Date        *string  `json:"date"`
		EffortHours *float64 `json:"effort_hours"`
		ProjectID   *string  `json:"project_id"`
	}
	if !decodeArgs(args, &a) {
		return "invalid arguments", true
	}
	if strings.TrimSpace(a.ActivityID) == "" {
		return "activity_id is required", true
	}
	if a.Title != nil && strings.TrimSpace(*a.Title) == "" {
		return "title cannot be empty", true
	}
	if a.Type != nil && !oneOf(*a.Type, activityTypes) {
		return "type must be one of " + strings.Join(activityTypes, ", "), true
	}
	if a.Date != nil && !validDate(*a.Date) {
		return "date must be a yyyy-MM-dd date", true
	}
	if a.EffortHours != nil && *a.EffortHours < 0 {
		return "effort_hours cannot be negative", true
	}
	if a.ProjectID != nil && strings.TrimSpace(*a.ProjectID) == "" {
		return "project_id cannot be empty (an activity always belongs to a project)", true
	}
	sp, msg, ok := h.ownedLiveSpace(ctx, c, a.SpaceID)
	if !ok {
		return msg, true
	}
	updated, err := h.spaces.PatchActivity(ctx, sp.ID, a.ActivityID, func(e *store.ActivityEntry) error {
		if a.Title != nil {
			e.Title = *a.Title
		}
		if a.Details != nil {
			e.Details = *a.Details
		}
		if a.Type != nil {
			e.Type = *a.Type
		}
		if a.Date != nil {
			e.Date = *a.Date
		}
		if a.EffortHours != nil {
			// 0 clears the optional column rather than storing a zero effort.
			if *a.EffortHours == 0 {
				e.EffortHours = nil
			} else {
				v := *a.EffortHours
				e.EffortHours = &v
			}
		}
		if a.ProjectID != nil {
			e.ProjectID = *a.ProjectID
		}
		return nil
	})
	if err != nil {
		return h.storeErrText("activity", err), true
	}
	return jsonText(map[string]any{"activity": updated}), false
}

func toolUpdateReminder(ctx context.Context, h *Handler, c caller, args json.RawMessage) (string, bool) {
	var a struct {
		SpaceID    string  `json:"space_id"`
		ReminderID string  `json:"reminder_id"`
		Text       *string `json:"text"`
		Details    *string `json:"details"`
		RemindAt   *string `json:"remind_at"`
		Done       *bool   `json:"done"`
		ProjectID  *string `json:"project_id"`
	}
	if !decodeArgs(args, &a) {
		return "invalid arguments", true
	}
	if strings.TrimSpace(a.ReminderID) == "" {
		return "reminder_id is required", true
	}
	if a.Text != nil && strings.TrimSpace(*a.Text) == "" {
		return "text cannot be empty", true
	}
	if a.RemindAt != nil && !validDateTime(*a.RemindAt) {
		return "remind_at must be an ISO datetime like 2026-07-28T09:00:00", true
	}
	sp, msg, ok := h.ownedLiveSpace(ctx, c, a.SpaceID)
	if !ok {
		return msg, true
	}
	updated, err := h.spaces.PatchReminder(ctx, sp.ID, a.ReminderID, func(r *store.Reminder) error {
		if a.Text != nil {
			r.Text = *a.Text
		}
		if a.Details != nil {
			r.Details = *a.Details
		}
		if a.RemindAt != nil {
			r.RemindAt = *a.RemindAt
		}
		if a.Done != nil {
			v := *a.Done
			r.Done = &v
		}
		if a.ProjectID != nil {
			r.ProjectID = optString(*a.ProjectID)
		}
		return nil
	})
	if err != nil {
		return h.storeErrText("reminder", err), true
	}
	return jsonText(map[string]any{"reminder": updated}), false
}

// errNotPending marks a dismiss attempt on an already-triaged capture. It
// travels back out of PatchInboxItem's apply func (which returns apply
// errors verbatim), so it is matched before storeErrText — otherwise the
// reason would be flattened into a generic "internal error".
var errNotPending = errors.New("inbox item is not pending")

func toolDismissInboxItem(ctx context.Context, h *Handler, c caller, args json.RawMessage) (string, bool) {
	var a struct {
		SpaceID string `json:"space_id"`
		InboxID string `json:"inbox_id"`
	}
	if !decodeArgs(args, &a) {
		return "invalid arguments", true
	}
	if strings.TrimSpace(a.InboxID) == "" {
		return "inbox_id is required", true
	}
	sp, msg, ok := h.ownedLiveSpace(ctx, c, a.SpaceID)
	if !ok {
		return msg, true
	}
	priorStatus := ""
	updated, err := h.spaces.PatchInboxItem(ctx, sp.ID, a.InboxID, func(it *store.InboxItem) error {
		if it.Status != "pending" {
			priorStatus = it.Status
			return errNotPending
		}
		it.Status = "dismissed"
		return nil
	})
	if errors.Is(err, errNotPending) {
		return "inbox item is already " + priorStatus + " — only pending captures can be dismissed", true
	}
	if err != nil {
		return h.storeErrText("inbox item", err), true
	}
	return jsonText(map[string]any{"inboxItem": updated}), false
}

func toolDeleteItem(ctx context.Context, h *Handler, c caller, args json.RawMessage) (string, bool) {
	var a struct {
		SpaceID string `json:"space_id"`
		Kind    string `json:"kind"`
		ItemID  string `json:"item_id"`
	}
	if !decodeArgs(args, &a) {
		return "invalid arguments", true
	}
	if strings.TrimSpace(a.ItemID) == "" {
		return "item_id is required", true
	}
	// Projects are excluded from the schema enum, but answer the attempt
	// with the reason rather than a bare validation failure.
	if a.Kind == "project" {
		return "deleting a project is not available here: it cascades to every activity, task, " +
			"note and reminder the project owns. Delete it from the donezo web app instead.", true
	}
	if !oneOf(a.Kind, deletableKinds) {
		return "kind must be one of " + strings.Join(deletableKinds, ", "), true
	}
	sp, msg, ok := h.ownedLiveSpace(ctx, c, a.SpaceID)
	if !ok {
		return msg, true
	}
	var err error
	switch a.Kind {
	case "task":
		err = h.spaces.DeleteTask(ctx, sp.ID, a.ItemID)
	case "note":
		err = h.spaces.DeleteNote(ctx, sp.ID, a.ItemID)
	case "reminder":
		err = h.spaces.DeleteReminder(ctx, sp.ID, a.ItemID)
	case "activity":
		err = h.spaces.DeleteActivity(ctx, sp.ID, a.ItemID)
	case "inbox_item":
		err = h.spaces.DeleteInboxItem(ctx, sp.ID, a.ItemID)
	}
	if err != nil {
		return h.storeErrText(strings.ReplaceAll(a.Kind, "_", " "), err), true
	}
	return jsonText(map[string]any{"deleted": true, "kind": a.Kind, "id": a.ItemID}), false
}
