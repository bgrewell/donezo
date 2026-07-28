package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// mustCreateInboxItem inserts a pending inbox capture fixtures convert.
func mustCreateInboxItem(t *testing.T, s *SpaceStore, id string) InboxItem {
	t.Helper()
	it, err := s.CreateInboxItem(context.Background(), testSpace, InboxItem{
		ID: id, Raw: "raw " + id, CapturedAt: "2026-07-25T08:30:00",
		SuggestedKind: "task", Status: "pending",
	})
	if err != nil {
		t.Fatalf("create inbox item %s: %v", id, err)
	}
	return it
}

func TestConstraintClassification(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		op      func(ctx context.Context, s *SpaceStore) error
		wantErr error
	}{
		{
			name: "duplicate project id is ErrDuplicateID",
			op: func(ctx context.Context, s *SpaceStore) error {
				_, err := s.CreateProject(ctx, testSpace, Project{ID: "loom", Name: "Dup", Color: "blue"})
				return err
			},
			wantErr: ErrDuplicateID,
		},
		{
			name: "duplicate task id is ErrDuplicateID",
			op: func(ctx context.Context, s *SpaceStore) error {
				task := TaskItem{ID: "t-dup", Title: "t", Status: "open", CreatedAt: "2026-07-01"}
				if _, err := s.CreateTask(ctx, testSpace, task); err != nil {
					return err
				}
				_, err := s.CreateTask(ctx, testSpace, task)
				return err
			},
			wantErr: ErrDuplicateID,
		},
		{
			name: "activity referencing a missing project is ErrInvalidReference",
			op: func(ctx context.Context, s *SpaceStore) error {
				_, err := s.CreateActivity(ctx, testSpace, ActivityEntry{
					ID: "a-x", ProjectID: "ghost", Date: "2026-07-01", Type: "work",
					Title: "t", Details: "d", Source: "manual",
				})
				return err
			},
			wantErr: ErrInvalidReference,
		},
		{
			name: "reminder referencing a missing project is ErrInvalidReference",
			op: func(ctx context.Context, s *SpaceStore) error {
				_, err := s.CreateReminder(ctx, testSpace, Reminder{
					ID: "r-x", Text: "t", RemindAt: "2026-07-27T09:00:00", ProjectID: ptr("ghost"),
				})
				return err
			},
			wantErr: ErrInvalidReference,
		},
		{
			name: "note update referencing a missing project is ErrInvalidReference",
			op: func(ctx context.Context, s *SpaceStore) error {
				n, err := s.CreateNote(ctx, testSpace, NoteItem{
					ID: "n-x", Title: "t", Body: "b", CreatedAt: "2026-07-01",
				})
				if err != nil {
					return err
				}
				n.ProjectID = ptr("ghost")
				_, err = s.UpdateNote(ctx, testSpace, n)
				return err
			},
			wantErr: ErrInvalidReference,
		},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newTestSpaceStore(t)
			mustCreateProject(t, s, "loom")
			err := tt.op(context.Background(), s)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestEnsureSpace(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s, err := NewSpaceStore(WithDataDir(dir), WithClock(fixedClock))
	if err != nil {
		t.Fatalf("NewSpaceStore: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("close space store: %v", err)
		}
	})
	if err := s.EnsureSpace(context.Background(), "fresh"); err != nil {
		t.Fatalf("EnsureSpace: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "spaces", "fresh.db")); err != nil {
		t.Errorf("space database not created: %v", err)
	}
	if err := s.EnsureSpace(context.Background(), "../escape"); err == nil {
		t.Error("want error for invalid space id, got nil")
	}
}

func TestPatchProject(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		id      string
		apply   func(*Project) error
		wantErr error
		check   func(t *testing.T, got Project)
	}{
		{
			name: "subset of fields incl next-action lifecycle",
			id:   "loom",
			apply: func(p *Project) error {
				p.NextAction = "ship it"
				p.AltNextActions = []string{"write docs"}
				p.ResumeContext = "was mid-refactor"
				p.Status = "waiting"
				p.WaitingOn = ptr("Dan")
				return nil
			},
			check: func(t *testing.T, got Project) {
				t.Helper()
				if got.NextAction != "ship it" || got.Status != "waiting" ||
					got.WaitingOn == nil || *got.WaitingOn != "Dan" ||
					!reflect.DeepEqual(got.AltNextActions, []string{"write docs"}) {
					t.Errorf("patched project = %+v", got)
				}
				// Untouched fields keep their stored values.
				if got.Name != "loom" || got.Color != "blue" {
					t.Errorf("untouched fields changed: %+v", got)
				}
				if got.UpdatedAt != fixedNow {
					t.Errorf("UpdatedAt = %q, want %q", got.UpdatedAt, fixedNow)
				}
			},
		},
		{
			name: "nil lists normalize to empty",
			id:   "loom",
			apply: func(p *Project) error {
				p.AltNextActions = nil
				p.Tags = nil
				return nil
			},
			check: func(t *testing.T, got Project) {
				t.Helper()
				if got.AltNextActions == nil || got.Tags == nil {
					t.Errorf("lists not normalized: %+v", got)
				}
			},
		},
		{
			name: "identity is immutable even if apply rewrites it",
			id:   "loom",
			apply: func(p *Project) error {
				p.ID = "hijack"
				p.Name = "Renamed"
				return nil
			},
			check: func(t *testing.T, got Project) {
				t.Helper()
				if got.ID != "loom" || got.Name != "Renamed" {
					t.Errorf("patched project = %+v", got)
				}
			},
		},
		{
			name:    "missing id is ErrNotFound",
			id:      "ghost",
			apply:   func(*Project) error { return nil },
			wantErr: ErrNotFound,
		},
		{
			name:    "apply error aborts the patch",
			id:      "loom",
			apply:   func(p *Project) error { p.Name = "broken"; return errors.New("nope") },
			wantErr: nil, // checked by message below
		},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newTestSpaceStore(t)
			ctx := context.Background()
			mustCreateProject(t, s, "loom")
			before, err := s.GetProject(ctx, testSpace, "loom")
			if err != nil {
				t.Fatalf("GetProject before: %v", err)
			}
			got, err := s.PatchProject(ctx, testSpace, tt.id, tt.apply)
			if tt.name == "apply error aborts the patch" {
				if err == nil || err.Error() != "nope" {
					t.Fatalf("err = %v, want the apply error", err)
				}
				after, gerr := s.GetProject(ctx, testSpace, "loom")
				if gerr != nil {
					t.Fatalf("GetProject after: %v", gerr)
				}
				if !reflect.DeepEqual(after, before) {
					t.Errorf("aborted patch changed the row:\n got %+v\nwant %+v", after, before)
				}
				return
			}
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("PatchProject: %v", err)
			}
			stored, err := s.GetProject(ctx, testSpace, "loom")
			if err != nil {
				t.Fatalf("GetProject after: %v", err)
			}
			if !reflect.DeepEqual(stored, got) {
				t.Errorf("returned row diverges from stored:\n got %+v\nstored %+v", got, stored)
			}
			tt.check(t, got)
		})
	}
}

