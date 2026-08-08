package store

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func seedNote(t *testing.T, s *SpaceStore, id string) NoteItem {
	t.Helper()
	n, err := s.CreateNote(context.Background(), "sandbox", NoteItem{
		ID: id, Title: "misfiled thought", Body: "should have been a task",
		CreatedAt: "2026-08-08",
	})
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	return n
}

func TestConvertNoteReplacesTheNote(t *testing.T) {
	t.Parallel()
	s := newTestSpaceStore(t)
	ctx := context.Background()
	seedNote(t, s, "note-conv-1")

	got, err := s.ConvertNote(ctx, "sandbox", "note-conv-1", Conversion{
		Kind: "task",
		Task: &TaskItem{ID: "task-from-note", Title: "misfiled thought", Status: "open", CreatedAt: "2026-08-08"},
	})
	if err != nil {
		t.Fatalf("ConvertNote: %v", err)
	}
	if got.ID != "note-conv-1" {
		t.Errorf("returned note = %q, want the one that was replaced", got.ID)
	}

	if _, err := s.GetNote(ctx, "sandbox", "note-conv-1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("source note still present after conversion (err %v)", err)
	}
	task, err := s.GetTask(ctx, "sandbox", "task-from-note")
	if err != nil {
		t.Fatalf("target task missing: %v", err)
	}
	if task.Title != "misfiled thought" {
		t.Errorf("task title = %q, want the note's", task.Title)
	}
}

// The transaction is the whole point: a note must never be destroyed without
// its replacement existing. A duplicate target id is the easiest way to make
// the insert fail after the delete has already run.
func TestConvertNoteRollsBackOnFailedInsert(t *testing.T) {
	t.Parallel()
	s := newTestSpaceStore(t)
	ctx := context.Background()
	seedNote(t, s, "note-conv-2")
	if _, err := s.CreateTask(ctx, "sandbox", TaskItem{
		ID: "task-taken", Title: "already here", Status: "open", CreatedAt: "2026-08-08",
	}); err != nil {
		t.Fatal(err)
	}

	_, err := s.ConvertNote(ctx, "sandbox", "note-conv-2", Conversion{
		Kind: "task",
		Task: &TaskItem{ID: "task-taken", Title: "clash", Status: "open", CreatedAt: "2026-08-08"},
	})
	if err == nil {
		t.Fatal("expected the duplicate target id to fail the conversion")
	}
	// The note must still be there — losing it would be losing content.
	if _, err := s.GetNote(ctx, "sandbox", "note-conv-2"); err != nil {
		t.Errorf("note was destroyed by a failed conversion: %v", err)
	}
	if task, err := s.GetTask(ctx, "sandbox", "task-taken"); err != nil || task.Title != "already here" {
		t.Errorf("the pre-existing task was disturbed: %+v (err %v)", task, err)
	}
}

func TestConvertNoteRejectsUnsuitableKinds(t *testing.T) {
	t.Parallel()
	s := newTestSpaceStore(t)
	ctx := context.Background()

	tests := []struct {
		name string
		conv Conversion
	}{
		{"note to note is an edit, not a conversion", Conversion{
			Kind: "note", Note: &NoteItem{ID: "n2", Title: "t", CreatedAt: "2026-08-08"},
		}},
		{"note to project is not a sensible target", Conversion{
			Kind: "project", Project: &Project{ID: "p2", Name: "P", Status: "active"},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			id := "note-kind-" + strings.ReplaceAll(tt.conv.Kind, " ", "")
			seedNote(t, s, id)
			_, err := s.ConvertNote(ctx, "sandbox", id, tt.conv)
			if err == nil {
				t.Fatalf("converting a note to %q should be refused", tt.conv.Kind)
			}
			// And the note survives being refused.
			if _, err := s.GetNote(ctx, "sandbox", id); err != nil {
				t.Errorf("note removed despite the conversion being refused: %v", err)
			}
		})
	}
}

func TestConvertNoteUnknownID(t *testing.T) {
	t.Parallel()
	s := newTestSpaceStore(t)
	_, err := s.ConvertNote(context.Background(), "sandbox", "note-nope", Conversion{
		Kind: "task",
		Task: &TaskItem{ID: "t-x", Title: "x", Status: "open", CreatedAt: "2026-08-08"},
	})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}
