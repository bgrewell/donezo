package store

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// seedCascadeFixture populates testSpace for cascade tests: project
// "loom" owns 3 activities, 2 tasks, and 1 note, and is loosely
// referenced by 1 inbox suggestion and 1 reminder; project "keep" owns
// one of each so cross-project isolation is assertable; project "bare"
// has no references at all. One extra inbox item suggests the
// never-created project "ghost" (suggestions carry no foreign key).
func seedCascadeFixture(t *testing.T, s *SpaceStore) {
	t.Helper()
	ctx := context.Background()
	for _, id := range []string{"loom", "keep", "bare"} {
		mustCreateProject(t, s, id)
	}
	type ref struct{ id, project string }
	for _, a := range []ref{
		{"act-l-1", "loom"}, {"act-l-2", "loom"}, {"act-l-3", "loom"}, {"act-k-1", "keep"},
	} {
		if _, err := s.CreateActivity(ctx, testSpace, ActivityEntry{
			ID: a.id, ProjectID: a.project, Date: "2026-07-20", Type: "work",
			Title: "t", Details: "d", Source: "manual",
		}); err != nil {
			t.Fatalf("create activity %s: %v", a.id, err)
		}
	}
	for _, tk := range []ref{{"tsk-l-1", "loom"}, {"tsk-l-2", "loom"}, {"tsk-k-1", "keep"}} {
		if _, err := s.CreateTask(ctx, testSpace, TaskItem{
			ID: tk.id, ProjectID: ptr(tk.project), Title: "t", Status: "open", CreatedAt: "2026-07-01",
		}); err != nil {
			t.Fatalf("create task %s: %v", tk.id, err)
		}
	}
	for _, n := range []ref{{"note-l-1", "loom"}, {"note-k-1", "keep"}} {
		if _, err := s.CreateNote(ctx, testSpace, NoteItem{
			ID: n.id, ProjectID: ptr(n.project), Title: "t", Body: "b", CreatedAt: "2026-07-01",
		}); err != nil {
			t.Fatalf("create note %s: %v", n.id, err)
		}
	}
	for _, ib := range []ref{{"inb-l-1", "loom"}, {"inb-k-1", "keep"}, {"inb-ghost", "ghost"}} {
		if _, err := s.CreateInboxItem(ctx, testSpace, InboxItem{
			ID: ib.id, Raw: "raw " + ib.id, CapturedAt: "2026-07-25T08:30:00",
			SuggestedKind: "task", SuggestedProjectID: ptr(ib.project), Status: "pending",
		}); err != nil {
			t.Fatalf("create inbox item %s: %v", ib.id, err)
		}
	}
	for _, r := range []ref{{"rem-l-1", "loom"}, {"rem-k-1", "keep"}} {
		if _, err := s.CreateReminder(ctx, testSpace, Reminder{
			ID: r.id, Text: "t", RemindAt: "2026-07-27T09:00:00", ProjectID: ptr(r.project),
		}); err != nil {
			t.Fatalf("create reminder %s: %v", r.id, err)
		}
	}
}

// idsOf collects entity ids via the given accessor, for order-stable
// list assertions.
func idsOf[T any](items []T, id func(T) string) []string {
	out := []string{}
	for _, it := range items {
		out = append(out, id(it))
	}
	return out
}

// mustState loads the full space state for post-cascade assertions.
func mustState(t *testing.T, s *SpaceStore) SpaceState {
	t.Helper()
	st, err := s.State(context.Background(), testSpace)
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	return st
}

// Deleting a project now moves it and the content it owns to the trash as one
// batch. These cover the three things that changes: what disappears from
// reads, what is deliberately left alone, and that a restore brings back
// exactly this delete and nothing else.
func TestSoftDeleteProjectHidesOwnedContent(t *testing.T) {
	t.Parallel()
	s := newTestSpaceStore(t)
	seedCascadeFixture(t, s)
	ctx := context.Background()

	got, err := s.SoftDeleteProject(ctx, testSpace, "loom")
	if err != nil {
		t.Fatalf("SoftDeleteProject: %v", err)
	}
	if want := (ProjectCascadeResult{Project: 1, Activities: 3, Tasks: 2, Notes: 1}); got != want {
		t.Errorf("counts = %+v, want %+v", got, want)
	}

	if _, err := s.GetProject(ctx, testSpace, "loom"); !errors.Is(err, ErrNotFound) {
		t.Errorf("trashed project still readable: err = %v, want ErrNotFound", err)
	}
	st := mustState(t, s)
	if ids := idsOf(st.Projects, func(p Project) string { return p.ID }); !reflect.DeepEqual(ids, []string{"keep", "bare"}) {
		t.Errorf("projects = %v, want [keep bare]", ids)
	}
	if ids := idsOf(st.Activities, func(a ActivityEntry) string { return a.ID }); !reflect.DeepEqual(ids, []string{"act-k-1"}) {
		t.Errorf("activities = %v, want only keep's", ids)
	}
	if ids := idsOf(st.Tasks, func(tk TaskItem) string { return tk.ID }); !reflect.DeepEqual(ids, []string{"tsk-k-1"}) {
		t.Errorf("tasks = %v, want only keep's", ids)
	}
	if ids := idsOf(st.Notes, func(n NoteItem) string { return n.ID }); !reflect.DeepEqual(ids, []string{"note-k-1"}) {
		t.Errorf("notes = %v, want only keep's", ids)
	}

	// The deliberate difference from the old hard cascade: loose references
	// are NOT detached. The project row still exists for them to point at, so
	// the link survives and comes back with a restore. They read as unfiled
	// meanwhile only because the project is filtered out of reads.
	for _, r := range st.Reminders {
		if r.ID == "rem-l-1" {
			if r.ProjectID == nil || *r.ProjectID != "loom" {
				t.Errorf("rem-l-1 projectId = %v, want it kept — a soft delete detaches nothing", r.ProjectID)
			}
		}
	}
	for _, it := range st.Inbox {
		if it.ID == "inb-l-1" {
			if it.SuggestedProjectID == nil || *it.SuggestedProjectID != "loom" {
				t.Errorf("inb-l-1 suggestedProjectId = %v, want it kept", it.SuggestedProjectID)
			}
		}
	}
	if ids := idsOf(st.Reminders, func(r Reminder) string { return r.ID }); !reflect.DeepEqual(ids, []string{"rem-l-1", "rem-k-1"}) {
		t.Errorf("reminders = %v, want both still live", ids)
	}
}