func TestPatchActivity(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		apply   func(*ActivityEntry) error
		wantErr error
		check   func(t *testing.T, got ActivityEntry)
	}{
		{
			name: "patch type and planned flag",
			apply: func(a *ActivityEntry) error {
				a.Type = "milestone"
				a.Planned = ptr(true)
				return nil
			},
			check: func(t *testing.T, got ActivityEntry) {
				t.Helper()
				if got.Type != "milestone" || got.Planned == nil || !*got.Planned {
					t.Errorf("patched activity = %+v", got)
				}
				if got.Title != "Fix" {
					t.Errorf("untouched title changed: %+v", got)
				}
			},
		},
		{
			name:    "reference to a missing project rolls back",
			apply:   func(a *ActivityEntry) error { a.ProjectID = "ghost"; return nil },
			wantErr: ErrInvalidReference,
		},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newTestSpaceStore(t)
			ctx := context.Background()
			mustCreateProject(t, s, "loom")
			if _, err := s.CreateActivity(ctx, testSpace, ActivityEntry{
				ID: "act-1", ProjectID: "loom", Date: "2026-07-20", Type: "work",
				Title: "Fix", Details: "d", Source: "manual",
			}); err != nil {
				t.Fatalf("create activity: %v", err)
			}
			got, err := s.PatchActivity(ctx, testSpace, "act-1", tt.apply)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				after, gerr := s.GetActivity(ctx, testSpace, "act-1")
				if gerr != nil {
					t.Fatalf("GetActivity after: %v", gerr)
				}
				if after.ProjectID != "loom" {
					t.Errorf("failed patch leaked: %+v", after)
				}
				return
			}
			if err != nil {
				t.Fatalf("PatchActivity: %v", err)
			}
			tt.check(t, got)
		})
	}
}

