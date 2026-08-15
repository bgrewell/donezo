package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// newTestSpaceStore builds a SpaceStore in a temp dir with a fixed clock.
func newTestSpaceStore(t *testing.T) *SpaceStore {
	t.Helper()
	s, err := NewSpaceStore(WithDataDir(t.TempDir()), WithClock(fixedClock))
	if err != nil {
		t.Fatalf("NewSpaceStore: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("close space store: %v", err)
		}
	})
	return s
}

// ptr returns a pointer to v, for optional-field fixtures.
func ptr[T any](v T) *T {
	return &v
}

const testSpace = "sandbox"

// mustCreateProject inserts a minimal project other fixtures can
// reference.
func mustCreateProject(t *testing.T, s *SpaceStore, id string) {
	t.Helper()
	_, err := s.CreateProject(context.Background(), testSpace, Project{
		ID: id, Name: id, Color: "blue", Purpose: "p", Outcome: "o",
		CurrentFocus: "cf", NextAction: "na", Status: "active", ResumeContext: "rc",
	})
	if err != nil {
		t.Fatalf("create project %s: %v", id, err)
	}
}

func TestNewSpaceStoreRequiresDataDir(t *testing.T) {
	t.Parallel()
	if _, err := NewSpaceStore(); err == nil {
		t.Fatal("want error without WithDataDir")
	}
}

