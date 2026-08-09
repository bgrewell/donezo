package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/bgrewell/donezo/internal/store"
)

// These exercise each tool's behavior end-to-end through the SDK client (see
// fixture.callTool in mcp_test.go), so they assert donezo's ownership, scope,
// validation, and store effects over the real wire protocol.

// parseToolJSON decodes a happy tool result's JSON text into dst.
func parseToolJSON(t *testing.T, text string, dst any) {
	t.Helper()
	if err := json.Unmarshal([]byte(text), dst); err != nil {
		t.Fatalf("parse tool text %q: %v", text, err)
	}
}

// seedInbox adds a pending inbox item to sandbox and returns its id.
func (f *fixture) seedInbox(t *testing.T, raw string) string {
	t.Helper()
	it, err := f.spaces.CreateInboxItem(context.Background(), "sandbox", store.InboxItem{
		ID: "inb-seed", Raw: raw, CapturedAt: "2026-07-26T00:00:00Z", SuggestedKind: "note", Status: "pending",
	})
	if err != nil {
		t.Fatalf("seed inbox: %v", err)
	}
	return it.ID
}

// ─── Cross-space / ownership ──────────────────────────────────────────────

func TestReadToolsForeignSpaceNotFound(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	// ben's token against space "private" (owned by other) must be
	// indistinguishable from an unknown space.
	for _, tc := range []struct {
		tool, args string
	}{
		{"get_space_overview", `{"space_id":"private"}`},
		{"get_project", `{"space_id":"private","project_id":"loom"}`},
		{"search", `{"space_id":"private","query":"x"}`},
		{"list_inbox", `{"space_id":"private"}`},
		{"get_timeline", `{"space_id":"private","from_date":"2026-01-01","to_date":"2026-12-31"}`},
		{"list_tasks", `{"space_id":"private"}`},
		{"list_notes", `{"space_id":"private"}`},
		{"list_reminders", `{"space_id":"private"}`},
		{"list_trash", `{"space_id":"private"}`},
		{"get_space_overview", `{"space_id":"nope"}`},
		{"list_notes", `{"space_id":"nope"}`},
	} {
		text, isErr := f.callTool(t, f.rw, tc.tool, tc.args)
		if !isErr || !strings.Contains(text, "space not found") {
			t.Errorf("%s on foreign/unknown space: isErr=%v text=%s", tc.tool, isErr, text)
		}
	}
}

func TestCrossSpaceCaptureWorks(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	// Give ben a second space and capture into it — capture works into ANY
	// owned space, not just a designated one.
	if _, err := f.core.CreateSpace(context.Background(), store.Space{
		ID: "second", UserID: f.user.ID, Name: "Second", Color: "green", Position: 2,
	}); err != nil {
		t.Fatalf("create second space: %v", err)
	}
	text, isErr := f.callTool(t, f.rw, "capture_to_inbox", `{"space_id":"second","text":"remember this"}`)
	if isErr {
		t.Fatalf("capture into second space: %s", text)
	}
	items, err := f.spaces.ListInboxItems(context.Background(), "second")
	if err != nil {
		t.Fatalf("list inbox: %v", err)
	}
	if len(items) != 1 || items[0].Raw != "remember this" || items[0].SuggestedKind != "note" {
		t.Errorf("second-space inbox = %+v", items)
	}
}

func TestWriteToolsRejectForeignSpace(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	text, isErr := f.callTool(t, f.rw, "capture_to_inbox", `{"space_id":"private","text":"x"}`)
	if !isErr || !strings.Contains(text, "space not found") {
		t.Errorf("write into foreign space: isErr=%v text=%s", isErr, text)
	}
}

// Every write tool must resolve its space through ownedLiveSpace, not
// ownedSpace — the archived-space barrier is one character away from being
// dropped, and the space is owned here so the ownership check cannot mask a
// missing liveness check.
func TestWriteToolsRejectArchivedSpace(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	if _, err := f.core.SetSpaceArchived(context.Background(), "sandbox", true); err != nil {
		t.Fatalf("archive: %v", err)
	}
	for _, tc := range []struct {
		tool, args string
	}{
		{"capture_to_inbox", `{"space_id":"sandbox","text":"x"}`},
		{"log_activity", `{"space_id":"sandbox","project_id":"loom","title":"x"}`},
		{"create_task", `{"space_id":"sandbox","title":"x"}`},
		{"complete_task", `{"space_id":"sandbox","task_id":"t-1"}`},
		{"create_note", `{"space_id":"sandbox","body":"x"}`},
		{"create_reminder", `{"space_id":"sandbox","text":"x","remind_at":"2026-08-01T09:00:00"}`},
		{"classify_inbox_item", `{"space_id":"sandbox","inbox_id":"i-1","kind":"task"}`},
		{"convert_note", `{"space_id":"sandbox","note_id":"n-1","kind":"task"}`},
		{"restore_item", `{"space_id":"sandbox","kind":"task","item_id":"t-1"}`},
		{"update_project", `{"space_id":"sandbox","project_id":"loom","status":"paused"}`},
		{"create_project", `{"space_id":"sandbox","name":"x"}`},
		{"update_task", `{"space_id":"sandbox","task_id":"t-1","title":"x"}`},
		{"update_note", `{"space_id":"sandbox","note_id":"n-1","body":"x"}`},
		{"update_activity", `{"space_id":"sandbox","activity_id":"a-1","title":"x"}`},
		{"update_reminder", `{"space_id":"sandbox","reminder_id":"r-1","text":"x"}`},
		{"dismiss_inbox_item", `{"space_id":"sandbox","inbox_id":"i-1"}`},
		{"delete_item", `{"space_id":"sandbox","kind":"task","item_id":"t-1"}`},
	} {
		text, isErr := f.callTool(t, f.rw, tc.tool, tc.args)
		if !isErr || !strings.Contains(text, "archived") {
			t.Errorf("%s into archived space: isErr=%v text=%s", tc.tool, isErr, text)
		}
	}
}

// ─── READ tools ───────────────────────────────────────────────────────────

func TestGetSpaceOverview(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	// One open task, one done task, one pending inbox item.
	if _, err := f.spaces.CreateTask(ctx, "sandbox", store.TaskItem{ID: "t-open", Title: "open one", Status: "open", CreatedAt: "2026-07-26"}); err != nil {
		t.Fatalf("task: %v", err)
	}
	if _, err := f.spaces.CreateTask(ctx, "sandbox", store.TaskItem{ID: "t-done", Title: "done one", Status: "done", CreatedAt: "2026-07-26"}); err != nil {
		t.Fatalf("task: %v", err)
	}
	f.seedInbox(t, "triage me")

	text, isErr := f.callTool(t, f.rw, "get_space_overview", `{"space_id":"sandbox"}`)
	if isErr {
		t.Fatalf("overview: %s", text)
	}
	var got struct {
		Projects []struct {
			ID         string `json:"id"`
			NextAction string `json:"nextAction"`
		} `json:"projects"`
		OpenTaskCount     int `json:"openTaskCount"`
		PendingInboxCount int `json:"pendingInboxCount"`
	}
	parseToolJSON(t, text, &got)
	if len(got.Projects) != 1 || got.Projects[0].ID != "loom" || got.Projects[0].NextAction != "na" {
		t.Errorf("projects = %+v", got.Projects)
	}
	if got.OpenTaskCount != 1 {
		t.Errorf("openTaskCount = %d, want 1", got.OpenTaskCount)
	}
	if got.PendingInboxCount != 1 {
		t.Errorf("pendingInboxCount = %d, want 1", got.PendingInboxCount)
	}
}

