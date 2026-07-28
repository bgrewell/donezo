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

func TestDeleteProjectCascade(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		id      string
		want    ProjectCascadeResult
		wantErr error
		check   func(t *testing.T, s *SpaceStore)
	}{
		{
			name: "cascade deletes owned content and detaches loose references",
			id:   "loom",
			want: ProjectCascadeResult{
				Project: 1, Activities: 3, Tasks: 2, Notes: 1,
				DetachedInbox: 1, DetachedReminders: 1,
			},
			check: func(t *testing.T, s *SpaceStore) {
				t.Helper()
				if _, err := s.GetProject(context.Background(), testSpace, "loom"); !errors.Is(err, ErrNotFound) {
					t.Errorf("loom after cascade: err = %v, want ErrNotFound", err)
				}
				st := mustState(t, s)
				if got := idsOf(st.Projects, func(p Project) string { return p.ID }); !reflect.DeepEqual(got, []string{"keep", "bare"}) {
					t.Errorf("projects = %v, want [keep bare]", got)
				}
				if got := idsOf(st.Activities, func(a ActivityEntry) string { return a.ID }); !reflect.DeepEqual(got, []string{"act-k-1"}) {
					t.Errorf("activities = %v, want [act-k-1]", got)
				}
				if got := idsOf(st.Tasks, func(tk TaskItem) string { return tk.ID }); !reflect.DeepEqual(got, []string{"tsk-k-1"}) {
					t.Errorf("tasks = %v, want [tsk-k-1]", got)
				}
				if got := idsOf(st.Notes, func(n NoteItem) string { return n.ID }); !reflect.DeepEqual(got, []string{"note-k-1"}) {
					t.Errorf("notes = %v, want [note-k-1]", got)
				}
				// Loose references survive with their project column nulled;
				// the other projects' references stay attached.
				for _, it := range st.Inbox {
					switch it.ID {
					case "inb-l-1":
						if it.SuggestedProjectID != nil {
							t.Errorf("inb-l-1 suggestedProjectId = %q, want nil", *it.SuggestedProjectID)
						}
					case "inb-k-1":
						if it.SuggestedProjectID == nil || *it.SuggestedProjectID != "keep" {
							t.Errorf("inb-k-1 suggestedProjectId = %v, want keep", it.SuggestedProjectID)
						}
					}
				}
				if got := idsOf(st.Inbox, func(it InboxItem) string { return it.ID }); !reflect.DeepEqual(got, []string{"inb-l-1", "inb-k-1", "inb-ghost"}) {
					t.Errorf("inbox = %v, want all three captures kept", got)
				}
				for _, r := range st.Reminders {
					switch r.ID {
					case "rem-l-1":
						if r.ProjectID != nil {
							t.Errorf("rem-l-1 projectId = %q, want nil", *r.ProjectID)
						}
					case "rem-k-1":
						if r.ProjectID == nil || *r.ProjectID != "keep" {
							t.Errorf("rem-k-1 projectId = %v, want keep", r.ProjectID)
						}
					}
				}
				if got := idsOf(st.Reminders, func(r Reminder) string { return r.ID }); !reflect.DeepEqual(got, []string{"rem-l-1", "rem-k-1"}) {
					t.Errorf("reminders = %v, want both kept", got)
				}
			},
		},
		{
			name: "project with no references reports zero cascade counts",
			id:   "bare",
			want: ProjectCascadeResult{Project: 1},
			check: func(t *testing.T, s *SpaceStore) {
				t.Helper()
				st := mustState(t, s)
				if len(st.Activities) != 4 || len(st.Tasks) != 3 || len(st.Notes) != 2 {
					t.Errorf("other projects' content disturbed: %d activities, %d tasks, %d notes",
						len(st.Activities), len(st.Tasks), len(st.Notes))
				}
			},
		},
		{
			name:    "unknown project is ErrNotFound and rolls the detaches back",
			id:      "ghost",
			wantErr: ErrNotFound,
			check: func(t *testing.T, s *SpaceStore) {
				t.Helper()
				// inb-ghost suggests "ghost"; the failed cascade must leave
				// that suggestion attached (the detach ran inside the
				// rolled-back transaction).
				it, err := s.GetInboxItem(context.Background(), testSpace, "inb-ghost")
				if err != nil {
					t.Fatalf("get inb-ghost: %v", err)
				}
				if it.SuggestedProjectID == nil || *it.SuggestedProjectID != "ghost" {
					t.Errorf("inb-ghost suggestedProjectId = %v, want ghost (rollback)", it.SuggestedProjectID)
				}
				st := mustState(t, s)
				if len(st.Projects) != 3 || len(st.Activities) != 4 || len(st.Tasks) != 3 {
					t.Errorf("failed cascade disturbed content: %d projects, %d activities, %d tasks",
						len(st.Projects), len(st.Activities), len(st.Tasks))
				}
			},
		},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newTestSpaceStore(t)
			seedCascadeFixture(t, s)
			got, err := s.DeleteProjectCascade(context.Background(), testSpace, tt.id)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("DeleteProjectCascade: %v", err)
			}
			if err == nil && got != tt.want {
				t.Errorf("counts = %+v, want %+v", got, tt.want)
			}
			if tt.check != nil {
				tt.check(t, s)
			}
		})
	}

	t.Run("blank id is rejected before SQL", func(t *testing.T) {
		t.Parallel()
		s := newTestSpaceStore(t)
		if _, err := s.DeleteProjectCascade(context.Background(), testSpace, ""); err == nil {
			t.Fatal("want error for blank project id, got nil")
		}
	})

	t.Run("invalid space id is rejected", func(t *testing.T) {
		t.Parallel()
		s := newTestSpaceStore(t)
		if _, err := s.DeleteProjectCascade(context.Background(), "../escape", "loom"); err == nil {
			t.Fatal("want error for invalid space id, got nil")
		}
	})
}
