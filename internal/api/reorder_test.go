package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// TestReorderProjects covers the atomic PATCH /projects/reorder: the ordered
// id list becomes the projects' positions in one request.
func TestReorderProjects(t *testing.T) {
	t.Parallel()
	h := newTestServer(t).Handler()
	// sandbox seeds "loom"; add two more to reorder.
	for _, id := range []string{"proj-a", "proj-b"} {
		body := fmt.Sprintf(
			`{"id":%q,"name":%q,"color":"green","purpose":"","outcome":"","currentFocus":"",`+
				`"nextAction":"","altNextActions":[],"status":"active","resumeContext":"","tags":[]}`, id, id)
		if rec := doJSON(t, h, http.MethodPost, "/api/spaces/sandbox/projects", body); rec.Code != http.StatusCreated {
			t.Fatalf("seed %s = %d (%s)", id, rec.Code, rec.Body)
		}
	}

	rec := doJSON(t, h, http.MethodPatch, "/api/spaces/sandbox/projects/reorder",
		`{"order":["proj-b","loom","proj-a"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("reorder = %d (%s)", rec.Code, rec.Body)
	}

	state := getState(t, h, "sandbox")
	var projects []struct {
		ID       string `json:"id"`
		Position int    `json:"position"`
	}
	if err := json.Unmarshal(state["projects"], &projects); err != nil {
		t.Fatalf("parse projects: %v", err)
	}
	pos := map[string]int{}
	for _, p := range projects {
		pos[p.ID] = p.Position
	}
	if !(pos["proj-b"] < pos["loom"] && pos["loom"] < pos["proj-a"]) {
		t.Errorf("reorder not applied: positions = %+v", pos)
	}
}

// TestCreateProjectRejectsNegativePosition guards the create path's position
// check, matching the PATCH path.
func TestCreateProjectRejectsNegativePosition(t *testing.T) {
	t.Parallel()
	h := newTestServer(t).Handler()
	body := `{"id":"proj-neg","name":"Neg","color":"green","purpose":"","outcome":"","currentFocus":"",` +
		`"nextAction":"","altNextActions":[],"status":"active","resumeContext":"","tags":[],"position":-5}`
	rec := doJSON(t, h, http.MethodPost, "/api/spaces/sandbox/projects", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("negative position = %d, want 400 (%s)", rec.Code, rec.Body)
	}
}