func TestGetProject(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	pid := "loom"
	if _, err := f.spaces.CreateTask(ctx, "sandbox", store.TaskItem{ID: "t1", ProjectID: &pid, Title: "todo", Status: "open", CreatedAt: "2026-07-26"}); err != nil {
		t.Fatalf("task: %v", err)
	}
	for _, d := range []string{"2026-07-20", "2026-07-25", "2026-07-10"} {
		if _, err := f.spaces.CreateActivity(ctx, "sandbox", store.ActivityEntry{
			ID: "a-" + d, ProjectID: pid, Date: d, Type: "work", Title: "did " + d, Source: "manual",
		}); err != nil {
			t.Fatalf("activity: %v", err)
		}
	}

	text, isErr := f.callTool(t, f.rw, "get_project", `{"space_id":"sandbox","project_id":"loom"}`)
	if isErr {
		t.Fatalf("get_project: %s", text)
	}
	var got struct {
		Project struct {
			ID            string `json:"id"`
			ResumeContext string `json:"resumeContext"`
		} `json:"project"`
		OpenTasks        []struct{ ID string } `json:"openTasks"`
		RecentActivities []struct {
			Date string `json:"date"`
		} `json:"recentActivities"`
	}
	parseToolJSON(t, text, &got)
	if got.Project.ID != "loom" || got.Project.ResumeContext != "rc" {
		t.Errorf("project = %+v", got.Project)
	}
	if len(got.OpenTasks) != 1 {
		t.Errorf("openTasks = %d, want 1", len(got.OpenTasks))
	}
	// Most recent first.
	if len(got.RecentActivities) != 3 || got.RecentActivities[0].Date != "2026-07-25" {
		t.Errorf("recentActivities = %+v, want newest first", got.RecentActivities)
	}

	if _, isErr := f.callTool(t, f.rw, "get_project", `{"space_id":"sandbox","project_id":"ghost"}`); !isErr {
		t.Error("unknown project should be an isError result")
	}
}

func TestSearch(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.spaces.CreateNote(ctx, "sandbox", store.NoteItem{ID: "n1", Title: "grocery", Body: "buy apples", CreatedAt: "2026-07-26"}); err != nil {
		t.Fatalf("note: %v", err)
	}
	if _, err := f.spaces.CreateTask(ctx, "sandbox", store.TaskItem{ID: "tk1", Title: "call the bank", Status: "open", CreatedAt: "2026-07-26"}); err != nil {
		t.Fatalf("task: %v", err)
	}

	text, isErr := f.callTool(t, f.rw, "search", `{"space_id":"sandbox","query":"APPLES"}`)
	if isErr {
		t.Fatalf("search: %s", text)
	}
	var got struct {
		Notes []struct{ ID string } `json:"notes"`
		Tasks []struct{ ID string } `json:"tasks"`
	}
	parseToolJSON(t, text, &got)
	if len(got.Notes) != 1 || len(got.Tasks) != 0 {
		t.Errorf("search apples: notes=%d tasks=%d", len(got.Notes), len(got.Tasks))
	}

	if text, isErr := f.callTool(t, f.rw, "search", `{"space_id":"sandbox","query":"  "}`); !isErr {
		t.Errorf("blank query should be isError, got %s", text)
	}
}

func TestGetTimeline(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	for _, d := range []string{"2026-06-01", "2026-07-15", "2026-08-01"} {
		if _, err := f.spaces.CreateActivity(ctx, "sandbox", store.ActivityEntry{
			ID: "a" + d, ProjectID: "loom", Date: d, Type: "work", Title: "t", Source: "manual",
		}); err != nil {
			t.Fatalf("activity: %v", err)
		}
	}
	text, isErr := f.callTool(t, f.rw, "get_timeline", `{"space_id":"sandbox","from_date":"2026-07-01","to_date":"2026-07-31"}`)
	if isErr {
		t.Fatalf("timeline: %s", text)
	}
	var got struct {
		Activities []struct {
			Date string `json:"date"`
		} `json:"activities"`
	}
	parseToolJSON(t, text, &got)
	if len(got.Activities) != 1 || got.Activities[0].Date != "2026-07-15" {
		t.Errorf("timeline in range = %+v", got.Activities)
	}

	for _, bad := range []string{
		`{"space_id":"sandbox","from_date":"nope","to_date":"2026-07-31"}`,
		`{"space_id":"sandbox","from_date":"2026-08-01","to_date":"2026-07-01"}`,
	} {
		if _, isErr := f.callTool(t, f.rw, "get_timeline", bad); !isErr {
			t.Errorf("bad timeline args %s should be isError", bad)
		}
	}
}

func TestListInbox(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	f.seedInbox(t, "pending one")
	if _, err := f.spaces.CreateInboxItem(ctx, "sandbox", store.InboxItem{
		ID: "inb-converted", Raw: "old", CapturedAt: "2026-07-26T00:00:00Z", SuggestedKind: "note", Status: "converted",
	}); err != nil {
		t.Fatalf("inbox: %v", err)
	}
	text, isErr := f.callTool(t, f.rw, "list_inbox", `{"space_id":"sandbox"}`)
	if isErr {
		t.Fatalf("list_inbox: %s", text)
	}
	var got struct {
		Inbox []struct{ ID string } `json:"inbox"`
	}
	parseToolJSON(t, text, &got)
	if len(got.Inbox) != 1 || got.Inbox[0].ID != "inb-seed" {
		t.Errorf("list_inbox = %+v (should be pending only)", got.Inbox)
	}
}

// ─── WRITE tools ──────────────────────────────────────────────────────────

func TestCaptureToInbox(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	if _, isErr := f.callTool(t, f.rw, "capture_to_inbox", `{"space_id":"sandbox"}`); !isErr {
		t.Error("missing text should be isError")
	}
	if _, isErr := f.callTool(t, f.rw, "capture_to_inbox", `{"space_id":"sandbox","text":"x","suggested_kind":"bogus"}`); !isErr {
		t.Error("bad suggested_kind should be isError")
	}
	text, isErr := f.callTool(t, f.rw, "capture_to_inbox", `{"space_id":"sandbox","text":"call mum","suggested_kind":"task"}`)
	if isErr {
		t.Fatalf("capture: %s", text)
	}
	items, err := f.spaces.ListInboxItems(context.Background(), "sandbox")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 1 || items[0].SuggestedKind != "task" || items[0].Status != "pending" {
		t.Errorf("captured = %+v", items)
	}
}

