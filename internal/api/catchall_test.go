package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/bgrewell/donezo/internal/store"
)

// TestEnsureCatchAllEndpoint covers POST /catchall: it lazily creates the
// space's "Miscellaneous" project and is idempotent.
func TestEnsureCatchAllEndpoint(t *testing.T) {
	t.Parallel()
	h := newTestServer(t).Handler()

	rec := doJSON(t, h, http.MethodPost, "/api/spaces/sandbox/catchall", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("ensure catch-all = %d (body %s)", rec.Code, rec.Body)
	}
	var first store.Project
	if err := json.Unmarshal(rec.Body.Bytes(), &first); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !first.Catchall || first.Name != "Miscellaneous" {
		t.Errorf("catch-all = %+v", first)
	}
	// Idempotent: a second call returns the same project, not a new one.
	rec2 := doJSON(t, h, http.MethodPost, "/api/spaces/sandbox/catchall", "")
	var second store.Project
	if err := json.Unmarshal(rec2.Body.Bytes(), &second); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if second.ID != first.ID {
		t.Errorf("second catch-all id = %q, want %q", second.ID, first.ID)
	}
}

// TestCreateActivityNoProjectRoutesToCatchAll covers the POST /activities path
// with an empty projectId: the store files it under the catch-all rather than
// rejecting it.
func TestCreateActivityNoProjectRoutesToCatchAll(t *testing.T) {
	t.Parallel()
	h := newTestServer(t).Handler()

	body := `{"id":"act-np","projectId":"","date":"2026-08-18","type":"work",` +
		`"title":"tidied the desk","details":"","source":"manual","tags":[],"links":[]}`
	rec := doJSON(t, h, http.MethodPost, "/api/spaces/sandbox/activities", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create activity no project = %d (body %s)", rec.Code, rec.Body)
	}
	var got store.ActivityEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.ProjectID == "" {
		t.Error("activity was stored with no project instead of the catch-all")
	}
}

// TestCreateProjectRejectsClientCatchall guards the fix that catchall is a
// system-reserved flag: a client cannot plant an arbitrary project as the
// space's catch-all through the public create path.
func TestCreateProjectRejectsClientCatchall(t *testing.T) {
	t.Parallel()
	h := newTestServer(t).Handler()

	body := `{"id":"proj-evil","name":"Evil","color":"green","purpose":"","outcome":"",` +
		`"currentFocus":"","nextAction":"","altNextActions":[],"status":"active",` +
		`"resumeContext":"","tags":[],"catchall":true}`
	rec := doJSON(t, h, http.MethodPost, "/api/spaces/sandbox/projects", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create project = %d (body %s)", rec.Code, rec.Body)
	}
	var got store.Project
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.Catchall {
		t.Error("client-supplied catchall:true was honored — a project must not be able to claim the catch-all flag")
	}
	// And it did not become the real catch-all: ensure creates a different one.
	ens := doJSON(t, h, http.MethodPost, "/api/spaces/sandbox/catchall", "")
	var catch store.Project
	if err := json.Unmarshal(ens.Body.Bytes(), &catch); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if catch.ID == "proj-evil" {
		t.Error("the injected project became the space's catch-all")
	}
}

// TestConvertToActivityRequiresProject guards the fix that keeps the relaxed
// activity validation from leaking to the conversion paths, which insert
// directly and cannot route to the catch-all.
func TestConvertToActivityRequiresProject(t *testing.T) {
	t.Parallel()
	h := newTestServer(t).Handler()
	seedConvertibleNote(t, h, seedNoteBody)

	body := `{"kind":"activity","activity":{"id":"act-cx","projectId":"","date":"2026-08-18",` +
		`"type":"work","title":"did a thing","details":"","source":"manual","tags":[],"links":[]}}`
	rec := doJSON(t, h, http.MethodPost, "/api/spaces/sandbox/notes/note-c1/convert", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("convert to activity with no project = %d, want 400 (body %s)", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "projectId") {
		t.Errorf("error should name projectId: %s", rec.Body)
	}
}
