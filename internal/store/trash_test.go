package store

import (
	"context"
	"errors"
	"testing"
)

// seedOne puts one of each entity in the space, unattached, so trash behaviour
// can be exercised without the project cascade in the way.
func seedTrashFixture(t *testing.T, s *SpaceStore) {
	t.Helper()
	ctx := context.Background()
	if _, err := s.CreateTask(ctx, testSpace, TaskItem{
		ID: "t-1", Title: "a task", Status: "open", CreatedAt: "2026-08-01",
	}); err != nil {
		t.Fatalf("task: %v", err)
	}
	if _, err := s.CreateNote(ctx, testSpace, NoteItem{
		ID: "n-1", Title: "a note", Body: "b", CreatedAt: "2026-08-01",
	}); err != nil {
		t.Fatalf("note: %v", err)
	}
	if _, err := s.CreateReminder(ctx, testSpace, Reminder{
		ID: "r-1", Text: "a reminder", RemindAt: "2026-08-20T09:00:00",
	}); err != nil {
		t.Fatalf("reminder: %v", err)
	}
	if _, err := s.CreateInboxItem(ctx, testSpace, InboxItem{
		ID: "i-1", Raw: "a capture", CapturedAt: "2026-08-01T09:00:00",
		SuggestedKind: "task", Status: "pending",
	}); err != nil {
		t.Fatalf("inbox: %v", err)
	}
}

// The core promise: a deleted row leaves every read but is still there, and
// comes back unchanged.
func TestDeleteHidesAndRestoreReturns(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, tc := range []struct {
		name, entity, id string
		del              func(s *SpaceStore) error
		live             func(s *SpaceStore) int
	}{
		{"task", TrashTask, "t-1",
			func(s *SpaceStore) error { return s.DeleteTask(ctx, testSpace, "t-1") },
			func(s *SpaceStore) int { v, _ := s.ListTasks(ctx, testSpace); return len(v) }},
		{"note", TrashNote, "n-1",
			func(s *SpaceStore) error { return s.DeleteNote(ctx, testSpace, "n-1") },
			func(s *SpaceStore) int { v, _ := s.ListNotes(ctx, testSpace); return len(v) }},
		{"reminder", TrashReminder, "r-1",
			func(s *SpaceStore) error { return s.DeleteReminder(ctx, testSpace, "r-1") },
			func(s *SpaceStore) int { v, _ := s.ListReminders(ctx, testSpace); return len(v) }},
		{"inbox item", TrashInbox, "i-1",
			func(s *SpaceStore) error { return s.DeleteInboxItem(ctx, testSpace, "i-1") },
			func(s *SpaceStore) int { v, _ := s.ListInboxItems(ctx, testSpace); return len(v) }},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := newTestSpaceStore(t)
			seedTrashFixture(t, s)
			if got := tc.live(s); got != 1 {
				t.Fatalf("expected 1 live %s to start, got %d", tc.name, got)
			}
			if err := tc.del(s); err != nil {
				t.Fatalf("delete: %v", err)
			}
			if got := tc.live(s); got != 0 {
				t.Errorf("%s still in reads after delete: %d live", tc.name, got)
			}
			trash, err := s.ListTrash(ctx, testSpace)
			if err != nil {
				t.Fatalf("list trash: %v", err)
			}
			if len(trash) != 1 || trash[0].ID != tc.id || trash[0].Entity != tc.entity {
				t.Fatalf("trash = %+v, want the one %s", trash, tc.name)
			}
			if trash[0].Label == "" {
				t.Error("trash entry has no label, so the view cannot say what it was")
			}
			if trash[0].BatchSize != 1 {
				t.Errorf("batch size = %d, want 1 for a row deleted on its own", trash[0].BatchSize)
			}
			// Deleting it again is the caller acting on a stale view.
			if err := tc.del(s); !errors.Is(err, ErrNotFound) {
				t.Errorf("second delete: err = %v, want ErrNotFound", err)
			}
			if _, err := s.RestoreItem(ctx, testSpace, tc.entity, tc.id); err != nil {
				t.Fatalf("restore: %v", err)
			}
			if got := tc.live(s); got != 1 {
				t.Errorf("%s not back in reads after restore: %d live", tc.name, got)
			}
			if trash, _ := s.ListTrash(ctx, testSpace); len(trash) != 0 {
				t.Errorf("trash still holds %+v after restore", trash)
			}
		})
	}
}