func TestLogActivity(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	if _, isErr := f.callTool(t, f.rw, "log_activity", `{"space_id":"sandbox","title":"x"}`); !isErr {
		t.Error("missing project_id should be isError")
	}
	if _, isErr := f.callTool(t, f.rw, "log_activity", `{"space_id":"sandbox","project_id":"loom","title":"x","type":"bogus"}`); !isErr {
		t.Error("bad type should be isError")
	}
	// Non-existent project -> foreign key rejection surfaces cleanly.
	if text, isErr := f.callTool(t, f.rw, "log_activity", `{"space_id":"sandbox","project_id":"ghost","title":"x"}`); !isErr || !strings.Contains(text, "project") {
		t.Errorf("bad project ref: isErr=%v text=%s", isErr, text)
	}
	text, isErr := f.callTool(t, f.rw, "log_activity", `{"space_id":"sandbox","project_id":"loom","title":"shipped v1","effort_hours":2.5}`)
	if isErr {
		t.Fatalf("log_activity: %s", text)
	}
	var got struct {
		Activity struct {
			Date        string  `json:"date"`
			Source      string  `json:"source"`
			EffortHours float64 `json:"effortHours"`
		} `json:"activity"`
	}
	parseToolJSON(t, text, &got)
	if got.Activity.Date != "2026-07-26" || got.Activity.Source != "manual" || got.Activity.EffortHours != 2.5 {
		t.Errorf("activity = %+v", got.Activity)
	}
}

func TestCreateTask(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	if _, isErr := f.callTool(t, f.rw, "create_task", `{"space_id":"sandbox"}`); !isErr {
		t.Error("missing title should be isError")
	}
	if _, isErr := f.callTool(t, f.rw, "create_task", `{"space_id":"sandbox","title":"x","due":"soon"}`); !isErr {
		t.Error("bad due should be isError")
	}
	text, isErr := f.callTool(t, f.rw, "create_task", `{"space_id":"sandbox","title":"write tests","due":"2026-08-01"}`)
	if isErr {
		t.Fatalf("create_task: %s", text)
	}
	var got struct {
		Task struct {
			Status    string `json:"status"`
			Due       string `json:"due"`
			CreatedAt string `json:"createdAt"`
		} `json:"task"`
	}
	parseToolJSON(t, text, &got)
	if got.Task.Status != "open" || got.Task.Due != "2026-08-01" || got.Task.CreatedAt != "2026-07-26" {
		t.Errorf("task = %+v", got.Task)
	}
}

func TestCompleteTask(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	pid := "loom"
	if _, err := f.spaces.CreateTask(ctx, "sandbox", store.TaskItem{ID: "twp", ProjectID: &pid, Title: "ship it", Status: "open", CreatedAt: "2026-07-26"}); err != nil {
		t.Fatalf("task: %v", err)
	}
	if _, err := f.spaces.CreateTask(ctx, "sandbox", store.TaskItem{ID: "tnp", Title: "loose task", Status: "open", CreatedAt: "2026-07-26"}); err != nil {
		t.Fatalf("task: %v", err)
	}

	// Unknown task.
	if text, isErr := f.callTool(t, f.rw, "complete_task", `{"space_id":"sandbox","task_id":"ghost"}`); !isErr || !strings.Contains(text, "task not found") {
		t.Errorf("unknown task: isErr=%v text=%s", isErr, text)
	}

	// With project + default log_activity=true: task done AND an activity logged.
	text, isErr := f.callTool(t, f.rw, "complete_task", `{"space_id":"sandbox","task_id":"twp"}`)
	if isErr {
		t.Fatalf("complete twp: %s", text)
	}
	var got struct {
		Task struct {
			Status string `json:"status"`
		} `json:"task"`
		LoggedActivity bool `json:"loggedActivity"`
		Activity       *struct {
			Title string `json:"title"`
		} `json:"activity"`
	}
	parseToolJSON(t, text, &got)
	if got.Task.Status != "done" || !got.LoggedActivity || got.Activity == nil || got.Activity.Title != "ship it" {
		t.Errorf("complete twp = %+v", got)
	}

	// Task without a project: completes, but no activity.
	text, _ = f.callTool(t, f.rw, "complete_task", `{"space_id":"sandbox","task_id":"tnp","log_activity":true}`)
	parseToolJSON(t, text, &got)
	if got.Task.Status != "done" || got.LoggedActivity {
		t.Errorf("complete tnp = %+v (expected done, no activity)", got)
	}

	acts, err := f.spaces.ListActivities(ctx, "sandbox")
	if err != nil {
		t.Fatalf("list activities: %v", err)
	}
	if len(acts) != 1 {
		t.Errorf("activities = %d, want exactly 1 (only the project task logged)", len(acts))
	}
}

func TestCreateNoteAndReminder(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	if _, isErr := f.callTool(t, f.rw, "create_note", `{"space_id":"sandbox"}`); !isErr {
		t.Error("missing body should be isError")
	}
	text, isErr := f.callTool(t, f.rw, "create_note", `{"space_id":"sandbox","body":"a longer body here"}`)
	if isErr {
		t.Fatalf("create_note: %s", text)
	}
	var note struct {
		Note struct {
			Title string `json:"title"`
		} `json:"note"`
	}
	parseToolJSON(t, text, &note)
	if note.Note.Title != "a longer body here" {
		t.Errorf("note title default = %q", note.Note.Title)
	}

	if _, isErr := f.callTool(t, f.rw, "create_reminder", `{"space_id":"sandbox","text":"x","remind_at":"whenever"}`); !isErr {
		t.Error("bad remind_at should be isError")
	}
	if _, isErr := f.callTool(t, f.rw, "create_reminder", `{"space_id":"sandbox","text":"standup","remind_at":"2026-07-28T09:00:00"}`); isErr {
		t.Error("valid reminder should succeed")
	}
	rems, err := f.spaces.ListReminders(context.Background(), "sandbox")
	if err != nil {
		t.Fatalf("list reminders: %v", err)
	}
	if len(rems) != 1 || rems[0].Text != "standup" {
		t.Errorf("reminders = %+v", rems)
	}
}

func TestClassifyInboxItem(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()

	// Unknown inbox id.
	if text, isErr := f.callTool(t, f.rw, "classify_inbox_item", `{"space_id":"sandbox","inbox_id":"ghost","kind":"note"}`); !isErr || !strings.Contains(text, "inbox item not found") {
		t.Errorf("unknown inbox: isErr=%v text=%s", isErr, text)
	}

	// Reminder without remind_at.
	f.seedInbox(t, "raw text")
	if _, isErr := f.callTool(t, f.rw, "classify_inbox_item", `{"space_id":"sandbox","inbox_id":"inb-seed","kind":"reminder"}`); !isErr {
		t.Error("reminder classify without remind_at should be isError")
	}
	// Activity without project.
	if _, isErr := f.callTool(t, f.rw, "classify_inbox_item", `{"space_id":"sandbox","inbox_id":"inb-seed","kind":"activity"}`); !isErr {
		t.Error("activity classify without project_id should be isError")
	}

	// Happy: convert to a note; the inbox item flips to converted.
	text, isErr := f.callTool(t, f.rw, "classify_inbox_item", `{"space_id":"sandbox","inbox_id":"inb-seed","kind":"note"}`)
	if isErr {
		t.Fatalf("classify note: %s", text)
	}
	var got struct {
		Kind    string `json:"kind"`
		Created struct {
			Title string `json:"title"`
			Body  string `json:"body"`
		} `json:"created"`
	}
	parseToolJSON(t, text, &got)
	if got.Kind != "note" || got.Created.Body != "raw text" {
		t.Errorf("classified note = %+v", got)
	}
	it, err := f.spaces.GetInboxItem(ctx, "sandbox", "inb-seed")
	if err != nil {
		t.Fatalf("get inbox: %v", err)
	}
	if it.Status != "converted" {
		t.Errorf("inbox status = %q, want converted", it.Status)
	}
	notes, err := f.spaces.ListNotes(ctx, "sandbox")
	if err != nil {
		t.Fatalf("list notes: %v", err)
	}
	if len(notes) != 1 {
		t.Errorf("notes = %d, want 1 from conversion", len(notes))
	}
}

