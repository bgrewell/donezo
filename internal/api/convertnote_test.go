package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

const seedNoteBody = `{"id":"note-c1","title":"misfiled thought","body":"should be a task","createdAt":"2026-08-08"}`

func seedConvertibleNote(t *testing.T, h http.Handler, body string) {
	t.Helper()
	if rec := doJSON(t, h, http.MethodPost, "/api/spaces/sandbox/notes", body); rec.Code != http.StatusCreated {
		t.Fatalf("seed note = %d (body %s)", rec.Code, rec.Body)
	}
}

func noteExists(t *testing.T, h http.Handler, id string) bool {
	t.Helper()
	rec := doJSON(t, h, http.MethodGet, "/api/spaces/sandbox/state", "")
	var st struct {
		Notes []struct {
			ID string `json:"id"`
		} `json:"notes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("parse state: %v", err)
	}
	for _, n := range st.Notes {
		if n.ID == id {
			return true
		}
	}
	return false
}

func TestConvertNoteToTask(t *testing.T) {
	t.Parallel()
	h := newTestServer(t).Handler()
	seedConvertibleNote(t, h, seedNoteBody)

	rec := doJSON(t, h, http.MethodPost, "/api/spaces/sandbox/notes/note-c1/convert",
		`{"kind":"task","task":{"id":"task-c1","title":"misfiled thought","status":"open","createdAt":"2026-08-08"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("convert = %d (body %s)", rec.Code, rec.Body)
	}
	// Assert against stored state, not the handler's echo: the echo would
	// happily report a conversion that never committed.
	if noteExists(t, h, "note-c1") {
		t.Error("the source note survived the conversion")
	}
	state := doJSON(t, h, http.MethodGet, "/api/spaces/sandbox/state", "")
	if !jsonHasID(t, state.Body.Bytes(), "tasks", "task-c1") {
		t.Error("the task was not created")
	}
}

// jsonHasID reports whether a state collection contains an id.
func jsonHasID(t *testing.T, body []byte, collection, id string) bool {
	t.Helper()
	var st map[string][]map[string]any
	if err := json.Unmarshal(body, &st); err != nil {
		t.Fatalf("parse state: %v", err)
	}
	for _, row := range st[collection] {
		if row["id"] == id {
			return true
		}
	}
	return false
}

// The bodies here carry no payload member on purpose. The handler's request
// struct has no note or project field, so sending one is rejected by the
// decoder's DisallowUnknownFields before any kind logic runs — the refusal
// would look identical while proving nothing about the restriction. Asserting
// the message, not just the 400, is what separates this guard from the
// generic five-kind validator underneath it.
func TestConvertNoteRejectsUnsuitableKinds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
	}{
		{"note", `{"kind":"note"}`},
		{"project", `{"kind":"project"}`},
		{"nonsense", `{"kind":"banana"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := newTestServer(t).Handler()
			seedConvertibleNote(t, h, seedNoteBody)
			rec := doJSON(t, h, http.MethodPost, "/api/spaces/sandbox/notes/note-c1/convert", tt.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("kind %q = %d, want 400 (body %s)", tt.name, rec.Code, rec.Body)
			}
			// The message must name what a note can become. Falling through to
			// the shared validator would advertise all five kinds, telling the
			// caller that note and project are legal targets here.
			if got := rec.Body.String(); !strings.Contains(got, "kind must be one of task, reminder, activity") {
				t.Errorf("body = %s, want it to name only the note targets", got)
			}
			if !noteExists(t, h, "note-c1") {
				t.Error("note removed despite the conversion being refused")
			}
		})
	}
}

// A payload that does not match the kind must be refused rather than
// dereferenced — the handler passes the request straight to validateConversion
// and then to the store, both of which assume the pointer for the named kind
// is set.
func TestConvertNoteRejectsMismatchedPayload(t *testing.T) {
	t.Parallel()
	h := newTestServer(t).Handler()
	seedConvertibleNote(t, h, seedNoteBody)

	rec := doJSON(t, h, http.MethodPost, "/api/spaces/sandbox/notes/note-c1/convert",
		`{"kind":"reminder","task":{"id":"t9","title":"x","status":"open","createdAt":"2026-08-08"}}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("mismatched payload = %d, want 400 (body %s)", rec.Code, rec.Body)
	}
	if !noteExists(t, h, "note-c1") {
		t.Error("note removed despite the conversion being refused")
	}
}

// Ownership is per space: another user's space must be indistinguishable from
// one that does not exist, on this route as on every other note route.
func TestConvertNoteForeignSpace(t *testing.T) {
	t.Parallel()
	h := newTestServer(t).Handler()
	rec := doJSON(t, h, http.MethodPost, "/api/spaces/private/notes/note-c1/convert",
		`{"kind":"task","task":{"id":"t9","title":"x","status":"open","createdAt":"2026-08-08"}}`)
	if rec.Code != http.StatusNotFound {
		t.Errorf("foreign space = %d, want 404 (body %s)", rec.Code, rec.Body)
	}
}

// The transaction has to hold across the HTTP boundary too: a refused insert
// must leave the note where it was rather than destroying content.
func TestConvertNoteDuplicateTargetKeepsTheNote(t *testing.T) {
	t.Parallel()
	h := newTestServer(t).Handler()
	seedConvertibleNote(t, h, seedNoteBody)
	if rec := doJSON(t, h, http.MethodPost, "/api/spaces/sandbox/tasks",
		`{"id":"task-taken","title":"already here","status":"open","createdAt":"2026-08-08"}`); rec.Code != http.StatusCreated {
		t.Fatalf("seed task = %d (body %s)", rec.Code, rec.Body)
	}

	rec := doJSON(t, h, http.MethodPost, "/api/spaces/sandbox/notes/note-c1/convert",
		`{"kind":"task","task":{"id":"task-taken","title":"clash","status":"open","createdAt":"2026-08-08"}}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate target = %d, want 409 (body %s)", rec.Code, rec.Body)
	}
	if !noteExists(t, h, "note-c1") {
		t.Error("note destroyed by a conversion that could not complete")
	}
}

func TestConvertNoteUnknownNote(t *testing.T) {
	t.Parallel()
	h := newTestServer(t).Handler()
	rec := doJSON(t, h, http.MethodPost, "/api/spaces/sandbox/notes/note-nope/convert",
		`{"kind":"task","task":{"id":"t9","title":"x","status":"open","createdAt":"2026-08-08"}}`)
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown note = %d, want 404 (body %s)", rec.Code, rec.Body)
	}
}

// Converting is a write, so a watching browser must be told.
func TestConvertNoteBumpsRevision(t *testing.T) {
	t.Parallel()
	h := newTestServer(t).Handler()
	seedConvertibleNote(t, h, seedNoteBody)
	before := revisionOf(t, h, "sandbox")
	if rec := doJSON(t, h, http.MethodPost, "/api/spaces/sandbox/notes/note-c1/convert",
		`{"kind":"task","task":{"id":"task-c2","title":"x","status":"open","createdAt":"2026-08-08"}}`); rec.Code != http.StatusOK {
		t.Fatalf("convert = %d (body %s)", rec.Code, rec.Body)
	}
	if after := revisionOf(t, h, "sandbox"); after <= before {
		t.Errorf("revision %d -> %d, want it to move", before, after)
	}
}