// Restore has to bring back exactly the batch — not a row the person had
// deleted separately beforehand, which is the whole reason a batch exists.
func TestRestoreProjectBringsBackOnlyItsOwnBatch(t *testing.T) {
	t.Parallel()
	s := newTestSpaceStore(t)
	seedCascadeFixture(t, s)
	ctx := context.Background()

	// Deleted on its own, a week earlier as far as the batch is concerned.
	if err := s.DeleteTask(ctx, testSpace, "tsk-l-1"); err != nil {
		t.Fatalf("delete task first: %v", err)
	}
	if _, err := s.SoftDeleteProject(ctx, testSpace, "loom"); err != nil {
		t.Fatalf("SoftDeleteProject: %v", err)
	}

	if _, err := s.RestoreItem(ctx, testSpace, TrashProject, "loom"); err != nil {
		t.Fatalf("RestoreItem: %v", err)
	}
	st := mustState(t, s)
	if _, err := s.GetProject(ctx, testSpace, "loom"); err != nil {
		t.Errorf("project not restored: %v", err)
	}
	ids := idsOf(st.Tasks, func(tk TaskItem) string { return tk.ID })
	for _, id := range ids {
		if id == "tsk-l-1" {
			t.Error("restoring the project resurrected a task deleted separately beforehand")
		}
	}
	// Everything the project delete took, though, is back.
	if len(idsOf(st.Activities, func(a ActivityEntry) string { return a.ID })) != 4 {
		t.Errorf("activities = %v, want all four back", st.Activities)
	}
	// And the loose references still point where they did.
	for _, r := range st.Reminders {
		if r.ID == "rem-l-1" && (r.ProjectID == nil || *r.ProjectID != "loom") {
			t.Errorf("rem-l-1 lost its project across delete and restore: %v", r.ProjectID)
		}
	}
}

// Purge is the only thing that removes data — and the point at which the
// detach the soft cascade skipped finally has to happen, or the foreign key
// fails as the project row goes.
func TestPurgeProjectDetachesThenRemoves(t *testing.T) {
	t.Parallel()
	s := newTestSpaceStore(t)
	seedCascadeFixture(t, s)
	ctx := context.Background()

	if _, err := s.SoftDeleteProject(ctx, testSpace, "loom"); err != nil {
		t.Fatalf("SoftDeleteProject: %v", err)
	}
	n, err := s.PurgeItem(ctx, testSpace, TrashProject, "loom")
	if err != nil {
		t.Fatalf("PurgeItem: %v", err)
	}
	if n != 7 { // project + 3 activities + 2 tasks + 1 note
		t.Errorf("purged %d rows, want 7", n)
	}

	st := mustState(t, s)
	// The loose references survive the purge, detached — a reminder's text
	// stands on its own, which is the distinction the hard cascade drew too.
	for _, r := range st.Reminders {
		if r.ID == "rem-l-1" && r.ProjectID != nil {
			t.Errorf("rem-l-1 projectId = %q after purge, want detached", *r.ProjectID)
		}
		if r.ID == "rem-k-1" && (r.ProjectID == nil || *r.ProjectID != "keep") {
			t.Errorf("rem-k-1 was disturbed: %v", r.ProjectID)
		}
	}
	for _, it := range st.Inbox {
		if it.ID == "inb-l-1" && it.SuggestedProjectID != nil {
			t.Errorf("inb-l-1 suggestedProjectId = %q after purge, want detached", *it.SuggestedProjectID)
		}
		if it.ID == "inb-k-1" && (it.SuggestedProjectID == nil || *it.SuggestedProjectID != "keep") {
			t.Errorf("inb-k-1 was disturbed: %v", it.SuggestedProjectID)
		}
	}
	if ids := idsOf(st.Reminders, func(r Reminder) string { return r.ID }); !reflect.DeepEqual(ids, []string{"rem-l-1", "rem-k-1"}) {
		t.Errorf("reminders = %v, want both kept", ids)
	}
	// And it is really gone — restoring it is no longer possible.
	if _, err := s.RestoreItem(ctx, testSpace, TrashProject, "loom"); !errors.Is(err, ErrNotFound) {
		t.Errorf("restore after purge: err = %v, want ErrNotFound", err)
	}
}