func TestClassifyInboxItemActivity(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.seedInbox(t, "shipped the thing")
	text, isErr := f.callTool(t, f.rw, "classify_inbox_item",
		`{"space_id":"sandbox","inbox_id":"inb-seed","kind":"activity","project_id":"loom","type":"milestone"}`)
	if isErr {
		t.Fatalf("classify activity: %s", text)
	}
	acts, err := f.spaces.ListActivities(context.Background(), "sandbox")
	if err != nil {
		t.Fatalf("list activities: %v", err)
	}
	if len(acts) != 1 || acts[0].Type != "milestone" || acts[0].Source != "capture" || acts[0].Title != "shipped the thing" {
		t.Errorf("classified activity = %+v", acts)
	}
}

// seedNote adds one note to sandbox, optionally on a project.
func (f *fixture) seedNote(t *testing.T, id, title, body string, projectID *string) {
	t.Helper()
	if _, err := f.spaces.CreateNote(context.Background(), "sandbox", store.NoteItem{
		ID: id, Title: title, Body: body, ProjectID: projectID, CreatedAt: "2026-07-26",
	}); err != nil {
		t.Fatalf("seed note %s: %v", id, err)
	}
}

// noteExists reports whether the note is still in sandbox.
func (f *fixture) noteExists(t *testing.T, id string) bool {
	t.Helper()
	_, err := f.spaces.GetNote(context.Background(), "sandbox", id)
	if err == nil {
		return true
	}
	if errors.Is(err, store.ErrNotFound) {
		return false
	}
	t.Fatalf("get note %s: %v", id, err)
	return false
}

// Each target kind takes its defaults from the note, and the note itself is
// gone afterwards — the source not surviving is the whole difference from
// classify_inbox_item.
func TestConvertNote(t *testing.T) {
	t.Parallel()
	loom := "loom"

	t.Run("task", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		f.seedNote(t, "n-task", "chase the invoice", "body worth kept", &loom)
		text, isErr := f.callTool(t, f.rw, "convert_note",
			`{"space_id":"sandbox","note_id":"n-task","kind":"task","due":"2026-08-20"}`)
		if isErr {
			t.Fatalf("convert to task: %s", text)
		}
		tasks, err := f.spaces.ListTasks(context.Background(), "sandbox")
		if err != nil {
			t.Fatalf("list tasks: %v", err)
		}
		if len(tasks) != 1 {
			t.Fatalf("tasks = %+v, want 1", tasks)
		}
		got := tasks[0]
		if got.Title != "chase the invoice" || got.Status != "open" {
			t.Errorf("task = %+v, want the note's title and status open", got)
		}
		if got.ProjectID == nil || *got.ProjectID != "loom" {
			t.Errorf("task project = %v, want the note's project carried over", got.ProjectID)
		}
		if got.Due == nil || *got.Due != "2026-08-20" {
			t.Errorf("task due = %v, want 2026-08-20", got.Due)
		}
		if f.noteExists(t, "n-task") {
			t.Error("note survived its conversion")
		}
		// Since #44 a task has somewhere to keep the body, so the conversion
		// carries it instead of destroying it. Asserted against the stored
		// task: the body string also appears in the tool's reply, so checking
		// the text alone would pass either way.
		if got.Details != "body worth kept" {
			t.Errorf("task details = %q, want the note's body carried over", got.Details)
		}
		if strings.Contains(text, "droppedBody") {
			t.Errorf("nothing is dropped any more, got %s", text)
		}
	})

	t.Run("reminder", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		f.seedNote(t, "n-rem", "renew the domain", "", nil)
		text, isErr := f.callTool(t, f.rw, "convert_note",
			`{"space_id":"sandbox","note_id":"n-rem","kind":"reminder","remind_at":"2026-08-20T09:00:00"}`)
		if isErr {
			t.Fatalf("convert to reminder: %s", text)
		}
		rems, err := f.spaces.ListReminders(context.Background(), "sandbox")
		if err != nil {
			t.Fatalf("list reminders: %v", err)
		}
		if len(rems) != 1 || rems[0].Text != "renew the domain" || rems[0].RemindAt != "2026-08-20T09:00:00" {
			t.Errorf("reminder = %+v", rems)
		}
		if f.noteExists(t, "n-rem") {
			t.Error("note survived its conversion")
		}
		if rems[0].Details != "" {
			t.Errorf("reminder details = %q, want empty for a note with no body", rems[0].Details)
		}
		if strings.Contains(text, "droppedBody") {
			t.Errorf("nothing is dropped any more, got %s", text)
		}
	})

	t.Run("activity keeps the body as details", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		f.seedNote(t, "n-act", "rewrote the parser", "took most of Tuesday", &loom)
		if text, isErr := f.callTool(t, f.rw, "convert_note",
			`{"space_id":"sandbox","note_id":"n-act","kind":"activity","type":"milestone"}`); isErr {
			t.Fatalf("convert to activity: %s", text)
		}
		acts, err := f.spaces.ListActivities(context.Background(), "sandbox")
		if err != nil {
			t.Fatalf("list activities: %v", err)
		}
		if len(acts) != 1 {
			t.Fatalf("activities = %+v, want 1", acts)
		}
		got := acts[0]
		if got.Title != "rewrote the parser" || got.Details != "took most of Tuesday" {
			t.Errorf("activity = %+v, want the note's title and body", got)
		}
		if got.ProjectID != "loom" || got.Type != "milestone" {
			t.Errorf("activity project/type = %q/%q, want loom/milestone", got.ProjectID, got.Type)
		}
		// Not "capture": this came from a note somebody wrote, not the
		// capture buffer.
		if got.Source != "manual" {
			t.Errorf("activity source = %q, want manual", got.Source)
		}
		if f.noteExists(t, "n-act") {
			t.Error("note survived its conversion")
		}
	})

	t.Run("explicit fields win over the note", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		f.seedNote(t, "n-over", "vague title", "vague body", nil)
		if text, isErr := f.callTool(t, f.rw, "convert_note",
			`{"space_id":"sandbox","note_id":"n-over","kind":"activity","project_id":"loom",`+
				`"title":"precise title","details":"precise details"}`); isErr {
			t.Fatalf("convert with overrides: %s", text)
		}
		acts, err := f.spaces.ListActivities(context.Background(), "sandbox")
		if err != nil {
			t.Fatalf("list activities: %v", err)
		}
		if len(acts) != 1 || acts[0].Title != "precise title" || acts[0].Details != "precise details" {
			t.Errorf("activity = %+v, want the caller's fields", acts)
		}
	})
}