func TestPatchTask(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		apply   func(*TaskItem) error
		wantErr error
		check   func(t *testing.T, got TaskItem)
	}{
		{
			name: "complete the task and clear due",
			apply: func(task *TaskItem) error {
				task.Status = "done"
				task.Due = nil
				return nil
			},
			check: func(t *testing.T, got TaskItem) {
				t.Helper()
				if got.Status != "done" || got.Due != nil {
					t.Errorf("patched task = %+v", got)
				}
			},
		},
		{
			name:    "reference to a missing project rolls back",
			apply:   func(task *TaskItem) error { task.ProjectID = ptr("ghost"); return nil },
			wantErr: ErrInvalidReference,
		},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newTestSpaceStore(t)
			ctx := context.Background()
			mustCreateProject(t, s, "loom")
			if _, err := s.CreateTask(ctx, testSpace, TaskItem{
				ID: "t-1", ProjectID: ptr("loom"), Title: "Do", Status: "open",
				Due: ptr("2026-08-01"), CreatedAt: "2026-07-01",
			}); err != nil {
				t.Fatalf("create task: %v", err)
			}
			got, err := s.PatchTask(ctx, testSpace, "t-1", tt.apply)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				after, gerr := s.GetTask(ctx, testSpace, "t-1")
				if gerr != nil {
					t.Fatalf("GetTask after: %v", gerr)
				}
				if after.ProjectID == nil || *after.ProjectID != "loom" {
					t.Errorf("failed patch leaked: %+v", after)
				}
				return
			}
			if err != nil {
				t.Fatalf("PatchTask: %v", err)
			}
			tt.check(t, got)
		})
	}
}

func TestPatchReminderAndInboxItem(t *testing.T) {
	t.Parallel()
	t.Run("reminder done toggles", func(t *testing.T) {
		t.Parallel()
		s := newTestSpaceStore(t)
		ctx := context.Background()
		if _, err := s.CreateReminder(ctx, testSpace, Reminder{
			ID: "r-1", Text: "Ping", RemindAt: "2026-07-27T09:00:00",
		}); err != nil {
			t.Fatalf("create reminder: %v", err)
		}
		got, err := s.PatchReminder(ctx, testSpace, "r-1", func(r *Reminder) error {
			r.Done = ptr(true)
			return nil
		})
		if err != nil {
			t.Fatalf("PatchReminder: %v", err)
		}
		if got.Done == nil || !*got.Done || got.Text != "Ping" {
			t.Errorf("patched reminder = %+v", got)
		}
	})
	t.Run("reminder missing id is ErrNotFound", func(t *testing.T) {
		t.Parallel()
		s := newTestSpaceStore(t)
		_, err := s.PatchReminder(context.Background(), testSpace, "ghost", func(*Reminder) error { return nil })
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("err = %v, want ErrNotFound", err)
		}
	})
	t.Run("inbox item status dismisses", func(t *testing.T) {
		t.Parallel()
		s := newTestSpaceStore(t)
		mustCreateInboxItem(t, s, "i-1")
		got, err := s.PatchInboxItem(context.Background(), testSpace, "i-1", func(it *InboxItem) error {
			it.Status = "dismissed"
			return nil
		})
		if err != nil {
			t.Fatalf("PatchInboxItem: %v", err)
		}
		if got.Status != "dismissed" || got.Raw != "raw i-1" {
			t.Errorf("patched inbox item = %+v", got)
		}
	})
	t.Run("inbox missing id is ErrNotFound", func(t *testing.T) {
		t.Parallel()
		s := newTestSpaceStore(t)
		_, err := s.PatchInboxItem(context.Background(), testSpace, "ghost", func(*InboxItem) error { return nil })
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("err = %v, want ErrNotFound", err)
		}
	})
}

