package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/bgrewell/donezo/internal/store"
)

// stateBody is the raw space state, for the jsonHasID assertions.
func stateBody(t *testing.T, h http.Handler) []byte {
	t.Helper()
	return doJSON(t, h, http.MethodGet, "/api/spaces/sandbox/state", "").Body.Bytes()
}

// trashList reads the trash and returns its entries.
func trashList(t *testing.T, h http.Handler) []store.TrashItem {
	t.Helper()
	rec := doJSON(t, h, http.MethodGet, "/api/spaces/sandbox/trash", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET trash = %d (body %s)", rec.Code, rec.Body)
	}
	var out struct {
		Trash []store.TrashItem `json:"trash"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("parse trash: %v", err)
	}
	return out.Trash
}

// The round trip over HTTP: delete hides it, the trash shows it, restore
// brings it back — asserted against stored state each time.
func TestTrashRoundTripOverHTTP(t *testing.T) {
	t.Parallel()
	h := newTestServer(t).Handler()

	if rec := doJSON(t, h, http.MethodPost, "/api/spaces/sandbox/notes",
		`{"id":"note-t1","title":"Keep me","body":"b","createdAt":"2026-08-09"}`); rec.Code != http.StatusCreated {
		t.Fatalf("seed note = %d (body %s)", rec.Code, rec.Body)
	}
	if rec := doJSON(t, h, http.MethodDelete, "/api/spaces/sandbox/notes/note-t1", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d (body %s)", rec.Code, rec.Body)
	}
	if jsonHasID(t, stateBody(t, h), "notes", "note-t1") {
		t.Error("deleted note still in state")
	}

	trash := trashList(t, h)
	if len(trash) != 1 || trash[0].ID != "note-t1" || trash[0].Entity != store.TrashNote {
		t.Fatalf("trash = %+v", trash)
	}
	if trash[0].Label != "Keep me" {
		t.Errorf("label = %q, want the note's title so the view can say what it was", trash[0].Label)
	}

	if rec := doJSON(t, h, http.MethodPost,
		"/api/spaces/sandbox/trash/note/note-t1/restore", ""); rec.Code != http.StatusOK {
		t.Fatalf("restore = %d (body %s)", rec.Code, rec.Body)
	}
	if !jsonHasID(t, stateBody(t, h), "notes", "note-t1") {
		t.Error("restored note missing from state")
	}
	if got := trashList(t, h); len(got) != 0 {
		t.Errorf("trash = %+v after restore", got)
	}
}

// Deleting a project trashes its content as one batch, and restoring the
// project brings all of it back in one call.
func TestTrashRestoresAProjectBatchWhole(t *testing.T) {
	t.Parallel()
	h := newTestServer(t).Handler()
	for _, body := range []struct{ path, json string }{
		{"/api/spaces/sandbox/tasks", `{"id":"tsk-b1","projectId":"loom","title":"t","status":"open","createdAt":"2026-08-09"}`},
		{"/api/spaces/sandbox/notes", `{"id":"note-b1","projectId":"loom","title":"n","body":"b","createdAt":"2026-08-09"}`},
	} {
		if rec := doJSON(t, h, http.MethodPost, body.path, body.json); rec.Code != http.StatusCreated {
			t.Fatalf("seed %s = %d (body %s)", body.path, rec.Code, rec.Body)
		}
	}
	if rec := doJSON(t, h, http.MethodDelete, "/api/spaces/sandbox/projects/loom", ""); rec.Code != http.StatusOK {
		t.Fatalf("delete project = %d (body %s)", rec.Code, rec.Body)
	}

	// One entry, not three: the trash lists deletes, not rows. Restore and
	// purge act on the batch, so a row per member would put three identical
	// Restore buttons on screen and bury the project among its own content.
	trash := trashList(t, h)
	if len(trash) != 1 {
		t.Fatalf("trash = %+v, want one entry for the whole delete", trash)
	}
	if trash[0].Entity != store.TrashProject || trash[0].ID != "loom" {
		t.Errorf("entry = %s %s, want the project to represent its own delete", trash[0].Entity, trash[0].ID)
	}
	if trash[0].BatchSize != 3 {
		t.Errorf("batchSize = %d, want 3 so the view can say what restoring brings back", trash[0].BatchSize)
	}

	if rec := doJSON(t, h, http.MethodPost,
		"/api/spaces/sandbox/trash/project/loom/restore", ""); rec.Code != http.StatusOK {
		t.Fatalf("restore = %d (body %s)", rec.Code, rec.Body)
	}
	state := stateBody(t, h)
	for _, tc := range []struct{ collection, id string }{
		{"projects", "loom"}, {"tasks", "tsk-b1"}, {"notes", "note-b1"},
	} {
		if !jsonHasID(t, state, tc.collection, tc.id) {
			t.Errorf("%s %s not restored with the batch", tc.collection, tc.id)
		}
	}
}

func TestTrashPurgeAndEmpty(t *testing.T) {
	t.Parallel()
	h := newTestServer(t).Handler()
	for i, body := range []string{
		`{"id":"note-p1","title":"one","body":"b","createdAt":"2026-08-09"}`,
		`{"id":"note-p2","title":"two","body":"b","createdAt":"2026-08-09"}`,
	} {
		if rec := doJSON(t, h, http.MethodPost, "/api/spaces/sandbox/notes", body); rec.Code != http.StatusCreated {
			t.Fatalf("seed %d = %d (body %s)", i, rec.Code, rec.Body)
		}
	}
	for _, id := range []string{"note-p1", "note-p2"} {
		if rec := doJSON(t, h, http.MethodDelete, "/api/spaces/sandbox/notes/"+id, ""); rec.Code != http.StatusNoContent {
			t.Fatalf("delete %s = %d", id, rec.Code)
		}
	}

	if rec := doJSON(t, h, http.MethodDelete, "/api/spaces/sandbox/trash/note/note-p1", ""); rec.Code != http.StatusOK {
		t.Fatalf("purge = %d (body %s)", rec.Code, rec.Body)
	}
	if got := trashList(t, h); len(got) != 1 || got[0].ID != "note-p2" {
		t.Errorf("trash = %+v, want only the un-purged one", got)
	}
	// Purged means gone: restoring it is a 404, not a silent success.
	if rec := doJSON(t, h, http.MethodPost,
		"/api/spaces/sandbox/trash/note/note-p1/restore", ""); rec.Code != http.StatusNotFound {
		t.Errorf("restore after purge = %d, want 404 (body %s)", rec.Code, rec.Body)
	}

	if rec := doJSON(t, h, http.MethodPost, "/api/spaces/sandbox/trash/empty", ""); rec.Code != http.StatusOK {
		t.Fatalf("empty = %d (body %s)", rec.Code, rec.Body)
	}
	if got := trashList(t, h); len(got) != 0 {
		t.Errorf("trash = %+v after emptying", got)
	}
}

func TestTrashRejectsUnknownEntity(t *testing.T) {
	t.Parallel()
	h := newTestServer(t).Handler()
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/api/spaces/sandbox/trash/sandwich/x/restore"},
		{http.MethodDelete, "/api/spaces/sandbox/trash/sandwich/x"},
	} {
		rec := doJSON(t, h, tc.method, tc.path, "")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s %s = %d, want 400 (body %s)", tc.method, tc.path, rec.Code, rec.Body)
		}
		// Naming the legal entities beats a 404 that reads like the item is
		// simply missing.
		if !strings.Contains(rec.Body.String(), "entity must be one of") {
			t.Errorf("body = %s, want it to name the legal entities", rec.Body)
		}
	}
}

// The sweep is what makes this a trash rather than an archive.
func TestTrashSweepPurgesOnlyWhatIsOldEnough(t *testing.T) {
	t.Parallel()
	// Start level with the STORE's clock, which is what stamps deleted_at.
	// Starting later would have the item already expired before the first
	// sweep, and the "inside the window" assertion below would pass for the
	// wrong reason — it did, the first time this was written.
	now := fixedClock()
	srv := newTestServer(t,
		WithClock(func() time.Time { return now }),
		WithTrashRetention(7*24*time.Hour),
	)
	h := srv.Handler()

	if rec := doJSON(t, h, http.MethodPost, "/api/spaces/sandbox/notes",
		`{"id":"note-s1","title":"old","body":"b","createdAt":"2026-08-09"}`); rec.Code != http.StatusCreated {
		t.Fatalf("seed = %d (body %s)", rec.Code, rec.Body)
	}
	if rec := doJSON(t, h, http.MethodDelete, "/api/spaces/sandbox/notes/note-s1", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d", rec.Code)
	}

	// Straight away, the sweep must leave it: it is inside the window.
	srv.sweepTrash(context.Background())
	if got := trashList(t, h); len(got) != 1 {
		t.Fatalf("sweep purged something inside the retention window: %+v", got)
	}

	// Part way through the window is the case that makes the window itself
	// load-bearing. Without it, a sweep whose cutoff is simply "now" passes
	// both of the other assertions — it did, and the mutation survived.
	now = now.Add(3 * 24 * time.Hour)
	srv.sweepTrash(context.Background())
	if got := trashList(t, h); len(got) != 1 {
		t.Fatalf("sweep purged an item 3 days into a 7-day window: %+v", got)
	}

	// Move past the window and sweep again.
	now = now.Add(5 * 24 * time.Hour)
	srv.sweepTrash(context.Background())
	if got := trashList(t, h); len(got) != 0 {
		t.Errorf("trash = %+v, want the expired note purged", got)
	}
}

// Retention of zero means nothing is ever purged on a timer.
func TestTrashSweepDisabled(t *testing.T) {
	t.Parallel()
	// The clock runs ahead of the store's, so a sweep that ran at all would
	// purge: with retention 0 its cutoff is "now", and the item was stamped
	// an hour earlier. Without that gap this test passes whether or not the
	// disable is honoured.
	srv := newTestServer(t,
		WithClock(func() time.Time { return fixedClock().Add(time.Hour) }),
		WithTrashRetention(0),
	)
	h := srv.Handler()
	if rec := doJSON(t, h, http.MethodPost, "/api/spaces/sandbox/notes",
		`{"id":"note-d1","title":"kept","body":"b","createdAt":"2026-08-09"}`); rec.Code != http.StatusCreated {
		t.Fatalf("seed = %d", rec.Code)
	}
	if rec := doJSON(t, h, http.MethodDelete, "/api/spaces/sandbox/notes/note-d1", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d", rec.Code)
	}
	// Run the real entry point with a LIVE context. A cancelled one would
	// make this pass either way: the startup sweep would fail at ListSpaces
	// before purging anything, which is not the disable being honoured. With
	// the disable in place RunTrashSweep returns at once; without it, it
	// sweeps immediately and then waits on the ticker, so cancelling after a
	// beat leaves the evidence either way.
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.RunTrashSweep(ctx)
	}()
	time.Sleep(200 * time.Millisecond)
	cancel()
	<-done
	if got := trashList(t, h); len(got) != 1 {
		t.Errorf("trash = %+v, want the item kept with the sweep disabled", got)
	}
}