// A refused conversion must leave the note exactly where it was: the store
// does both halves in one transaction, and the handler refuses before
// touching it. A note deleted by a rejected call is unrecoverable.
func TestConvertNoteRefusalsKeepTheNote(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.seedNote(t, "n-keep", "still a note", "with a body", nil)

	for _, tc := range []struct {
		name, args, want string
	}{
		{"unknown note", `{"space_id":"sandbox","note_id":"ghost","kind":"task"}`, "note not found"},
		{"missing note_id", `{"space_id":"sandbox","kind":"task"}`, "note_id is required"},
		{"kind note", `{"space_id":"sandbox","note_id":"n-keep","kind":"note"}`, "update_note"},
		{"kind project", `{"space_id":"sandbox","note_id":"n-keep","kind":"project"}`, "cannot become"},
		{"unknown kind", `{"space_id":"sandbox","note_id":"n-keep","kind":"sandwich"}`, "kind must be one of"},
		{"reminder without remind_at", `{"space_id":"sandbox","note_id":"n-keep","kind":"reminder"}`, "remind_at is required"},
		{"activity without project", `{"space_id":"sandbox","note_id":"n-keep","kind":"activity"}`, "project_id is required"},
		{"task with a bad due", `{"space_id":"sandbox","note_id":"n-keep","kind":"task","due":"soon"}`, "yyyy-MM-dd"},
		{"activity on a missing project", `{"space_id":"sandbox","note_id":"n-keep","kind":"activity","project_id":"ghost"}`, "project"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text, isErr := f.callTool(t, f.rw, "convert_note", tc.args)
			if !isErr || !strings.Contains(text, tc.want) {
				t.Errorf("isErr=%v text=%q, want an error containing %q", isErr, text, tc.want)
			}
			if !f.noteExists(t, "n-keep") {
				t.Fatal("a refused conversion deleted the note")
			}
		})
	}

	// Nothing was created along the way either.
	ctx := context.Background()
	tasks, err := f.spaces.ListTasks(ctx, "sandbox")
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	acts, err := f.spaces.ListActivities(ctx, "sandbox")
	if err != nil {
		t.Fatalf("list activities: %v", err)
	}
	rems, err := f.spaces.ListReminders(ctx, "sandbox")
	if err != nil {
		t.Fatalf("list reminders: %v", err)
	}
	if len(tasks)+len(acts)+len(rems) != 0 {
		t.Errorf("refused conversions created %d tasks, %d activities, %d reminders", len(tasks), len(acts), len(rems))
	}
}

func TestUpdateProject(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	if _, isErr := f.callTool(t, f.rw, "update_project", `{"space_id":"sandbox","project_id":"loom","status":"bogus"}`); !isErr {
		t.Error("bad status should be isError")
	}
	if text, isErr := f.callTool(t, f.rw, "update_project", `{"space_id":"sandbox","project_id":"ghost","status":"paused"}`); !isErr || !strings.Contains(text, "project not found") {
		t.Errorf("unknown project: isErr=%v text=%s", isErr, text)
	}
	text, isErr := f.callTool(t, f.rw, "update_project",
		`{"space_id":"sandbox","project_id":"loom","next_action":"draft the RFC","status":"waiting","waiting_on":"review","alt_next_actions":["ping Sam"]}`)
	if isErr {
		t.Fatalf("update_project: %s", text)
	}
	var got struct {
		Project struct {
			NextAction     string   `json:"nextAction"`
			Status         string   `json:"status"`
			WaitingOn      *string  `json:"waitingOn"`
			AltNextActions []string `json:"altNextActions"`
		} `json:"project"`
	}
	parseToolJSON(t, text, &got)
	if got.Project.NextAction != "draft the RFC" || got.Project.Status != "waiting" ||
		got.Project.WaitingOn == nil || *got.Project.WaitingOn != "review" ||
		len(got.Project.AltNextActions) != 1 {
		t.Errorf("updated project = %+v", got.Project)
	}

	// Clearing waiting_on with an empty string. Verify against the store so
	// an omitted (nil) field in the response cannot mask a stale pointer.
	if _, isErr := f.callTool(t, f.rw, "update_project", `{"space_id":"sandbox","project_id":"loom","waiting_on":""}`); isErr {
		t.Fatalf("clear waiting_on: %s", text)
	}
	proj, err := f.spaces.GetProject(context.Background(), "sandbox", "loom")
	if err != nil {
		t.Fatalf("get project: %v", err)
	}
	if proj.WaitingOn != nil {
		t.Errorf("waiting_on should clear to nil, got %v", *proj.WaitingOn)
	}
}

// ─── READ tools: listing ──────────────────────────────────────────────────

func TestListTasks(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	loom := "loom"
	seed := []store.TaskItem{
		{ID: "t-1", Title: "open unlinked", Status: "open", CreatedAt: "2026-07-26"},
		{ID: "t-2", Title: "open on loom", Status: "open", ProjectID: &loom, CreatedAt: "2026-07-26"},
		{ID: "t-3", Title: "done one", Status: "done", CreatedAt: "2026-07-26"},
		{ID: "t-4", Title: "someday one", Status: "someday", CreatedAt: "2026-07-26"},
	}
	for _, task := range seed {
		if _, err := f.spaces.CreateTask(ctx, "sandbox", task); err != nil {
			t.Fatalf("seed task %s: %v", task.ID, err)
		}
	}

	tests := []struct {
		name string
		args string
		want []string
	}{
		{"defaults to open", `{"space_id":"sandbox"}`, []string{"t-1", "t-2"}},
		{"filter by project", `{"space_id":"sandbox","project_id":"loom"}`, []string{"t-2"}},
		{"filter by status", `{"space_id":"sandbox","status":"done"}`, []string{"t-3"}},
		{"status and project together", `{"space_id":"sandbox","status":"open","project_id":"loom"}`, []string{"t-2"}},
		{"someday", `{"space_id":"sandbox","status":"someday"}`, []string{"t-4"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			text, isErr := f.callTool(t, f.rw, "list_tasks", tc.args)
			if isErr {
				t.Fatalf("list_tasks: %s", text)
			}
			var got struct {
				Tasks []struct {
					ID string `json:"id"`
				} `json:"tasks"`
				Count int `json:"count"`
			}
			parseToolJSON(t, text, &got)
			ids := []string{}
			for _, task := range got.Tasks {
				ids = append(ids, task.ID)
			}
			sort.Strings(ids)
			if strings.Join(ids, ",") != strings.Join(tc.want, ",") {
				t.Errorf("ids = %v, want %v", ids, tc.want)
			}
			if got.Count != len(tc.want) {
				t.Errorf("count = %d, want %d", got.Count, len(tc.want))
			}
		})
	}

	if _, isErr := f.callTool(t, f.rw, "list_tasks", `{"space_id":"sandbox","status":"bogus"}`); !isErr {
		t.Error("bad status should be isError")
	}
}

func TestListNotes(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	loom := "loom"
	if _, err := f.spaces.CreateNote(ctx, "sandbox", store.NoteItem{ID: "n-1", Title: "loose", Body: "b", CreatedAt: "2026-07-26"}); err != nil {
		t.Fatalf("note: %v", err)
	}
	if _, err := f.spaces.CreateNote(ctx, "sandbox", store.NoteItem{ID: "n-2", Title: "on loom", Body: "b", ProjectID: &loom, CreatedAt: "2026-07-26"}); err != nil {
		t.Fatalf("note: %v", err)
	}

	text, isErr := f.callTool(t, f.rw, "list_notes", `{"space_id":"sandbox"}`)
	if isErr {
		t.Fatalf("list_notes: %s", text)
	}
	var all struct {
		Count int `json:"count"`
	}
	parseToolJSON(t, text, &all)
	if all.Count != 2 {
		t.Errorf("all notes count = %d, want 2", all.Count)
	}

	text, isErr = f.callTool(t, f.rw, "list_notes", `{"space_id":"sandbox","project_id":"loom"}`)
	if isErr {
		t.Fatalf("list_notes by project: %s", text)
	}
	var scoped struct {
		Notes []struct {
			ID string `json:"id"`
		} `json:"notes"`
	}
	parseToolJSON(t, text, &scoped)
	if len(scoped.Notes) != 1 || scoped.Notes[0].ID != "n-2" {
		t.Errorf("project-scoped notes = %+v, want just n-2", scoped.Notes)
	}
}