func TestConvertInboxItem(t *testing.T) {
	t.Parallel()
	conversions := map[string]Conversion{
		"task": {Kind: "task", Task: &TaskItem{
			ID: "t-new", Title: "Do", Status: "open", CreatedAt: "2026-07-25"}},
		"note": {Kind: "note", Note: &NoteItem{
			ID: "n-new", Title: "Note", Body: "b", CreatedAt: "2026-07-25"}},
		"reminder": {Kind: "reminder", Reminder: &Reminder{
			ID: "r-new", Text: "Ping", RemindAt: "2026-07-27T09:00:00"}},
		"activity": {Kind: "activity", Activity: &ActivityEntry{
			ID: "a-new", ProjectID: "loom", Date: "2026-07-25", Type: "work",
			Title: "Did", Details: "d", Source: "capture"}},
		"project": {Kind: "project", Project: &Project{
			ID: "p-new", Name: "New", Color: "green", Purpose: "p", Outcome: "o",
			CurrentFocus: "cf", NextAction: "na", Status: "active", ResumeContext: "rc"}},
	}
	// exists reports whether the conversion's created entity is stored.
	exists := func(ctx context.Context, s *SpaceStore, kind string) error {
		var err error
		switch kind {
		case "task":
			_, err = s.GetTask(ctx, testSpace, "t-new")
		case "note":
			_, err = s.GetNote(ctx, testSpace, "n-new")
		case "reminder":
			_, err = s.GetReminder(ctx, testSpace, "r-new")
		case "activity":
			_, err = s.GetActivity(ctx, testSpace, "a-new")
		case "project":
			_, err = s.GetProject(ctx, testSpace, "p-new")
		}
		return err
	}
	for kind, conv := range conversions {
		kind, conv := kind, conv // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run("converts to "+kind, func(t *testing.T) {
			t.Parallel()
			s := newTestSpaceStore(t)
			ctx := context.Background()
			mustCreateProject(t, s, "loom")
			mustCreateInboxItem(t, s, "i-1")
			it, err := s.ConvertInboxItem(ctx, testSpace, "i-1", conv)
			if err != nil {
				t.Fatalf("ConvertInboxItem: %v", err)
			}
			if it.Status != "converted" {
				t.Errorf("inbox status = %q, want converted", it.Status)
			}
			if err := exists(ctx, s, kind); err != nil {
				t.Errorf("created %s missing: %v", kind, err)
			}
		})
	}

	t.Run("failure rolls the whole conversion back", func(t *testing.T) {
		t.Parallel()
		s := newTestSpaceStore(t)
		ctx := context.Background()
		mustCreateProject(t, s, "loom")
		mustCreateInboxItem(t, s, "i-1")
		_, err := s.ConvertInboxItem(ctx, testSpace, "i-1", Conversion{
			Kind: "activity",
			Activity: &ActivityEntry{
				ID: "a-bad", ProjectID: "ghost", Date: "2026-07-25", Type: "work",
				Title: "t", Details: "d", Source: "capture",
			},
		})
		if !errors.Is(err, ErrInvalidReference) {
			t.Fatalf("err = %v, want ErrInvalidReference", err)
		}
		// Atomicity: the inbox item must still be pending, the activity absent.
		it, err := s.GetInboxItem(ctx, testSpace, "i-1")
		if err != nil {
			t.Fatalf("GetInboxItem: %v", err)
		}
		if it.Status != "pending" {
			t.Errorf("inbox status after rollback = %q, want pending", it.Status)
		}
		if _, err := s.GetActivity(ctx, testSpace, "a-bad"); !errors.Is(err, ErrNotFound) {
			t.Errorf("activity after rollback err = %v, want ErrNotFound", err)
		}
	})

	t.Run("duplicate created id rolls back", func(t *testing.T) {
		t.Parallel()
		s := newTestSpaceStore(t)
		ctx := context.Background()
		mustCreateInboxItem(t, s, "i-1")
		if _, err := s.CreateTask(ctx, testSpace, TaskItem{
			ID: "t-new", Title: "already here", Status: "open", CreatedAt: "2026-07-01",
		}); err != nil {
			t.Fatalf("create task: %v", err)
		}
		_, err := s.ConvertInboxItem(ctx, testSpace, "i-1", conversions["task"])
		if !errors.Is(err, ErrDuplicateID) {
			t.Fatalf("err = %v, want ErrDuplicateID", err)
		}
		it, err := s.GetInboxItem(ctx, testSpace, "i-1")
		if err != nil {
			t.Fatalf("GetInboxItem: %v", err)
		}
		if it.Status != "pending" {
			t.Errorf("inbox status after rollback = %q, want pending", it.Status)
		}
	})

	t.Run("unknown inbox id is ErrNotFound", func(t *testing.T) {
		t.Parallel()
		s := newTestSpaceStore(t)
		_, err := s.ConvertInboxItem(context.Background(), testSpace, "ghost", conversions["note"])
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("err = %v, want ErrNotFound", err)
		}
	})

	t.Run("kind without matching payload errors", func(t *testing.T) {
		t.Parallel()
		s := newTestSpaceStore(t)
		mustCreateInboxItem(t, s, "i-1")
		if _, err := s.ConvertInboxItem(context.Background(), testSpace, "i-1",
			Conversion{Kind: "task"}); err == nil {
			t.Error("want error for missing payload, got nil")
		}
	})

	t.Run("extra payload of another kind errors", func(t *testing.T) {
		t.Parallel()
		s := newTestSpaceStore(t)
		mustCreateInboxItem(t, s, "i-1")
		conv := conversions["task"]
		conv.Note = conversions["note"].Note
		if _, err := s.ConvertInboxItem(context.Background(), testSpace, "i-1", conv); err == nil {
			t.Error("want error for extra payload, got nil")
		}
	})

	t.Run("unknown kind errors", func(t *testing.T) {
		t.Parallel()
		s := newTestSpaceStore(t)
		mustCreateInboxItem(t, s, "i-1")
		if _, err := s.ConvertInboxItem(context.Background(), testSpace, "i-1",
			Conversion{Kind: "wish"}); err == nil {
			t.Error("want error for unknown kind, got nil")
		}
	})
}

