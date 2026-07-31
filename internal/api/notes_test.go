package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/bgrewell/donezo/internal/store"
)

// fetchNote reads one note back out of the space state, so an assertion can
// check what was stored rather than what a handler echoed.
func fetchNote(t *testing.T, h http.Handler, id string) store.NoteItem {
	t.Helper()
	rec := doJSON(t, h, http.MethodGet, "/api/spaces/sandbox/state", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("state = %d (body %s)", rec.Code, rec.Body)
	}
	var state struct {
		Notes []store.NoteItem `json:"notes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &state); err != nil {
		t.Fatalf("parse state: %v", err)
	}
	for _, n := range state.Notes {
		if n.ID == id {
			return n
		}
	}
	t.Fatalf("note %s not found in space state", id)
	return store.NoteItem{}
}

// seedNote creates the note the note-route tests operate on.
func seedNote(t *testing.T, h http.Handler) {
	t.Helper()
	if rec := doJSON(t, h, http.MethodPost, "/api/spaces/sandbox/notes",
		`{"id":"note-1","title":"Original","body":"Original body",`+
			`"projectId":"loom","createdAt":"2026-07-26"}`); rec.Code != http.StatusCreated {
		t.Fatalf("seed note = %d (body %s)", rec.Code, rec.Body)
	}
}

func TestPatchNote(t *testing.T) {
	t.Parallel()
	h := newTestServer(t).Handler()
	seedNote(t, h)

	// A patch changes only what it names.
	rec := doJSON(t, h, http.MethodPatch, "/api/spaces/sandbox/notes/note-1",
		`{"title":"Revised"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch title = %d (body %s)", rec.Code, rec.Body)
	}
	var got store.NoteItem
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.Title != "Revised" {
		t.Errorf("title = %q, want Revised", got.Title)
	}
	if got.Body != "Original body" {
		t.Errorf("untouched body changed to %q", got.Body)
	}
	if got.ProjectID == nil || *got.ProjectID != "loom" {
		t.Errorf("untouched projectId = %v, want loom", got.ProjectID)
	}

	// null detaches the note from its project; the create route allows a
	// project-less note, so the patch route must allow reaching that state.
	//
	// Decoded into a fresh value on purpose: projectId is omitempty, so a
	// cleared project is absent from the response rather than null, and
	// reusing the struct above would leave its stale pointer in place and
	// pass whether or not the clear actually happened.
	rec = doJSON(t, h, http.MethodPatch, "/api/spaces/sandbox/notes/note-1",
		`{"projectId":null}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("detach = %d (body %s)", rec.Code, rec.Body)
	}
	var detached store.NoteItem
	if err := json.Unmarshal(rec.Body.Bytes(), &detached); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if detached.ProjectID != nil {
		t.Errorf("projectId should clear to nil, got %q", *detached.ProjectID)
	}
	// And it is really stored that way, not just echoed.
	if stored := fetchNote(t, h, "note-1"); stored.ProjectID != nil {
		t.Errorf("stored projectId = %q, want nil", *stored.ProjectID)
	}

	// An emptied body is still a valid note — the create route does not
	// require a body either, and the patch route must not be stricter.
	rec = doJSON(t, h, http.MethodPatch, "/api/spaces/sandbox/notes/note-1", `{"body":""}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("empty body = %d (body %s), want 200", rec.Code, rec.Body)
	}
}

func TestPatchNoteRejectsBadInput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		path string
		body string
		want int
	}{
		{"empty title", "/api/spaces/sandbox/notes/note-1", `{"title":""}`, http.StatusBadRequest},
		{"empty projectId", "/api/spaces/sandbox/notes/note-1", `{"projectId":""}`, http.StatusBadRequest},
		{"bad createdAt", "/api/spaces/sandbox/notes/note-1", `{"createdAt":"yesterday"}`, http.StatusBadRequest},
		{"wrong type", "/api/spaces/sandbox/notes/note-1", `{"title":42}`, http.StatusBadRequest},
		{"unknown field", "/api/spaces/sandbox/notes/note-1", `{"colour":"red"}`, http.StatusBadRequest},
		{"unknown note", "/api/spaces/sandbox/notes/ghost", `{"title":"x"}`, http.StatusNotFound},
		{"missing project", "/api/spaces/sandbox/notes/note-1", `{"projectId":"ghost"}`, http.StatusBadRequest},
		{"foreign space", "/api/spaces/private/notes/note-1", `{"title":"x"}`, http.StatusNotFound},
	}
	for _, tt := range tests {
		tt := tt // capture (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := newTestServer(t).Handler()
			seedNote(t, h)
			rec := doJSON(t, h, http.MethodPatch, tt.path, tt.body)
			if rec.Code != tt.want {
				t.Errorf("status = %d, want %d (body %s)", rec.Code, tt.want, rec.Body)
			}
		})
	}
}

func TestDeleteNote(t *testing.T) {
	t.Parallel()
	h := newTestServer(t).Handler()
	seedNote(t, h)

	rec := doJSON(t, h, http.MethodDelete, "/api/spaces/sandbox/notes/note-1", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d, want 204 (body %s)", rec.Code, rec.Body)
	}
	if body := strings.TrimSpace(rec.Body.String()); body != "" {
		t.Errorf("204 should carry no body, got %s", body)
	}

	// Gone from the space state, not merely reported deleted.
	state := doJSON(t, h, http.MethodGet, "/api/spaces/sandbox/state", "")
	if state.Code != http.StatusOK {
		t.Fatalf("state = %d", state.Code)
	}
	var got struct {
		Notes []store.NoteItem `json:"notes"`
	}
	if err := json.Unmarshal(state.Body.Bytes(), &got); err != nil {
		t.Fatalf("parse state: %v", err)
	}
	for _, n := range got.Notes {
		if n.ID == "note-1" {
			t.Errorf("note still present after delete: %+v", n)
		}
	}

	// Deleting again reports not-found rather than succeeding, so a caller
	// can tell a real delete from a no-op.
	if rec := doJSON(t, h, http.MethodDelete, "/api/spaces/sandbox/notes/note-1", ""); rec.Code != http.StatusNotFound {
		t.Errorf("second delete = %d, want 404", rec.Code)
	}

	// A note in a space the caller does not own is indistinguishable from
	// one that does not exist.
	if rec := doJSON(t, h, http.MethodDelete, "/api/spaces/private/notes/note-1", ""); rec.Code != http.StatusNotFound {
		t.Errorf("foreign-space delete = %d, want 404", rec.Code)
	}
}