func TestListReminders(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	done := true
	seed := []store.Reminder{
		{ID: "r-late", Text: "later", RemindAt: "2026-08-02T09:00:00"},
		{ID: "r-soon", Text: "sooner", RemindAt: "2026-07-30T09:00:00"},
		{ID: "r-done", Text: "handled", RemindAt: "2026-07-29T09:00:00", Done: &done},
	}
	for _, r := range seed {
		if _, err := f.spaces.CreateReminder(ctx, "sandbox", r); err != nil {
			t.Fatalf("seed reminder %s: %v", r.ID, err)
		}
	}

	// Pending only, soonest first.
	text, isErr := f.callTool(t, f.rw, "list_reminders", `{"space_id":"sandbox"}`)
	if isErr {
		t.Fatalf("list_reminders: %s", text)
	}
	var got struct {
		Reminders []struct {
			ID string `json:"id"`
		} `json:"reminders"`
	}
	parseToolJSON(t, text, &got)
	if len(got.Reminders) != 2 || got.Reminders[0].ID != "r-soon" || got.Reminders[1].ID != "r-late" {
		t.Errorf("pending reminders = %+v, want r-soon then r-late", got.Reminders)
	}

	// include_done widens it.
	text, isErr = f.callTool(t, f.rw, "list_reminders", `{"space_id":"sandbox","include_done":true}`)
	if isErr {
		t.Fatalf("list_reminders include_done: %s", text)
	}
	var withDone struct {
		Count int `json:"count"`
	}
	parseToolJSON(t, text, &withDone)
	if withDone.Count != 3 {
		t.Errorf("include_done count = %d, want 3", withDone.Count)
	}
}

// ─── WRITE tools: create_project ──────────────────────────────────────────

func TestCreateProject(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	if _, isErr := f.callTool(t, f.rw, "create_project", `{"space_id":"sandbox","name":"  "}`); !isErr {
		t.Error("blank name should be isError")
	}
	if _, isErr := f.callTool(t, f.rw, "create_project", `{"space_id":"sandbox","name":"X","color":"chartreuse"}`); !isErr {
		t.Error("bad color should be isError")
	}

	text, isErr := f.callTool(t, f.rw, "create_project",
		`{"space_id":"sandbox","name":"Datacenter","purpose":"racks","outcome":"racked","tags":["infra"]}`)
	if isErr {
		t.Fatalf("create_project: %s", text)
	}
	var got struct {
		Project struct {
			ID      string   `json:"id"`
			Name    string   `json:"name"`
			Color   string   `json:"color"`
			Purpose string   `json:"purpose"`
			Status  string   `json:"status"`
			Tags    []string `json:"tags"`
		} `json:"project"`
	}
	parseToolJSON(t, text, &got)
	if got.Project.Name != "Datacenter" || got.Project.Purpose != "racks" {
		t.Errorf("created project = %+v", got.Project)
	}
	if got.Project.Color != "blue" {
		t.Errorf("color should default to blue, got %q", got.Project.Color)
	}
	if got.Project.Status != "active" {
		t.Errorf("status should default to active, got %q", got.Project.Status)
	}
	if len(got.Project.Tags) != 1 || got.Project.Tags[0] != "infra" {
		t.Errorf("tags = %v", got.Project.Tags)
	}
	if !strings.HasPrefix(got.Project.ID, "proj-") {
		t.Errorf("id = %q, want a proj- prefix", got.Project.ID)
	}
	// It is really in the store, not just echoed back.
	if _, err := f.spaces.GetProject(context.Background(), "sandbox", got.Project.ID); err != nil {
		t.Errorf("created project missing from store: %v", err)
	}
}

// ─── WRITE tools: updates ─────────────────────────────────────────────────

func TestUpdateTask(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	due := "2026-08-01"
	if _, err := f.spaces.CreateTask(ctx, "sandbox", store.TaskItem{
		ID: "t-1", Title: "original", Status: "open", Due: &due, CreatedAt: "2026-07-26",
	}); err != nil {
		t.Fatalf("seed task: %v", err)
	}

	bad := []struct {
		name string
		args string
	}{
		{"empty title", `{"space_id":"sandbox","task_id":"t-1","title":"  "}`},
		{"bad status", `{"space_id":"sandbox","task_id":"t-1","status":"nope"}`},
		{"bad due", `{"space_id":"sandbox","task_id":"t-1","due":"August 1st"}`},
		{"missing task_id", `{"space_id":"sandbox","task_id":""}`},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			if _, isErr := f.callTool(t, f.rw, "update_task", tc.args); !isErr {
				t.Error("should be isError")
			}
		})
	}
	if text, isErr := f.callTool(t, f.rw, "update_task", `{"space_id":"sandbox","task_id":"ghost","title":"x"}`); !isErr || !strings.Contains(text, "task not found") {
		t.Errorf("unknown task: isErr=%v text=%s", isErr, text)
	}

	text, isErr := f.callTool(t, f.rw, "update_task",
		`{"space_id":"sandbox","task_id":"t-1","title":"revised","status":"waiting","waiting_on":"Sam","project_id":"loom"}`)
	if isErr {
		t.Fatalf("update_task: %s", text)
	}
	var got struct {
		Task struct {
			Title     string  `json:"title"`
			Status    string  `json:"status"`
			WaitingOn *string `json:"waitingOn"`
			ProjectID *string `json:"projectId"`
		} `json:"task"`
	}
	parseToolJSON(t, text, &got)
	if got.Task.Title != "revised" || got.Task.Status != "waiting" ||
		got.Task.WaitingOn == nil || *got.Task.WaitingOn != "Sam" ||
		got.Task.ProjectID == nil || *got.Task.ProjectID != "loom" {
		t.Errorf("updated task = %+v", got.Task)
	}

	// Empty strings clear the optional columns; check the store so an omitted
	// field in the response cannot mask a stale pointer.
	if _, isErr := f.callTool(t, f.rw, "update_task", `{"space_id":"sandbox","task_id":"t-1","due":"","project_id":"","waiting_on":""}`); isErr {
		t.Fatal("clearing optional fields should succeed")
	}
	task, err := f.spaces.GetTask(ctx, "sandbox", "t-1")
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if task.Due != nil || task.ProjectID != nil || task.WaitingOn != nil {
		t.Errorf("optional fields should all clear, got due=%v project=%v waitingOn=%v", task.Due, task.ProjectID, task.WaitingOn)
	}
}

