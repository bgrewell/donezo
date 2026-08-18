package store

import (
	"reflect"
	"testing"
)

func TestProjectPositionOrdering(t *testing.T) {
	t.Parallel()
	s, ctx := newCatchAllStore(t)

	mk := func(id string) Project {
		p, err := s.CreateProject(ctx, "sp", Project{
			ID: id, Name: id, Color: "blue", Status: "active",
			AltNextActions: []string{}, Tags: []string{},
		})
		if err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
		return p
	}
	// ids lists non-catchall project ids in stored (position) order.
	ids := func() []string {
		list, err := s.ListProjects(ctx, "sp")
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		out := []string{}
		for _, p := range list {
			if !p.Catchall {
				out = append(out, p.ID)
			}
		}
		return out
	}
	setPos := func(id string, pos int) {
		if _, err := s.PatchProject(ctx, "sp", id, func(p *Project) error {
			p.Position = pos
			return nil
		}); err != nil {
			t.Fatalf("patch %s: %v", id, err)
		}
	}

	// Auto-append: each new project lands one past the current max.
	a, b, c := mk("a"), mk("b"), mk("c")
	if a.Position != 0 || b.Position != 1 || c.Position != 2 {
		t.Fatalf("auto positions = %d,%d,%d, want 0,1,2", a.Position, b.Position, c.Position)
	}
	if got := ids(); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Errorf("append order = %v, want [a b c]", got)
	}

	// Reorder by rewriting positions (as a drag does): reverse to c, b, a.
	setPos("a", 2)
	setPos("c", 0)
	if got := ids(); !reflect.DeepEqual(got, []string{"c", "b", "a"}) {
		t.Errorf("reordered = %v, want [c b a]", got)
	}

	// A further new project still appends past the current max, not to the top.
	d := mk("d")
	if d.Position != 3 {
		t.Errorf("new project position = %d, want 3 (append)", d.Position)
	}
	if got := ids(); got[len(got)-1] != "d" {
		t.Errorf("new project not appended: %v", got)
	}
}
