package mcp

import (
	"context"
	"encoding/json"
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
		{"get_space_overview", `{"space_id":"nope"}`},
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

func TestWriteToolsRejectArchivedSpace(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	if _, err := f.core.SetSpaceArchived(context.Background(), "sandbox", true); err != nil {
		t.Fatalf("archive: %v", err)
	}
	text, isErr := f.callTool(t, f.rw, "capture_to_inbox", `{"space_id":"sandbox","text":"x"}`)
	if !isErr || !strings.Contains(text, "archived") {
		t.Errorf("write into archived space: isErr=%v text=%s", isErr, text)
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