func TestUpdateNote(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	loom := "loom"
	if _, err := f.spaces.CreateNote(ctx, "sandbox", store.NoteItem{
		ID: "n-1", Title: "old title", Body: "old body", ProjectID: &loom, CreatedAt: "2026-07-26",
	}); err != nil {
		t.Fatalf("seed note: %v", err)
	}

	if _, isErr := f.callTool(t, f.rw, "update_note", `{"space_id":"sandbox","note_id":"n-1","body":"   "}`); !isErr {
		t.Error("empty body should be isError")
	}
	if text, isErr := f.callTool(t, f.rw, "update_note", `{"space_id":"sandbox","note_id":"ghost","body":"x"}`); !isErr || !strings.Contains(text, "note not found") {
		t.Errorf("unknown note: isErr=%v text=%s", isErr, text)
	}

	text, isErr := f.callTool(t, f.rw, "update_note",
		`{"space_id":"sandbox","note_id":"n-1","title":"new title","body":"new body"}`)
	if isErr {
		t.Fatalf("update_note: %s", text)
	}
	var got struct {
		Note struct {
			Title     string  `json:"title"`
			Body      string  `json:"body"`
			ProjectID *string `json:"projectId"`
		} `json:"note"`
	}
	parseToolJSON(t, text, &got)
	if got.Note.Title != "new title" || got.Note.Body != "new body" {
		t.Errorf("updated note = %+v", got.Note)
	}
	// An untouched field survives the patch.
	if got.Note.ProjectID == nil || *got.Note.ProjectID != "loom" {
		t.Errorf("projectId should be preserved, got %v", got.Note.ProjectID)
	}

	// Detaching from the project.
	if _, isErr := f.callTool(t, f.rw, "update_note", `{"space_id":"sandbox","note_id":"n-1","project_id":""}`); isErr {
		t.Fatal("detaching project should succeed")
	}
	note, err := f.spaces.GetNote(ctx, "sandbox", "n-1")
	if err != nil {
		t.Fatalf("get note: %v", err)
	}
	if note.ProjectID != nil {
		t.Errorf("projectId should clear to nil, got %v", *note.ProjectID)
	}
}

func TestUpdateActivity(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	effort := 2.0
	if _, err := f.spaces.CreateActivity(ctx, "sandbox", store.ActivityEntry{
		ID: "a-1", ProjectID: "loom", Date: "2026-07-26", Type: "work", Title: "old",
		Details: "d", EffortHours: &effort, Source: "manual", Tags: []string{}, Links: []store.ActivityLink{},
	}); err != nil {
		t.Fatalf("seed activity: %v", err)
	}

	bad := []struct {
		name string
		args string
	}{
		{"bad type", `{"space_id":"sandbox","activity_id":"a-1","type":"pondering"}`},
		{"bad date", `{"space_id":"sandbox","activity_id":"a-1","date":"yesterday"}`},
		{"empty title", `{"space_id":"sandbox","activity_id":"a-1","title":" "}`},
		{"negative effort", `{"space_id":"sandbox","activity_id":"a-1","effort_hours":-1}`},
		{"blank project", `{"space_id":"sandbox","activity_id":"a-1","project_id":""}`},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			if _, isErr := f.callTool(t, f.rw, "update_activity", tc.args); !isErr {
				t.Error("should be isError")
			}
		})
	}

	text, isErr := f.callTool(t, f.rw, "update_activity",
		`{"space_id":"sandbox","activity_id":"a-1","title":"corrected","type":"decision","date":"2026-07-27"}`)
	if isErr {
		t.Fatalf("update_activity: %s", text)
	}
	var got struct {
		Activity struct {
			Title       string   `json:"title"`
			Type        string   `json:"type"`
			Date        string   `json:"date"`
			EffortHours *float64 `json:"effortHours"`
		} `json:"activity"`
	}
	parseToolJSON(t, text, &got)
	if got.Activity.Title != "corrected" || got.Activity.Type != "decision" || got.Activity.Date != "2026-07-27" {
		t.Errorf("updated activity = %+v", got.Activity)
	}
	if got.Activity.EffortHours == nil || *got.Activity.EffortHours != 2 {
		t.Errorf("effort should be untouched at 2, got %v", got.Activity.EffortHours)
	}

	// Zero effort clears the optional column rather than storing 0.
	if _, isErr := f.callTool(t, f.rw, "update_activity", `{"space_id":"sandbox","activity_id":"a-1","effort_hours":0}`); isErr {
		t.Fatal("clearing effort should succeed")
	}
	act, err := f.spaces.GetActivity(ctx, "sandbox", "a-1")
	if err != nil {
		t.Fatalf("get activity: %v", err)
	}
	if act.EffortHours != nil {
		t.Errorf("effortHours should clear to nil, got %v", *act.EffortHours)
	}
}

func TestUpdateReminder(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.spaces.CreateReminder(ctx, "sandbox", store.Reminder{
		ID: "r-1", Text: "old", RemindAt: "2026-07-30T09:00:00",
	}); err != nil {
		t.Fatalf("seed reminder: %v", err)
	}

	if _, isErr := f.callTool(t, f.rw, "update_reminder", `{"space_id":"sandbox","reminder_id":"r-1","remind_at":"soon"}`); !isErr {
		t.Error("bad remind_at should be isError")
	}
	if _, isErr := f.callTool(t, f.rw, "update_reminder", `{"space_id":"sandbox","reminder_id":"r-1","text":""}`); !isErr {
		t.Error("empty text should be isError")
	}

	text, isErr := f.callTool(t, f.rw, "update_reminder",
		`{"space_id":"sandbox","reminder_id":"r-1","text":"new","remind_at":"2026-08-05T14:30:00","done":true}`)
	if isErr {
		t.Fatalf("update_reminder: %s", text)
	}
	var got struct {
		Reminder struct {
			Text     string `json:"text"`
			RemindAt string `json:"remindAt"`
			Done     *bool  `json:"done"`
		} `json:"reminder"`
	}
	parseToolJSON(t, text, &got)
	if got.Reminder.Text != "new" || got.Reminder.RemindAt != "2026-08-05T14:30:00" {
		t.Errorf("updated reminder = %+v", got.Reminder)
	}
	if got.Reminder.Done == nil || !*got.Reminder.Done {
		t.Errorf("done should be true, got %v", got.Reminder.Done)
	}

	// And back to not-done.
	if _, isErr := f.callTool(t, f.rw, "update_reminder", `{"space_id":"sandbox","reminder_id":"r-1","done":false}`); isErr {
		t.Fatal("unsetting done should succeed")
	}
	rem, err := f.spaces.GetReminder(ctx, "sandbox", "r-1")
	if err != nil {
		t.Fatalf("get reminder: %v", err)
	}
	if rem.Done != nil && *rem.Done {
		t.Error("done should be false")
	}
}