func TestCreateSpaceAtEnd(t *testing.T) {
	t.Parallel()
	s := newTestCoreStore(t)
	ctx := context.Background()
	ben, err := s.CreateUser(ctx, "ben", "Ben")
	if err != nil {
		t.Fatalf("create user ben: %v", err)
	}
	other, err := s.CreateUser(ctx, "other", "Other")
	if err != nil {
		t.Fatalf("create user other: %v", err)
	}

	first, err := s.CreateSpaceAtEnd(ctx, Space{ID: "one", UserID: ben.ID, Name: "One", Color: "blue"})
	if err != nil {
		t.Fatalf("CreateSpaceAtEnd one: %v", err)
	}
	if first.Position != 0 {
		t.Errorf("first position = %d, want 0", first.Position)
	}
	if first.CreatedAt != fixedNow {
		t.Errorf("CreatedAt = %q, want %q", first.CreatedAt, fixedNow)
	}
	second, err := s.CreateSpaceAtEnd(ctx, Space{ID: "two", UserID: ben.ID, Name: "Two", Color: "rose"})
	if err != nil {
		t.Fatalf("CreateSpaceAtEnd two: %v", err)
	}
	if second.Position != 1 {
		t.Errorf("second position = %d, want 1", second.Position)
	}
	// Positions are per owner: another user's first space starts at 0.
	theirs, err := s.CreateSpaceAtEnd(ctx, Space{ID: "theirs", UserID: other.ID, Name: "Theirs", Color: "tan"})
	if err != nil {
		t.Fatalf("CreateSpaceAtEnd theirs: %v", err)
	}
	if theirs.Position != 0 {
		t.Errorf("other user's position = %d, want 0", theirs.Position)
	}

	if _, err := s.CreateSpaceAtEnd(ctx, Space{ID: "one", UserID: ben.ID, Name: "Dup", Color: "blue"}); !errors.Is(err, ErrDuplicateID) {
		t.Errorf("duplicate err = %v, want ErrDuplicateID", err)
	}
	if _, err := s.CreateSpaceAtEnd(ctx, Space{ID: "../bad", UserID: ben.ID, Name: "Bad", Color: "blue"}); err == nil {
		t.Error("want error for invalid id, got nil")
	}
	if _, err := s.CreateSpaceAtEnd(ctx, Space{ID: "noname", UserID: ben.ID, Color: "blue"}); err == nil {
		t.Error("want error for empty name, got nil")
	}
}

