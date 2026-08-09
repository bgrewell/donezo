package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

// #44: tasks and reminders carry an optional details field so the short form
// can stay short. These assert it survives creation, patching, and clearing —
// through stored state, since the handler's echo would report a details field
// that never reached the database.

// stateEntity returns one row from a state collection by id.
func stateEntity(t *testing.T, h http.Handler, collection, id string) map[string]any {
	t.Helper()
	rec := doJSON(t, h, http.MethodGet, "/api/spaces/sandbox/state", "")
	var st map[string][]map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("parse state: %v", err)
	}
	for _, row := range st[collection] {
		if row["id"] == id {
			return row
		}
	}
	t.Fatalf("%s %q not found in state", collection, id)
	return nil
}

func TestTaskDetailsRoundTripAndPatch(t *testing.T) {
	t.Parallel()
	h := newTestServer(t).Handler()

	if rec := doJSON(t, h, http.MethodPost, "/api/spaces/sandbox/tasks",
		`{"id":"tsk-d1","title":"Short title","details":"The long version.\nOver two lines.","status":"open","createdAt":"2026-08-09"}`,
	); rec.Code != http.StatusCreated {
		t.Fatalf("create = %d (body %s)", rec.Code, rec.Body)
	}
	if got := stateEntity(t, h, "tasks", "tsk-d1")["details"]; got != "The long version.\nOver two lines." {
		t.Errorf("details = %v after create", got)
	}

	// Patching the title alone must not disturb details — the whole point is
	// that the two are independent.
	if rec := doJSON(t, h, http.MethodPatch, "/api/spaces/sandbox/tasks/tsk-d1",
		`{"title":"Shorter"}`); rec.Code != http.StatusOK {
		t.Fatalf("patch title = %d (body %s)", rec.Code, rec.Body)
	}
	row := stateEntity(t, h, "tasks", "tsk-d1")
	if row["title"] != "Shorter" {
		t.Errorf("title = %v", row["title"])
	}
	if row["details"] != "The long version.\nOver two lines." {
		t.Errorf("patching the title disturbed details: %v", row["details"])
	}

	if rec := doJSON(t, h, http.MethodPatch, "/api/spaces/sandbox/tasks/tsk-d1",
		`{"details":"Replaced."}`); rec.Code != http.StatusOK {
		t.Fatalf("patch details = %d (body %s)", rec.Code, rec.Body)
	}
	if got := stateEntity(t, h, "tasks", "tsk-d1")["details"]; got != "Replaced." {
		t.Errorf("details = %v after patch", got)
	}

	// Empty string is how it is cleared; there is no null form.
	if rec := doJSON(t, h, http.MethodPatch, "/api/spaces/sandbox/tasks/tsk-d1",
		`{"details":""}`); rec.Code != http.StatusOK {
		t.Fatalf("clear = %d (body %s)", rec.Code, rec.Body)
	}
	if got := stateEntity(t, h, "tasks", "tsk-d1")["details"]; got != "" {
		t.Errorf("details = %v after clearing, want empty", got)
	}
}

func TestReminderDetailsRoundTripAndPatch(t *testing.T) {
	t.Parallel()
	h := newTestServer(t).Handler()

	if rec := doJSON(t, h, http.MethodPost, "/api/spaces/sandbox/reminders",
		`{"id":"rem-d1","text":"Ping Dan","details":"About the RAN550 licence.","remindAt":"2026-08-20T09:00:00"}`,
	); rec.Code != http.StatusCreated {
		t.Fatalf("create = %d (body %s)", rec.Code, rec.Body)
	}
	if got := stateEntity(t, h, "reminders", "rem-d1")["details"]; got != "About the RAN550 licence." {
		t.Errorf("details = %v after create", got)
	}

	if rec := doJSON(t, h, http.MethodPatch, "/api/spaces/sandbox/reminders/rem-d1",
		`{"done":true}`); rec.Code != http.StatusOK {
		t.Fatalf("patch done = %d (body %s)", rec.Code, rec.Body)
	}
	if got := stateEntity(t, h, "reminders", "rem-d1")["details"]; got != "About the RAN550 licence." {
		t.Errorf("marking done disturbed details: %v", got)
	}

	if rec := doJSON(t, h, http.MethodPatch, "/api/spaces/sandbox/reminders/rem-d1",
		`{"details":""}`); rec.Code != http.StatusOK {
		t.Fatalf("clear = %d (body %s)", rec.Code, rec.Body)
	}
	if got := stateEntity(t, h, "reminders", "rem-d1")["details"]; got != "" {
		t.Errorf("details = %v after clearing", got)
	}
}

// An item created without details must read back as empty rather than absent,
// so a client never has to tell the two apart.
func TestDetailsIsAlwaysPresentOnTheWire(t *testing.T) {
	t.Parallel()
	h := newTestServer(t).Handler()
	if rec := doJSON(t, h, http.MethodPost, "/api/spaces/sandbox/tasks",
		`{"id":"tsk-d2","title":"No details","status":"open","createdAt":"2026-08-09"}`); rec.Code != http.StatusCreated {
		t.Fatalf("create task = %d (body %s)", rec.Code, rec.Body)
	}
	if rec := doJSON(t, h, http.MethodPost, "/api/spaces/sandbox/reminders",
		`{"id":"rem-d2","text":"No details","remindAt":"2026-08-20T09:00:00"}`); rec.Code != http.StatusCreated {
		t.Fatalf("create reminder = %d (body %s)", rec.Code, rec.Body)
	}
	for _, tc := range []struct{ collection, id string }{
		{"tasks", "tsk-d2"}, {"reminders", "rem-d2"},
	} {
		row := stateEntity(t, h, tc.collection, tc.id)
		got, present := row["details"]
		if !present {
			t.Errorf("%s: details absent from the wire, want an empty string", tc.collection)
		} else if got != "" {
			t.Errorf("%s: details = %v, want empty", tc.collection, got)
		}
	}
}
