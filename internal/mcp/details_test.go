package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/bgrewell/donezo/internal/store"
)

// #44: tasks and reminders carry an optional details field, and the paths that
// turn long text into one of them now split rather than pour everything into
// the title.

func TestCreateTaskAndReminderCarryDetails(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()

	if text, isErr := f.callTool(t, f.rw, "create_task",
		`{"space_id":"sandbox","title":"Rotate the PATs","details":"hiera-eyaml, 90-day max.\nSee the runbook."}`); isErr {
		t.Fatalf("create_task: %s", text)
	}
	tasks, err := f.spaces.ListTasks(ctx, "sandbox")
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("tasks = %d, want 1", len(tasks))
	}
	if tasks[0].Title != "Rotate the PATs" {
		t.Errorf("title = %q", tasks[0].Title)
	}
	if tasks[0].Details != "hiera-eyaml, 90-day max.\nSee the runbook." {
		t.Errorf("details = %q", tasks[0].Details)
	}

	if text, isErr := f.callTool(t, f.rw, "create_reminder",
		`{"space_id":"sandbox","text":"Ping Dan","details":"About the RAN550 licence.","remind_at":"2026-08-20T09:00:00"}`); isErr {
		t.Fatalf("create_reminder: %s", text)
	}
	rems, err := f.spaces.ListReminders(ctx, "sandbox")
	if err != nil {
		t.Fatalf("list reminders: %v", err)
	}
	if len(rems) != 1 || rems[0].Details != "About the RAN550 licence." {
		t.Errorf("reminder = %+v", rems)
	}
}

// update_* is how an over-long title gets moved into details after the fact,
// which is the migration path for everything already in the database.
func TestUpdateMovesTitleIntoDetails(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()

	if text, isErr := f.callTool(t, f.rw, "create_task",
		`{"space_id":"sandbox","title":"Everything about the thing, at length, in one line"}`); isErr {
		t.Fatalf("create_task: %s", text)
	}
	tasks, err := f.spaces.ListTasks(ctx, "sandbox")
	if err != nil || len(tasks) != 1 {
		t.Fatalf("list tasks: %+v (err %v)", tasks, err)
	}
	id := tasks[0].ID

	if text, isErr := f.callTool(t, f.rw, "update_task",
		`{"space_id":"sandbox","task_id":"`+id+`","title":"The thing","details":"Everything about it, at length."}`); isErr {
		t.Fatalf("update_task: %s", text)
	}
	got, err := f.spaces.GetTask(ctx, "sandbox", id)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if got.Title != "The thing" || got.Details != "Everything about it, at length." {
		t.Errorf("task = %+v", got)
	}

	// And details can be cleared again with an empty string.
	if text, isErr := f.callTool(t, f.rw, "update_task",
		`{"space_id":"sandbox","task_id":"`+id+`","details":""}`); isErr {
		t.Fatalf("clear details: %s", text)
	}
	if after, err := f.spaces.GetTask(ctx, "sandbox", id); err != nil || after.Details != "" {
		t.Errorf("details = %q after clearing (err %v)", after.Details, err)
	}
}

// A capture with a first line and a body should not become a paragraph-long
// title. The split is on the newline the person actually typed.
func TestClassifySplitsMultiLineCaptures(t *testing.T) {
	t.Parallel()
	const raw = "Dig into CORE and decide whether to continue\n\nOnly the eval context has real content. " +
		"Everything under internal/manifest is a stub."

	t.Run("task", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		f.seedInbox(t, raw)
		if text, isErr := f.callTool(t, f.rw, "classify_inbox_item",
			`{"space_id":"sandbox","inbox_id":"inb-seed","kind":"task"}`); isErr {
			t.Fatalf("classify: %s", text)
		}
		tasks, err := f.spaces.ListTasks(context.Background(), "sandbox")
		if err != nil || len(tasks) != 1 {
			t.Fatalf("tasks = %+v (err %v)", tasks, err)
		}
		if tasks[0].Title != "Dig into CORE and decide whether to continue" {
			t.Errorf("title = %q, want just the first line", tasks[0].Title)
		}
		if !strings.HasPrefix(tasks[0].Details, "Only the eval context") {
			t.Errorf("details = %q, want the rest of the capture", tasks[0].Details)
		}
	})

	t.Run("activity", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		f.seedInbox(t, raw)
		if text, isErr := f.callTool(t, f.rw, "classify_inbox_item",
			`{"space_id":"sandbox","inbox_id":"inb-seed","kind":"activity","project_id":"loom"}`); isErr {
			t.Fatalf("classify: %s", text)
		}
		acts, err := f.spaces.ListActivities(context.Background(), "sandbox")
		if err != nil || len(acts) != 1 {
			t.Fatalf("activities = %+v (err %v)", acts, err)
		}
		if acts[0].Title != "Dig into CORE and decide whether to continue" {
			t.Errorf("title = %q, want just the first line", acts[0].Title)
		}
		if !strings.HasPrefix(acts[0].Details, "Only the eval context") {
			t.Errorf("details = %q", acts[0].Details)
		}
	})

	// The reminder branch is the twin of the task one and was unpinned: the
	// whole of it could be reverted with every other test still green.
	t.Run("reminder", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		f.seedInbox(t, raw)
		if text, isErr := f.callTool(t, f.rw, "classify_inbox_item",
			`{"space_id":"sandbox","inbox_id":"inb-seed","kind":"reminder","remind_at":"2026-08-20T09:00:00"}`); isErr {
			t.Fatalf("classify: %s", text)
		}
		rems, err := f.spaces.ListReminders(context.Background(), "sandbox")
		if err != nil || len(rems) != 1 {
			t.Fatalf("reminders = %+v (err %v)", rems, err)
		}
		if rems[0].Text != "Dig into CORE and decide whether to continue" {
			t.Errorf("text = %q, want just the first line", rems[0].Text)
		}
		if !strings.HasPrefix(rems[0].Details, "Only the eval context") {
			t.Errorf("details = %q, want the rest of the capture", rems[0].Details)
		}
	})

	// A single line has no break the author gave, so it stays whole rather
	// than being guessed apart at a sentence boundary.
	t.Run("single line is left alone", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		f.seedInbox(t, "One long line. With two sentences.")
		if text, isErr := f.callTool(t, f.rw, "classify_inbox_item",
			`{"space_id":"sandbox","inbox_id":"inb-seed","kind":"task"}`); isErr {
			t.Fatalf("classify: %s", text)
		}
		tasks, err := f.spaces.ListTasks(context.Background(), "sandbox")
		if err != nil || len(tasks) != 1 {
			t.Fatalf("tasks = %+v (err %v)", tasks, err)
		}
		if tasks[0].Title != "One long line. With two sentences." || tasks[0].Details != "" {
			t.Errorf("task = %+v, want the whole line as the title", tasks[0])
		}
	})

	// An explicit title still wins, and the rest still becomes details.
	t.Run("explicit title wins", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		f.seedInbox(t, raw)
		if text, isErr := f.callTool(t, f.rw, "classify_inbox_item",
			`{"space_id":"sandbox","inbox_id":"inb-seed","kind":"task","title":"CORE: decide"}`); isErr {
			t.Fatalf("classify: %s", text)
		}
		tasks, err := f.spaces.ListTasks(context.Background(), "sandbox")
		if err != nil || len(tasks) != 1 {
			t.Fatalf("tasks = %+v (err %v)", tasks, err)
		}
		if tasks[0].Title != "CORE: decide" {
			t.Errorf("title = %q, want the caller's", tasks[0].Title)
		}
		if !strings.HasPrefix(tasks[0].Details, "Only the eval context") {
			t.Errorf("details = %q, want the remainder still carried", tasks[0].Details)
		}
	})
}

