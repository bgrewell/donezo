package store

import (
	"context"
	"errors"
	"testing"
)

// newCatchAllStore returns a store with one migrated space ready for use.
func newCatchAllStore(t *testing.T) (*SpaceStore, context.Context) {
	t.Helper()
	s, err := NewSpaceStore(WithDataDir(t.TempDir()), WithClock(fixedClock))
	if err != nil {
		t.Fatalf("NewSpaceStore: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	ctx := context.Background()
	if err := s.EnsureSpace(ctx, "sp"); err != nil {
		t.Fatalf("EnsureSpace: %v", err)
	}
	return s, ctx
}

// countCatchAll returns how many catch-all projects a space holds.
func countCatchAll(t *testing.T, s *SpaceStore, ctx context.Context) int {
	t.Helper()
	projects, err := s.ListProjects(ctx, "sp")
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	n := 0
	for _, p := range projects {
		if p.Catchall {
			n++
		}
	}
	return n
}

func TestGetOrCreateCatchAll(t *testing.T) {
	t.Parallel()
	s, ctx := newCatchAllStore(t)

	first, err := s.GetOrCreateCatchAll(ctx, "sp")
	if err != nil {
		t.Fatalf("GetOrCreateCatchAll: %v", err)
	}
	if !first.Catchall || first.Name != "Miscellaneous" || first.Color != "steel" || first.Status != "active" {
		t.Errorf("catch-all = %+v", first)
	}

	// Idempotent: a second call returns the same row, not a new project.
	second, err := s.GetOrCreateCatchAll(ctx, "sp")
	if err != nil {
		t.Fatalf("GetOrCreateCatchAll again: %v", err)
	}
	if second.ID != first.ID {
		t.Errorf("second catch-all id = %q, want %q", second.ID, first.ID)
	}
	if n := countCatchAll(t, s, ctx); n != 1 {
		t.Errorf("catch-all count = %d, want 1", n)
	}
}

func TestCreateActivityRoutesEmptyProjectToCatchAll(t *testing.T) {
	t.Parallel()
	s, ctx := newCatchAllStore(t)

	// An activity with no project is filed under the catch-all, created lazily.
	a1, err := s.CreateActivity(ctx, "sp", ActivityEntry{
		ID: "act-1", Date: "2026-08-18", Type: "work", Title: "tidied the desk",
		Source: "manual", Tags: []string{}, Links: []ActivityLink{},
	})
	if err != nil {
		t.Fatalf("CreateActivity(no project): %v", err)
	}
	catch, err := s.GetOrCreateCatchAll(ctx, "sp")
	if err != nil {
		t.Fatalf("GetOrCreateCatchAll: %v", err)
	}
	if a1.ProjectID != catch.ID {
		t.Errorf("unparented activity project = %q, want catch-all %q", a1.ProjectID, catch.ID)
	}

	// A second unparented activity reuses the same catch-all.
	if _, err := s.CreateActivity(ctx, "sp", ActivityEntry{
		ID: "act-2", Date: "2026-08-18", Type: "work", Title: "watered the plants",
		Source: "manual", Tags: []string{}, Links: []ActivityLink{},
	}); err != nil {
		t.Fatalf("CreateActivity(no project) again: %v", err)
	}
	if n := countCatchAll(t, s, ctx); n != 1 {
		t.Errorf("catch-all count after two unparented activities = %d, want 1", n)
	}

	// An activity naming a real project is untouched by the routing.
	if _, err := s.CreateProject(ctx, "sp", Project{
		ID: "loom", Name: "Loom", Color: "blue", Status: "active",
		AltNextActions: []string{}, Tags: []string{},
	}); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	a3, err := s.CreateActivity(ctx, "sp", ActivityEntry{
		ID: "act-3", ProjectID: "loom", Date: "2026-08-18", Type: "work", Title: "shipped v1",
		Source: "manual", Tags: []string{}, Links: []ActivityLink{},
	})
	if err != nil {
		t.Fatalf("CreateActivity(loom): %v", err)
	}
	if a3.ProjectID != "loom" {
		t.Errorf("explicit-project activity was rerouted to %q", a3.ProjectID)
	}
}

func TestCatchAllOneLivePerSpace(t *testing.T) {
	t.Parallel()
	s, ctx := newCatchAllStore(t)

	if _, err := s.GetOrCreateCatchAll(ctx, "sp"); err != nil {
		t.Fatalf("GetOrCreateCatchAll: %v", err)
	}
	// A second live catch-all is rejected by the partial unique index — the
	// invariant GetOrCreateCatchAll relies on to converge concurrent callers.
	db, err := s.db(ctx, "sp")
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	_, err = s.insertProject(ctx, db, Project{
		ID: "proj-dup", Name: "Dup", Color: "steel", Status: "active",
		AltNextActions: []string{}, Tags: []string{}, Catchall: true,
	})
	if !errors.Is(err, ErrDuplicateID) {
		t.Errorf("second live catch-all err = %v, want ErrDuplicateID", err)
	}
}