// Retention: the sweep removes what is older than the cutoff and leaves the
// rest, which is the whole difference between a trash and a delete.
func TestPurgeExpiredOnlyTakesTheOldEnough(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestSpaceStore(t)
	seedTrashFixture(t, s)

	if err := s.DeleteTask(ctx, testSpace, "t-1"); err != nil {
		t.Fatalf("delete task: %v", err)
	}
	if err := s.DeleteNote(ctx, testSpace, "n-1"); err != nil {
		t.Fatalf("delete note: %v", err)
	}
	// Age the task's deletion past the cutoff, leaving the note recent. The
	// fixed clock makes both identical otherwise, which would let a sweep
	// that ignores its cutoff pass.
	db, err := s.db(ctx, testSpace)
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET deleted_at = '2026-01-01T00:00:00Z' WHERE id = 't-1'`); err != nil {
		t.Fatalf("age the task: %v", err)
	}

	n, err := s.PurgeExpired(ctx, testSpace, "2026-06-01T00:00:00Z")
	if err != nil {
		t.Fatalf("PurgeExpired: %v", err)
	}
	if n != 1 {
		t.Errorf("purged %d rows, want just the aged one", n)
	}
	trash, err := s.ListTrash(ctx, testSpace)
	if err != nil {
		t.Fatalf("list trash: %v", err)
	}
	if len(trash) != 1 || trash[0].ID != "n-1" {
		t.Errorf("trash = %+v, want the recent note still there", trash)
	}
	// The aged one is really gone, not merely hidden.
	if _, err := s.RestoreItem(ctx, testSpace, TrashTask, "t-1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("restore after purge: err = %v, want ErrNotFound", err)
	}
}

func TestEmptyTrashRemovesEverythingTrashedAndNothingLive(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestSpaceStore(t)
	seedTrashFixture(t, s)

	if err := s.DeleteTask(ctx, testSpace, "t-1"); err != nil {
		t.Fatalf("delete task: %v", err)
	}
	if err := s.DeleteNote(ctx, testSpace, "n-1"); err != nil {
		t.Fatalf("delete note: %v", err)
	}
	n, err := s.EmptyTrash(ctx, testSpace)
	if err != nil {
		t.Fatalf("EmptyTrash: %v", err)
	}
	if n != 2 {
		t.Errorf("emptied %d rows, want 2", n)
	}
	if trash, _ := s.ListTrash(ctx, testSpace); len(trash) != 0 {
		t.Errorf("trash = %+v after emptying", trash)
	}
	// The live rows are untouched — an empty-trash that took them too would
	// be indistinguishable from one that worked, on counts alone.
	rems, err := s.ListReminders(ctx, testSpace)
	if err != nil {
		t.Fatalf("list reminders: %v", err)
	}
	if len(rems) != 1 {
		t.Errorf("live reminders = %d, want 1 untouched", len(rems))
	}
	inbox, err := s.ListInboxItems(ctx, testSpace)
	if err != nil {
		t.Fatalf("list inbox: %v", err)
	}
	if len(inbox) != 1 {
		t.Errorf("live inbox items = %d, want 1 untouched", len(inbox))
	}
}

// A restore names one row but acts on its batch, so asking for a row that was
// never trashed has to fail rather than silently do nothing.
func TestRestoreAndPurgeRejectUnknownAndLiveRows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestSpaceStore(t)
	seedTrashFixture(t, s)

	if _, err := s.RestoreItem(ctx, testSpace, TrashTask, "t-1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("restore a live row: err = %v, want ErrNotFound", err)
	}
	if _, err := s.PurgeItem(ctx, testSpace, TrashTask, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("purge an unknown row: err = %v, want ErrNotFound", err)
	}
	if _, err := s.RestoreItem(ctx, testSpace, "sandwich", "t-1"); err == nil {
		t.Error("restore with an unknown entity should be refused")
	}
	// And nothing was disturbed by any of that.
	if tasks, _ := s.ListTasks(ctx, testSpace); len(tasks) != 1 {
		t.Errorf("live tasks = %d, want 1", len(tasks))
	}
}

// The listing is one entry per delete. A per-row listing was the obvious
// first shape and it put a project's whole cascade on screen, the project
// buried among it, with an identical Restore on every row.
func TestListTrashIsOnePerDeleteNotPerRow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestSpaceStore(t)
	seedCascadeFixture(t, s)

	if _, err := s.SoftDeleteProject(ctx, testSpace, "loom"); err != nil {
		t.Fatalf("delete project: %v", err)
	}
	if err := s.DeleteReminder(ctx, testSpace, "rem-k-1"); err != nil {
		t.Fatalf("delete reminder: %v", err)
	}

	trash, err := s.ListTrash(ctx, testSpace)
	if err != nil {
		t.Fatalf("list trash: %v", err)
	}
	if len(trash) != 2 {
		t.Fatalf("trash = %+v, want two entries: the project delete and the reminder", trash)
	}
	var project, reminder *TrashItem
	for i := range trash {
		switch trash[i].Entity {
		case TrashProject:
			project = &trash[i]
		case TrashReminder:
			reminder = &trash[i]
		}
	}
	if project == nil {
		t.Fatalf("no project entry: %+v", trash)
	}
	if project.ID != "loom" || project.Label != "loom" {
		t.Errorf("project entry = %+v, want it described by the project itself", *project)
	}
	// 1 project + 3 activities + 2 tasks + 1 note.
	if project.BatchSize != 7 {
		t.Errorf("batchSize = %d, want 7", project.BatchSize)
	}
	if reminder == nil || reminder.BatchSize != 1 {
		t.Errorf("reminder entry = %+v, want its own batch of 1", reminder)
	}
}