// convert_note's reminder target carries the body too — the same gap as the
// split above, on the other conversion path.
func TestConvertNoteCarriesBodyIntoReminderDetails(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.seedNote(t, "n-rbody", "Renew the domain", "Registrar is Gandi; card expires in March.", nil)

	if text, isErr := f.callTool(t, f.rw, "convert_note",
		`{"space_id":"sandbox","note_id":"n-rbody","kind":"reminder","remind_at":"2026-08-20T09:00:00"}`); isErr {
		t.Fatalf("convert_note: %s", text)
	}
	rems, err := f.spaces.ListReminders(context.Background(), "sandbox")
	if err != nil || len(rems) != 1 {
		t.Fatalf("reminders = %+v (err %v)", rems, err)
	}
	if rems[0].Text != "Renew the domain" {
		t.Errorf("text = %q", rems[0].Text)
	}
	if rems[0].Details != "Registrar is Gandi; card expires in March." {
		t.Errorf("details = %q, want the note's body", rems[0].Details)
	}
}

// Search has to reach the field long-form text was just moved into, or
// splitting a title makes it unfindable.
func TestSearchMatchesTaskAndReminderDetails(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.spaces.CreateTask(ctx, "sandbox", store.TaskItem{
		ID: "tsk-s1", Title: "Investigate nightly drops", Status: "open",
		Details: "the upstream connector renegotiates TLS", CreatedAt: "2026-08-09",
	}); err != nil {
		t.Fatalf("seed task: %v", err)
	}
	if _, err := f.spaces.CreateReminder(ctx, "sandbox", store.Reminder{
		ID: "rem-s1", Text: "Renew the domain", Details: "registrar is Gandi",
		RemindAt: "2026-08-20T09:00:00",
	}); err != nil {
		t.Fatalf("seed reminder: %v", err)
	}

	for _, tc := range []struct{ query, want string }{
		{"renegotiates", "tsk-s1"},
		{"Gandi", "rem-s1"},
	} {
		text, isErr := f.callTool(t, f.rw, "search",
			`{"space_id":"sandbox","query":"`+tc.query+`"}`)
		if isErr {
			t.Fatalf("search %q: %s", tc.query, text)
		}
		if !strings.Contains(text, tc.want) {
			t.Errorf("search %q did not find %s — details are not searched: %s", tc.query, tc.want, text)
		}
	}
}

// Converting a note into a task used to destroy the body. It now lands in
// details — the case that made #44 worth doing.
func TestConvertNoteCarriesBodyIntoTaskDetails(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.seedNote(t, "n-body", "Ship the parser rewrite", "Blocked on the lexer; see PR #87.", nil)

	if text, isErr := f.callTool(t, f.rw, "convert_note",
		`{"space_id":"sandbox","note_id":"n-body","kind":"task"}`); isErr {
		t.Fatalf("convert_note: %s", text)
	}
	tasks, err := f.spaces.ListTasks(context.Background(), "sandbox")
	if err != nil || len(tasks) != 1 {
		t.Fatalf("tasks = %+v (err %v)", tasks, err)
	}
	if tasks[0].Title != "Ship the parser rewrite" {
		t.Errorf("title = %q", tasks[0].Title)
	}
	if tasks[0].Details != "Blocked on the lexer; see PR #87." {
		t.Errorf("details = %q, want the note's body", tasks[0].Details)
	}
}