func TestProjectCRUD(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   Project
		want Project // after create; CreatedAt/UpdatedAt asserted separately
	}{
		{
			name: "all optionals set",
			in: Project{
				ID: "loom", Name: "Loom", Color: "blue", Purpose: "auth", Outcome: "boring",
				CurrentFocus: "v0.9", NextAction: "test", AltNextActions: []string{"a", "b"},
				Status: "waiting", ResumeContext: "ctx", WaitingOn: ptr("Dan"), Tags: []string{"go"},
			},
			want: Project{
				ID: "loom", Name: "Loom", Color: "blue", Purpose: "auth", Outcome: "boring",
				CurrentFocus: "v0.9", NextAction: "test", AltNextActions: []string{"a", "b"},
				Status: "waiting", ResumeContext: "ctx", WaitingOn: ptr("Dan"), Tags: []string{"go"},
			},
		},
		{
			name: "nil optionals normalize to empty",
			in: Project{
				ID: "min", Name: "Min", Color: "steel", Purpose: "p", Outcome: "o",
				CurrentFocus: "cf", NextAction: "na", Status: "active", ResumeContext: "rc",
			},
			want: Project{
				ID: "min", Name: "Min", Color: "steel", Purpose: "p", Outcome: "o",
				CurrentFocus: "cf", NextAction: "na", AltNextActions: []string{},
				Status: "active", ResumeContext: "rc", WaitingOn: nil, Tags: []string{},
			},
		},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newTestSpaceStore(t)
			ctx := context.Background()
			created, err := s.CreateProject(ctx, testSpace, tt.in)
			if err != nil {
				t.Fatalf("CreateProject: %v", err)
			}
			if created.CreatedAt != fixedNow || created.UpdatedAt != fixedNow {
				t.Errorf("timestamps = %q/%q, want %q", created.CreatedAt, created.UpdatedAt, fixedNow)
			}
			got, err := s.GetProject(ctx, testSpace, tt.in.ID)
			if err != nil {
				t.Fatalf("GetProject: %v", err)
			}
			tt.want.CreatedAt, tt.want.UpdatedAt = fixedNow, fixedNow
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("round-trip:\n got %+v\nwant %+v", got, tt.want)
			}

			list, err := s.ListProjects(ctx, testSpace)
			if err != nil {
				t.Fatalf("ListProjects: %v", err)
			}
			if len(list) != 1 || !reflect.DeepEqual(list[0], tt.want) {
				t.Errorf("list = %+v, want [%+v]", list, tt.want)
			}

			updated := got
			updated.Name = "Renamed"
			updated.WaitingOn = nil
			updated.Tags = []string{"new"}
			afterUpdate, err := s.UpdateProject(ctx, testSpace, updated)
			if err != nil {
				t.Fatalf("UpdateProject: %v", err)
			}
			if afterUpdate.Name != "Renamed" || afterUpdate.WaitingOn != nil ||
				!reflect.DeepEqual(afterUpdate.Tags, []string{"new"}) {
				t.Errorf("update round-trip = %+v", afterUpdate)
			}

			if err := s.DeleteProject(ctx, testSpace, tt.in.ID); err != nil {
				t.Fatalf("DeleteProject: %v", err)
			}
			if _, err := s.GetProject(ctx, testSpace, tt.in.ID); !errors.Is(err, ErrNotFound) {
				t.Errorf("after delete err = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestActivityCRUD(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   ActivityEntry
		want ActivityEntry
	}{
		{
			name: "all optionals set",
			in: ActivityEntry{
				ID: "act-1", ProjectID: "loom", Date: "2026-07-20", Type: "work",
				Title: "Fix", Details: "d", EffortHours: ptr(2.5), Source: "manual",
				Tags: []string{"bug"}, Links: []ActivityLink{{Label: "PR", URL: "https://x/1"}},
				NextAction: ptr("ship"), Planned: ptr(true),
			},
			want: ActivityEntry{
				ID: "act-1", ProjectID: "loom", Date: "2026-07-20", Type: "work",
				Title: "Fix", Details: "d", EffortHours: ptr(2.5), Source: "manual",
				Tags: []string{"bug"}, Links: []ActivityLink{{Label: "PR", URL: "https://x/1"}},
				NextAction: ptr("ship"), Planned: ptr(true),
			},
		},
		{
			name: "optionals absent",
			in: ActivityEntry{
				ID: "act-2", ProjectID: "loom", Date: "2026-07-21", Type: "decision",
				Title: "Choose", Details: "d", Source: "capture",
			},
			want: ActivityEntry{
				ID: "act-2", ProjectID: "loom", Date: "2026-07-21", Type: "decision",
				Title: "Choose", Details: "d", EffortHours: nil, Source: "capture",
				Tags: []string{}, Links: []ActivityLink{}, NextAction: nil, Planned: nil,
			},
		},
		{
			name: "planned false is preserved (not dropped like nil)",
			in: ActivityEntry{
				ID: "act-3", ProjectID: "loom", Date: "2026-07-22", Type: "work",
				Title: "T", Details: "d", Source: "manual", Planned: ptr(false),
			},
			want: ActivityEntry{
				ID: "act-3", ProjectID: "loom", Date: "2026-07-22", Type: "work",
				Title: "T", Details: "d", Source: "manual",
				Tags: []string{}, Links: []ActivityLink{}, Planned: ptr(false),
			},
		},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newTestSpaceStore(t)
			ctx := context.Background()
			mustCreateProject(t, s, "loom")
			if _, err := s.CreateActivity(ctx, testSpace, tt.in); err != nil {
				t.Fatalf("CreateActivity: %v", err)
			}
			got, err := s.GetActivity(ctx, testSpace, tt.in.ID)
			if err != nil {
				t.Fatalf("GetActivity: %v", err)
			}
			tt.want.CreatedAt, tt.want.UpdatedAt = fixedNow, fixedNow
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("round-trip:\n got %+v\nwant %+v", got, tt.want)
			}

			got.Title = "Updated"
			got.EffortHours = ptr(1.0)
			afterUpdate, err := s.UpdateActivity(ctx, testSpace, got)
			if err != nil {
				t.Fatalf("UpdateActivity: %v", err)
			}
			if afterUpdate.Title != "Updated" || afterUpdate.EffortHours == nil || *afterUpdate.EffortHours != 1.0 {
				t.Errorf("update round-trip = %+v", afterUpdate)
			}

			if err := s.DeleteActivity(ctx, testSpace, tt.in.ID); err != nil {
				t.Fatalf("DeleteActivity: %v", err)
			}
			if _, err := s.GetActivity(ctx, testSpace, tt.in.ID); !errors.Is(err, ErrNotFound) {
				t.Errorf("after delete err = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestTaskCRUD(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   TaskItem
	}{
		{
			name: "all optionals set",
			in: TaskItem{
				ID: "t-1", ProjectID: ptr("loom"), Title: "Do it", Status: "waiting",
				Due: ptr("2026-08-01"), WaitingOn: ptr("Dan"), CreatedAt: "2026-07-01",
			},
		},
		{
			name: "no project, no due",
			in:   TaskItem{ID: "t-2", Title: "Loose end", Status: "open", CreatedAt: "2026-07-02"},
		},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newTestSpaceStore(t)
			ctx := context.Background()
			mustCreateProject(t, s, "loom")
			if _, err := s.CreateTask(ctx, testSpace, tt.in); err != nil {
				t.Fatalf("CreateTask: %v", err)
			}
			got, err := s.GetTask(ctx, testSpace, tt.in.ID)
			if err != nil {
				t.Fatalf("GetTask: %v", err)
			}
			if !reflect.DeepEqual(got, tt.in) {
				t.Errorf("round-trip:\n got %+v\nwant %+v", got, tt.in)
			}

			got.Status = "done"
			got.Due = nil
			afterUpdate, err := s.UpdateTask(ctx, testSpace, got)
			if err != nil {
				t.Fatalf("UpdateTask: %v", err)
			}
			refetched, err := s.GetTask(ctx, testSpace, tt.in.ID)
			if err != nil {
				t.Fatalf("GetTask after update: %v", err)
			}
			if !reflect.DeepEqual(refetched, afterUpdate) {
				t.Errorf("update round-trip:\n got %+v\nwant %+v", refetched, afterUpdate)
			}
			if refetched.Status != "done" || refetched.Due != nil {
				t.Errorf("update not applied: %+v", refetched)
			}

			if err := s.DeleteTask(ctx, testSpace, tt.in.ID); err != nil {
				t.Fatalf("DeleteTask: %v", err)
			}
			if _, err := s.GetTask(ctx, testSpace, tt.in.ID); !errors.Is(err, ErrNotFound) {
				t.Errorf("after delete err = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestNoteCRUD(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   NoteItem
	}{
		{name: "with project", in: NoteItem{ID: "n-1", ProjectID: ptr("loom"), Title: "T", Body: "B", CreatedAt: "2026-07-01"}},
		{name: "without project", in: NoteItem{ID: "n-2", Title: "T2", Body: "", CreatedAt: "2026-07-02"}},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newTestSpaceStore(t)
			ctx := context.Background()
			mustCreateProject(t, s, "loom")
			if _, err := s.CreateNote(ctx, testSpace, tt.in); err != nil {
				t.Fatalf("CreateNote: %v", err)
			}
			got, err := s.GetNote(ctx, testSpace, tt.in.ID)
			if err != nil {
				t.Fatalf("GetNote: %v", err)
			}
			if !reflect.DeepEqual(got, tt.in) {
				t.Errorf("round-trip:\n got %+v\nwant %+v", got, tt.in)
			}

			got.Body = "updated body"
			if _, err := s.UpdateNote(ctx, testSpace, got); err != nil {
				t.Fatalf("UpdateNote: %v", err)
			}
			refetched, err := s.GetNote(ctx, testSpace, tt.in.ID)
			if err != nil {
				t.Fatalf("GetNote after update: %v", err)
			}
			if refetched.Body != "updated body" {
				t.Errorf("update not applied: %+v", refetched)
			}

			if err := s.DeleteNote(ctx, testSpace, tt.in.ID); err != nil {
				t.Fatalf("DeleteNote: %v", err)
			}
			if _, err := s.GetNote(ctx, testSpace, tt.in.ID); !errors.Is(err, ErrNotFound) {
				t.Errorf("after delete err = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestReminderCRUD(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   Reminder
	}{
		{
			name: "all optionals set",
			in:   Reminder{ID: "r-1", Text: "Ping", RemindAt: "2026-07-27T09:00:00", ProjectID: ptr("loom"), Done: ptr(true)},
		},
		{
			name: "optionals absent",
			in:   Reminder{ID: "r-2", Text: "Nudge", RemindAt: "2026-07-28T09:00:00"},
		},
		{
			name: "done false preserved",
			in:   Reminder{ID: "r-3", Text: "Check", RemindAt: "2026-07-29T09:00:00", Done: ptr(false)},
		},
		{
			name: "recurring reminder round-trips its interval",
			in:   Reminder{ID: "r-4", Text: "FIDO key", RemindAt: "2026-07-30T09:00:00", Repeat: &ReminderRepeat{Every: 1, Unit: "day"}},
		},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newTestSpaceStore(t)
			ctx := context.Background()
			mustCreateProject(t, s, "loom")
			if _, err := s.CreateReminder(ctx, testSpace, tt.in); err != nil {
				t.Fatalf("CreateReminder: %v", err)
			}
			got, err := s.GetReminder(ctx, testSpace, tt.in.ID)
			if err != nil {
				t.Fatalf("GetReminder: %v", err)
			}
			if !reflect.DeepEqual(got, tt.in) {
				t.Errorf("round-trip:\n got %+v\nwant %+v", got, tt.in)
			}

			got.Done = ptr(true)
			got.Text = "updated"
			if _, err := s.UpdateReminder(ctx, testSpace, got); err != nil {
				t.Fatalf("UpdateReminder: %v", err)
			}
			refetched, err := s.GetReminder(ctx, testSpace, tt.in.ID)
			if err != nil {
				t.Fatalf("GetReminder after update: %v", err)
			}
			if refetched.Text != "updated" || refetched.Done == nil || !*refetched.Done {
				t.Errorf("update not applied: %+v", refetched)
			}

			if err := s.DeleteReminder(ctx, testSpace, tt.in.ID); err != nil {
				t.Fatalf("DeleteReminder: %v", err)
			}
			if _, err := s.GetReminder(ctx, testSpace, tt.in.ID); !errors.Is(err, ErrNotFound) {
				t.Errorf("after delete err = %v, want ErrNotFound", err)
			}
		})
	}
}

// TestRescheduleReminder covers the re-arm that makes a reminder recurring:
// the interval survives into the dispatcher's pending read, a live reminder
// re-arms with its bookkeeping cleared, and a done or trashed one refuses to
// re-arm — which is how a recurring reminder stops.
func TestRescheduleReminder(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	pendingByID := func(t *testing.T, s *SpaceStore, id string) (PendingReminder, bool) {
		t.Helper()
		pend, err := s.PendingReminders(ctx, testSpace)
		if err != nil {
			t.Fatalf("PendingReminders: %v", err)
		}
		for _, p := range pend {
			if p.ID == id {
				return p, true
			}
		}
		return PendingReminder{}, false
	}

	t.Run("carries the interval and re-arms a live reminder", func(t *testing.T) {
		t.Parallel()
		s := newTestSpaceStore(t)
		rem := Reminder{ID: "r-rec", Text: "FIDO key", RemindAt: "2026-07-30T09:00:00", Repeat: &ReminderRepeat{Every: 1, Unit: "day"}}
		if _, err := s.CreateReminder(ctx, testSpace, rem); err != nil {
			t.Fatalf("CreateReminder: %v", err)
		}
		p, ok := pendingByID(t, s, "r-rec")
		if !ok {
			t.Fatal("recurring reminder missing from pending set")
		}
		if p.Repeat == nil || p.Repeat.Every != 1 || p.Repeat.Unit != "day" {
			t.Fatalf("pending reminder lost its interval: %+v", p.Repeat)
		}

		// Deliver it (marks notified) and record a failure, so there is
		// bookkeeping for the re-arm to clear.
		if err := s.MarkReminderNotified(ctx, testSpace, "r-rec"); err != nil {
			t.Fatalf("MarkReminderNotified: %v", err)
		}
		if _, err := s.RecordReminderFailure(ctx, testSpace, "r-rec"); err != nil {
			t.Fatalf("RecordReminderFailure: %v", err)
		}
		if _, ok := pendingByID(t, s, "r-rec"); ok {
			t.Fatal("delivered reminder should have left the pending set")
		}

		if err := s.RescheduleReminder(ctx, testSpace, "r-rec", "2026-07-31T09:00:00"); err != nil {
			t.Fatalf("RescheduleReminder: %v", err)
		}
		p, ok = pendingByID(t, s, "r-rec")
		if !ok {
			t.Fatal("re-armed reminder should be pending again")
		}
		if p.RemindAt != "2026-07-31T09:00:00" {
			t.Errorf("remind_at = %q, want the next occurrence", p.RemindAt)
		}
		if p.Attempts != 0 {
			t.Errorf("attempts = %d, want reset to 0", p.Attempts)
		}
	})

	t.Run("refuses to re-arm a done reminder", func(t *testing.T) {
		t.Parallel()
		s := newTestSpaceStore(t)
		if _, err := s.CreateReminder(ctx, testSpace, Reminder{
			ID: "r-done", Text: "Done", RemindAt: "2026-07-30T09:00:00",
			Repeat: &ReminderRepeat{Every: 1, Unit: "week"}, Done: ptr(true),
		}); err != nil {
			t.Fatalf("CreateReminder: %v", err)
		}
		if err := s.RescheduleReminder(ctx, testSpace, "r-done", "2026-08-06T09:00:00"); !errors.Is(err, ErrNotFound) {
			t.Errorf("reschedule of a done reminder err = %v, want ErrNotFound", err)
		}
	})

	t.Run("refuses to re-arm a trashed reminder", func(t *testing.T) {
		t.Parallel()
		s := newTestSpaceStore(t)
		if _, err := s.CreateReminder(ctx, testSpace, Reminder{
			ID: "r-trash", Text: "Trash", RemindAt: "2026-07-30T09:00:00",
			Repeat: &ReminderRepeat{Every: 2, Unit: "hour"},
		}); err != nil {
			t.Fatalf("CreateReminder: %v", err)
		}
		if err := s.DeleteReminder(ctx, testSpace, "r-trash"); err != nil {
			t.Fatalf("DeleteReminder: %v", err)
		}
		if err := s.RescheduleReminder(ctx, testSpace, "r-trash", "2026-07-30T11:00:00"); !errors.Is(err, ErrNotFound) {
			t.Errorf("reschedule of a trashed reminder err = %v, want ErrNotFound", err)
		}
	})
}

func TestInboxCRUD(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   InboxItem
	}{
		{
			name: "with suggestion",
			in: InboxItem{
				ID: "i-1", Raw: "call dan", CapturedAt: "2026-07-25T08:30:00",
				SuggestedKind: "task", SuggestedProjectID: ptr("loom"), Status: "pending",
			},
		},
		{
			name: "suggestion for project that does not exist (soft reference)",
			in: InboxItem{
				ID: "i-2", Raw: "new idea", CapturedAt: "2026-07-25T09:00:00",
				SuggestedKind: "project", SuggestedProjectID: ptr("does-not-exist"), Status: "pending",
			},
		},
		{
			name: "no suggestion",
			in: InboxItem{
				ID: "i-3", Raw: "stray thought", CapturedAt: "2026-07-25T10:00:00",
				SuggestedKind: "note", Status: "dismissed",
			},
		},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newTestSpaceStore(t)
			ctx := context.Background()
			mustCreateProject(t, s, "loom")
			if _, err := s.CreateInboxItem(ctx, testSpace, tt.in); err != nil {
				t.Fatalf("CreateInboxItem: %v", err)
			}
			got, err := s.GetInboxItem(ctx, testSpace, tt.in.ID)
			if err != nil {
				t.Fatalf("GetInboxItem: %v", err)
			}
			if !reflect.DeepEqual(got, tt.in) {
				t.Errorf("round-trip:\n got %+v\nwant %+v", got, tt.in)
			}

			got.Status = "converted"
			if _, err := s.UpdateInboxItem(ctx, testSpace, got); err != nil {
				t.Fatalf("UpdateInboxItem: %v", err)
			}
			refetched, err := s.GetInboxItem(ctx, testSpace, tt.in.ID)
			if err != nil {
				t.Fatalf("GetInboxItem after update: %v", err)
			}
			if refetched.Status != "converted" {
				t.Errorf("update not applied: %+v", refetched)
			}

			if err := s.DeleteInboxItem(ctx, testSpace, tt.in.ID); err != nil {
				t.Fatalf("DeleteInboxItem: %v", err)
			}
			if _, err := s.GetInboxItem(ctx, testSpace, tt.in.ID); !errors.Is(err, ErrNotFound) {
				t.Errorf("after delete err = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestSpaceStoreErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		op           func(ctx context.Context, s *SpaceStore) error
		wantNotFound bool
	}{
		{
			name: "duplicate project id",
			op: func(ctx context.Context, s *SpaceStore) error {
				_, err := s.CreateProject(ctx, testSpace, Project{ID: "loom", Name: "Dup", Color: "blue"})
				return err
			},
		},
		{
			name: "empty project id",
			op: func(ctx context.Context, s *SpaceStore) error {
				_, err := s.CreateProject(ctx, testSpace, Project{Name: "NoID"})
				return err
			},
		},
		{
			name: "activity referencing missing project",
			op: func(ctx context.Context, s *SpaceStore) error {
				_, err := s.CreateActivity(ctx, testSpace, ActivityEntry{
					ID: "a-x", ProjectID: "ghost", Date: "2026-07-01", Type: "work",
					Title: "t", Details: "d", Source: "manual",
				})
				return err
			},
		},
		{
			name: "task referencing missing project",
			op: func(ctx context.Context, s *SpaceStore) error {
				_, err := s.CreateTask(ctx, testSpace, TaskItem{
					ID: "t-x", ProjectID: ptr("ghost"), Title: "t", Status: "open", CreatedAt: "2026-07-01",
				})
				return err
			},
		},
		{
			name: "note referencing missing project",
			op: func(ctx context.Context, s *SpaceStore) error {
				_, err := s.CreateNote(ctx, testSpace, NoteItem{
					ID: "n-x", ProjectID: ptr("ghost"), Title: "t", Body: "b", CreatedAt: "2026-07-01",
				})
				return err
			},
		},
		{
			name: "reminder referencing missing project",
			op: func(ctx context.Context, s *SpaceStore) error {
				_, err := s.CreateReminder(ctx, testSpace, Reminder{
					ID: "r-x", Text: "t", RemindAt: "2026-07-27T09:00:00", ProjectID: ptr("ghost"),
				})
				return err
			},
		},
		{
			name: "update missing task",
			op: func(ctx context.Context, s *SpaceStore) error {
				_, err := s.UpdateTask(ctx, testSpace, TaskItem{ID: "ghost", Title: "t", Status: "open", CreatedAt: "x"})
				return err
			},
			wantNotFound: true,
		},
		{
			name: "update missing activity",
			op: func(ctx context.Context, s *SpaceStore) error {
				_, err := s.UpdateActivity(ctx, testSpace, ActivityEntry{
					ID: "ghost", ProjectID: "loom", Date: "d", Type: "work", Title: "t", Details: "d", Source: "manual",
				})
				return err
			},
			wantNotFound: true,
		},
		{
			name: "delete missing note",
			op: func(ctx context.Context, s *SpaceStore) error {
				return s.DeleteNote(ctx, testSpace, "ghost")
			},
			wantNotFound: true,
		},
		{
			name: "delete missing reminder",
			op: func(ctx context.Context, s *SpaceStore) error {
				return s.DeleteReminder(ctx, testSpace, "ghost")
			},
			wantNotFound: true,
		},
		{
			name: "delete missing inbox item",
			op: func(ctx context.Context, s *SpaceStore) error {
				return s.DeleteInboxItem(ctx, testSpace, "ghost")
			},
			wantNotFound: true,
		},
		{
			name: "invalid space id",
			op: func(ctx context.Context, s *SpaceStore) error {
				_, err := s.ListProjects(ctx, "../escape")
				return err
			},
		},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newTestSpaceStore(t)
			ctx := context.Background()
			mustCreateProject(t, s, "loom")
			err := tt.op(ctx, s)
			if err == nil {
				t.Fatal("want error, got nil")
			}
			if tt.wantNotFound && !errors.Is(err, ErrNotFound) {
				t.Errorf("err = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestStateAssemblyJSON(t *testing.T) {
	t.Parallel()
	s := newTestSpaceStore(t)
	ctx := context.Background()

	if _, err := s.CreateProject(ctx, testSpace, Project{
		ID: "loom", Name: "Loom", Color: "blue", Purpose: "Auth platform",
		Outcome: "Boring auth", CurrentFocus: "v0.9", NextAction: "Write test",
		AltNextActions: []string{"Sweep job"}, Status: "waiting", ResumeContext: "Plan agreed",
		WaitingOn: ptr("Dan"), Tags: []string{"go", "auth"},
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := s.CreateActivity(ctx, testSpace, ActivityEntry{
		ID: "act-1", ProjectID: "loom", Date: "2026-07-20", Type: "work",
		Title: "Fix bug", Details: "Details here", EffortHours: ptr(2.5), Source: "manual",
		Tags: []string{"bug"}, Links: []ActivityLink{{Label: "PR", URL: "https://example.com/pr/1"}},
		NextAction: ptr("Ship it"), Planned: ptr(true),
	}); err != nil {
		t.Fatalf("create activity: %v", err)
	}
	if _, err := s.CreateTask(ctx, testSpace, TaskItem{
		ID: "t-1", Title: "Standalone task", Status: "open", CreatedAt: "2026-07-01",
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if _, err := s.CreateNote(ctx, testSpace, NoteItem{
		ID: "n-1", ProjectID: ptr("loom"), Title: "Note", Body: "Body", CreatedAt: "2026-07-02",
	}); err != nil {
		t.Fatalf("create note: %v", err)
	}
	if _, err := s.CreateReminder(ctx, testSpace, Reminder{
		ID: "r-1", Text: "Ping Dan", RemindAt: "2026-07-27T09:00:00",
	}); err != nil {
		t.Fatalf("create reminder: %v", err)
	}
	if _, err := s.CreateInboxItem(ctx, testSpace, InboxItem{
		ID: "i-1", Raw: "raw text", CapturedAt: "2026-07-25T08:30:00",
		SuggestedKind: "task", Status: "pending",
	}); err != nil {
		t.Fatalf("create inbox item: %v", err)
	}

	state, err := s.State(ctx, testSpace)
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	got, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}

	// The exact wire format: frontend camelCase names, optionals omitted
	// when absent, arrays never null, no server-side timestamp leakage.
	want := `{` +
		`"projects":[{"id":"loom","name":"Loom","color":"blue","purpose":"Auth platform",` +
		`"outcome":"Boring auth","currentFocus":"v0.9","nextAction":"Write test",` +
		`"altNextActions":["Sweep job"],"status":"waiting","resumeContext":"Plan agreed",` +
		`"waitingOn":"Dan","tags":["go","auth"]}],` +
		`"activities":[{"id":"act-1","projectId":"loom","date":"2026-07-20","type":"work",` +
		`"title":"Fix bug","details":"Details here","effortHours":2.5,"source":"manual",` +
		`"tags":["bug"],"links":[{"label":"PR","url":"https://example.com/pr/1"}],` +
		`"nextAction":"Ship it","planned":true}],` +
		`"tasks":[{"id":"t-1","title":"Standalone task","details":"","status":"open","createdAt":"2026-07-01"}],` +
		`"notes":[{"id":"n-1","projectId":"loom","title":"Note","body":"Body","createdAt":"2026-07-02"}],` +
		`"reminders":[{"id":"r-1","text":"Ping Dan","details":"","remindAt":"2026-07-27T09:00:00"}],` +
		`"inbox":[{"id":"i-1","raw":"raw text","capturedAt":"2026-07-25T08:30:00",` +
		`"suggestedKind":"task","status":"pending"}]}`
	if string(got) != want {
		t.Errorf("state JSON mismatch:\n got %s\nwant %s", got, want)
	}
}

func TestStateEmptySpaceMarshalsEmptyArrays(t *testing.T) {
	t.Parallel()
	s := newTestSpaceStore(t)
	state, err := s.State(context.Background(), "fresh")
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	got, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	want := `{"projects":[],"activities":[],"tasks":[],"notes":[],"reminders":[],"inbox":[]}`
	if string(got) != want {
		t.Errorf("empty state JSON = %s, want %s", got, want)
	}
}

func TestImportState(t *testing.T) {
	t.Parallel()
	fullState := func() SpaceState {
		return SpaceState{
			Projects: []Project{
				{ID: "p1", Name: "One", Color: "blue", Purpose: "p", Outcome: "o",
					CurrentFocus: "cf", NextAction: "na", Status: "active", ResumeContext: "rc"},
				{ID: "p2", Name: "Two", Color: "green", Purpose: "p", Outcome: "o",
					CurrentFocus: "cf", NextAction: "na", Status: "paused", ResumeContext: "rc"},
			},
			Activities: []ActivityEntry{
				{ID: "a1", ProjectID: "p1", Date: "2026-07-01", Type: "work", Title: "t",
					Details: "d", Source: "manual"},
			},
			Tasks:     []TaskItem{{ID: "t1", ProjectID: ptr("p2"), Title: "task", Status: "open", CreatedAt: "2026-07-01"}},
			Notes:     []NoteItem{{ID: "n1", Title: "note", Body: "b", CreatedAt: "2026-07-01"}},
			Reminders: []Reminder{{ID: "r1", Text: "remind", RemindAt: "2026-07-27T09:00:00"}},
			Inbox:     []InboxItem{{ID: "i1", Raw: "raw", CapturedAt: "2026-07-25T08:00:00", SuggestedKind: "task", Status: "pending"}},
		}
	}
	wantCounts := func(st SpaceState) [6]int {
		return [6]int{len(st.Projects), len(st.Activities), len(st.Tasks),
			len(st.Notes), len(st.Reminders), len(st.Inbox)}
	}
	tests := []struct {
		name    string
		state   func() SpaceState
		wantErr bool
		want    [6]int // projects, activities, tasks, notes, reminders, inbox left in the space
	}{
		{
			name:  "happy path imports everything",
			state: fullState,
			want:  [6]int{2, 1, 1, 1, 1, 1},
		},
		{
			name:  "empty state is a no-op",
			state: func() SpaceState { return SpaceState{} },
			want:  [6]int{},
		},
		{
			name: "mid-dataset foreign key failure leaves the space untouched",
			state: func() SpaceState {
				st := fullState()
				st.Activities = append(st.Activities, ActivityEntry{
					ID: "a-bad", ProjectID: "ghost", Date: "2026-07-02", Type: "work",
					Title: "t", Details: "d", Source: "manual",
				})
				return st
			},
			wantErr: true,
			want:    [6]int{}, // atomic: nothing from the batch may remain
		},
		{
			name: "duplicate id within the batch leaves the space untouched",
			state: func() SpaceState {
				st := fullState()
				st.Projects = append(st.Projects, st.Projects[0])
				return st
			},
			wantErr: true,
			want:    [6]int{},
		},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newTestSpaceStore(t)
			ctx := context.Background()
			err := s.ImportState(ctx, testSpace, tt.state())
			if tt.wantErr && err == nil {
				t.Fatal("want error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ImportState: %v", err)
			}
			after, err := s.State(ctx, testSpace)
			if err != nil {
				t.Fatalf("State: %v", err)
			}
			if got := wantCounts(after); got != tt.want {
				t.Errorf("post-import counts = %v, want %v", got, tt.want)
			}
			if !tt.wantErr && len(after.Projects) > 0 {
				// A clean import must round-trip content, not just counts.
				if after.Projects[0].ID != "p1" || after.Tasks[0].Title != "task" {
					t.Errorf("imported content mismatch: %+v", after)
				}
			}
		})
	}
}

func TestNewStoresCreatePrivateDirs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	core, err := NewCoreStore(WithDataDir(filepath.Join(dir, "data")), WithClock(fixedClock))
	if err != nil {
		t.Fatalf("NewCoreStore: %v", err)
	}
	spaces, err := NewSpaceStore(WithDataDir(filepath.Join(dir, "data")), WithClock(fixedClock))
	if err != nil {
		t.Fatalf("NewSpaceStore: %v", err)
	}
	t.Cleanup(func() {
		if err := core.Close(); err != nil {
			t.Errorf("close core: %v", err)
		}
		if err := spaces.Close(); err != nil {
			t.Errorf("close spaces: %v", err)
		}
	})
	for _, sub := range []string{"data", "data/spaces"} {
		info, err := os.Stat(filepath.Join(dir, sub))
		if err != nil {
			t.Fatalf("stat %s: %v", sub, err)
		}
		// Personal task data must not be group/world readable.
		if perm := info.Mode().Perm(); perm != 0o700 {
			t.Errorf("%s permissions = %o, want 700", sub, perm)
		}
	}
}

func TestSpaceStoreConnectionCache(t *testing.T) {
	t.Parallel()
	s := newTestSpaceStore(t)
	ctx := context.Background()
	db1, err := s.db(ctx, testSpace)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	db2, err := s.db(ctx, testSpace)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	if db1 != db2 {
		t.Error("connection not cached: got distinct handles for same space")
	}
	other, err := s.db(ctx, "other")
	if err != nil {
		t.Fatalf("open other space: %v", err)
	}
	if other == db1 {
		t.Error("distinct spaces share a handle")
	}
}