func TestUpdateProjectDescriptiveFields(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	if _, isErr := f.callTool(t, f.rw, "update_project", `{"space_id":"sandbox","project_id":"loom","color":"puce"}`); !isErr {
		t.Error("bad color should be isError")
	}
	if _, isErr := f.callTool(t, f.rw, "update_project", `{"space_id":"sandbox","project_id":"loom","name":"  "}`); !isErr {
		t.Error("blank name should be isError")
	}

	text, isErr := f.callTool(t, f.rw, "update_project",
		`{"space_id":"sandbox","project_id":"loom","name":"Loom v2","purpose":"weave","outcome":"woven","color":"violet","tags":["a","b"]}`)
	if isErr {
		t.Fatalf("update_project: %s", text)
	}
	var got struct {
		Project struct {
			Name       string   `json:"name"`
			Purpose    string   `json:"purpose"`
			Outcome    string   `json:"outcome"`
			Color      string   `json:"color"`
			Tags       []string `json:"tags"`
			NextAction string   `json:"nextAction"`
		} `json:"project"`
	}
	parseToolJSON(t, text, &got)
	if got.Project.Name != "Loom v2" || got.Project.Purpose != "weave" ||
		got.Project.Outcome != "woven" || got.Project.Color != "violet" || len(got.Project.Tags) != 2 {
		t.Errorf("updated project = %+v", got.Project)
	}
	// Designations the call did not touch survive.
	if got.Project.NextAction != "na" {
		t.Errorf("nextAction should be preserved, got %q", got.Project.NextAction)
	}
}

// ─── WRITE tools: dismiss and delete ──────────────────────────────────────

func TestDismissInboxItem(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	id := f.seedInbox(t, "never mind")

	text, isErr := f.callTool(t, f.rw, "dismiss_inbox_item",
		`{"space_id":"sandbox","inbox_id":"`+id+`"}`)
	if isErr {
		t.Fatalf("dismiss: %s", text)
	}
	var got struct {
		InboxItem struct {
			Status string `json:"status"`
		} `json:"inboxItem"`
	}
	parseToolJSON(t, text, &got)
	if got.InboxItem.Status != "dismissed" {
		t.Errorf("status = %q, want dismissed", got.InboxItem.Status)
	}

	// Dismissing twice explains itself rather than reporting a generic error.
	text, isErr = f.callTool(t, f.rw, "dismiss_inbox_item", `{"space_id":"sandbox","inbox_id":"`+id+`"}`)
	if !isErr {
		t.Fatal("dismissing an already-dismissed item should be isError")
	}
	if !strings.Contains(text, "already dismissed") {
		t.Errorf("message should name the current status, got %q", text)
	}

	if text, isErr := f.callTool(t, f.rw, "dismiss_inbox_item", `{"space_id":"sandbox","inbox_id":"ghost"}`); !isErr || !strings.Contains(text, "not found") {
		t.Errorf("unknown inbox item: isErr=%v text=%s", isErr, text)
	}
}

func TestDeleteItem(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.spaces.CreateTask(ctx, "sandbox", store.TaskItem{ID: "t-1", Title: "t", Status: "open", CreatedAt: "2026-07-26"}); err != nil {
		t.Fatalf("seed task: %v", err)
	}
	if _, err := f.spaces.CreateNote(ctx, "sandbox", store.NoteItem{ID: "n-1", Title: "n", Body: "b", CreatedAt: "2026-07-26"}); err != nil {
		t.Fatalf("seed note: %v", err)
	}
	if _, err := f.spaces.CreateReminder(ctx, "sandbox", store.Reminder{ID: "r-1", Text: "r", RemindAt: "2026-07-30T09:00:00"}); err != nil {
		t.Fatalf("seed reminder: %v", err)
	}
	if _, err := f.spaces.CreateActivity(ctx, "sandbox", store.ActivityEntry{
		ID: "a-1", ProjectID: "loom", Date: "2026-07-26", Type: "work", Title: "a",
		Source: "manual", Tags: []string{}, Links: []store.ActivityLink{},
	}); err != nil {
		t.Fatalf("seed activity: %v", err)
	}
	inboxID := f.seedInbox(t, "junk")

	tests := []struct {
		kind   string
		id     string
		exists func() error
	}{
		{"task", "t-1", func() error { _, err := f.spaces.GetTask(ctx, "sandbox", "t-1"); return err }},
		{"note", "n-1", func() error { _, err := f.spaces.GetNote(ctx, "sandbox", "n-1"); return err }},
		{"reminder", "r-1", func() error { _, err := f.spaces.GetReminder(ctx, "sandbox", "r-1"); return err }},
		{"activity", "a-1", func() error { _, err := f.spaces.GetActivity(ctx, "sandbox", "a-1"); return err }},
		{"inbox_item", inboxID, func() error { _, err := f.spaces.GetInboxItem(ctx, "sandbox", inboxID); return err }},
	}
	for _, tc := range tests {
		t.Run(tc.kind, func(t *testing.T) {
			if err := tc.exists(); err != nil {
				t.Fatalf("precondition: %s should exist first: %v", tc.kind, err)
			}
			text, isErr := f.callTool(t, f.rw, "delete_item",
				`{"space_id":"sandbox","kind":"`+tc.kind+`","item_id":"`+tc.id+`"}`)
			if isErr {
				t.Fatalf("delete %s: %s", tc.kind, text)
			}
			if err := tc.exists(); !errors.Is(err, store.ErrNotFound) {
				t.Errorf("%s should be gone, got err=%v", tc.kind, err)
			}
			// Deleting it again reports not-found rather than succeeding.
			if _, isErr := f.callTool(t, f.rw, "delete_item",
				`{"space_id":"sandbox","kind":"`+tc.kind+`","item_id":"`+tc.id+`"}`); !isErr {
				t.Errorf("second delete of %s should be isError", tc.kind)
			}
		})
	}
}

func TestDeleteItemRefusesProject(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	text, isErr := f.callTool(t, f.rw, "delete_item", `{"space_id":"sandbox","kind":"project","item_id":"loom"}`)
	if !isErr {
		t.Fatal("deleting a project should be isError")
	}
	if !strings.Contains(text, "cascades") || !strings.Contains(text, "web app") {
		t.Errorf("refusal should explain why and where to go instead, got %q", text)
	}
	// And the project is untouched.
	if _, err := f.spaces.GetProject(context.Background(), "sandbox", "loom"); err != nil {
		t.Errorf("project should survive a refused delete: %v", err)
	}
}

// ─── Scope and ownership across the new surface ───────────────────────────

// The new write tools must be gated exactly like the original ones: absent
// for read-only tokens, and refused for spaces the caller does not own.
func TestNewWriteToolsGated(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	newWrites := []struct {
		name string
		args string
	}{
		{"create_project", `{"space_id":"private","name":"x"}`},
		{"update_task", `{"space_id":"private","task_id":"t-1","title":"x"}`},
		{"update_note", `{"space_id":"private","note_id":"n-1","body":"x"}`},
		{"update_activity", `{"space_id":"private","activity_id":"a-1","title":"x"}`},
		{"update_reminder", `{"space_id":"private","reminder_id":"r-1","text":"x"}`},
		{"dismiss_inbox_item", `{"space_id":"private","inbox_id":"i-1"}`},
		{"delete_item", `{"space_id":"private","kind":"task","item_id":"t-1"}`},
		{"convert_note", `{"space_id":"private","note_id":"n-1","kind":"task"}`},
	}

	listed := listToolNames(t, f.ro)
	for _, tc := range newWrites {
		t.Run(tc.name, func(t *testing.T) {
			// Not offered to a read-only token.
			for _, n := range listed {
				if n == tc.name {
					t.Errorf("%s should not be listed for read_only", tc.name)
				}
			}
			// Foreign space is indistinguishable from a missing one.
			text, isErr := f.callTool(t, f.rw, tc.name, tc.args)
			if !isErr || !strings.Contains(text, "space not found") {
				t.Errorf("foreign space: isErr=%v text=%s", isErr, text)
			}
		})
	}
}