func TestPatchSpace(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		id      string
		apply   func(*Space) error
		wantErr bool
		check   func(t *testing.T, got Space)
	}{
		{
			name: "name color and position",
			id:   "one",
			apply: func(sp *Space) error {
				sp.Name = "Renamed"
				sp.Color = "green"
				sp.Position = 7
				return nil
			},
			check: func(t *testing.T, got Space) {
				t.Helper()
				if got.Name != "Renamed" || got.Color != "green" || got.Position != 7 {
					t.Errorf("patched space = %+v", got)
				}
			},
		},
		{
			name: "identity is immutable even if apply rewrites it",
			id:   "one",
			apply: func(sp *Space) error {
				sp.ID = "hijack"
				sp.UserID = 999
				sp.CreatedAt = "1999-01-01T00:00:00Z"
				return nil
			},
			check: func(t *testing.T, got Space) {
				t.Helper()
				if got.ID != "one" || got.UserID == 999 || got.CreatedAt != fixedNow {
					t.Errorf("identity leaked: %+v", got)
				}
			},
		},
		{
			name:    "missing id is ErrNotFound",
			id:      "ghost",
			apply:   func(*Space) error { return nil },
			wantErr: true,
		},
		{
			name:    "empty name is rejected",
			id:      "one",
			apply:   func(sp *Space) error { sp.Name = ""; return nil },
			wantErr: true,
		},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newTestCoreStore(t)
			ctx := context.Background()
			ben, err := s.CreateUser(ctx, "ben", "Ben")
			if err != nil {
				t.Fatalf("create user: %v", err)
			}
			if _, err := s.CreateSpace(ctx, Space{ID: "one", UserID: ben.ID, Name: "One", Color: "blue"}); err != nil {
				t.Fatalf("create space: %v", err)
			}
			got, err := s.PatchSpace(ctx, tt.id, tt.apply)
			if tt.wantErr {
				if err == nil {
					t.Fatal("want error, got nil")
				}
				if tt.id == "ghost" && !errors.Is(err, ErrNotFound) {
					t.Errorf("err = %v, want ErrNotFound", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("PatchSpace: %v", err)
			}
			stored, err := s.GetSpace(ctx, "one")
			if err != nil {
				t.Fatalf("GetSpace after: %v", err)
			}
			if !reflect.DeepEqual(stored, got) {
				t.Errorf("returned row diverges from stored:\n got %+v\nstored %+v", got, stored)
			}
			tt.check(t, got)
		})
	}
}

func TestSetSpaceArchived(t *testing.T) {
	t.Parallel()
	s := newTestCoreStore(t)
	ctx := context.Background()
	ben, err := s.CreateUser(ctx, "ben", "Ben")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := s.CreateSpace(ctx, Space{ID: "one", UserID: ben.ID, Name: "One", Color: "blue"}); err != nil {
		t.Fatalf("create space: %v", err)
	}

	archived, err := s.SetSpaceArchived(ctx, "one", true)
	if err != nil {
		t.Fatalf("SetSpaceArchived(true): %v", err)
	}
	if archived.ArchivedAt == nil || *archived.ArchivedAt != fixedNow {
		t.Errorf("ArchivedAt = %v, want %q", archived.ArchivedAt, fixedNow)
	}
	unarchived, err := s.SetSpaceArchived(ctx, "one", false)
	if err != nil {
		t.Fatalf("SetSpaceArchived(false): %v", err)
	}
	if unarchived.ArchivedAt != nil {
		t.Errorf("ArchivedAt after unarchive = %v, want nil", unarchived.ArchivedAt)
	}
	if _, err := s.SetSpaceArchived(ctx, "ghost", true); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}
