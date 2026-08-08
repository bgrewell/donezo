package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

// revisionOf reads a space's current revision through the API.
func revisionOf(t *testing.T, h http.Handler, spaceID string) uint64 {
	t.Helper()
	rec := doJSON(t, h, http.MethodGet, "/api/spaces/"+spaceID+"/revision", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("revision = %d (body %s)", rec.Code, rec.Body)
	}
	var got struct {
		Revision uint64 `json:"revision"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("parse revision: %v", err)
	}
	return got.Revision
}

func TestSpaceRevisionStartsAtZero(t *testing.T) {
	t.Parallel()
	h := newTestServer(t).Handler()
	if got := revisionOf(t, h, "sandbox"); got != 0 {
		t.Errorf("revision = %d, want 0 for an untouched space", got)
	}
}

// The whole point is that a successful write moves the number and nothing
// else does — a client refetches on every move, so a bump for a request that
// changed nothing is wasted work on every open tab.
func TestSpaceRevisionMovesOnlyOnSuccessfulWrites(t *testing.T) {
	t.Parallel()
	taskBody := `{"id":"rev-1","title":"probe","status":"open","createdAt":"2026-08-08"}`

	tests := []struct {
		name     string
		method   string
		path     string
		body     string
		wantBump bool
	}{
		{
			name: "successful create", method: http.MethodPost,
			path: "/api/spaces/sandbox/tasks", body: taskBody, wantBump: true,
		},
		{
			name: "rejected create changes nothing", method: http.MethodPost,
			path: "/api/spaces/sandbox/tasks", body: `{"id":"","title":""}`, wantBump: false,
		},
		{
			name: "read does not bump", method: http.MethodGet,
			path: "/api/spaces/sandbox/state", body: "", wantBump: false,
		},
		{
			name:   "write to an unknown space does not bump the known one",
			method: http.MethodPost, path: "/api/spaces/nope/tasks",
			body: taskBody, wantBump: false,
		},
		{
			name: "duplicate id is refused and does not bump", method: http.MethodPost,
			path: "/api/spaces/sandbox/tasks", body: taskBody, wantBump: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := newTestServer(t).Handler()
			// The duplicate case needs the row to exist first.
			if tt.name == "duplicate id is refused and does not bump" {
				if rec := doJSON(t, h, http.MethodPost, "/api/spaces/sandbox/tasks", taskBody); rec.Code != http.StatusCreated {
					t.Fatalf("seed = %d (body %s)", rec.Code, rec.Body)
				}
			}
			before := revisionOf(t, h, "sandbox")
			doJSON(t, h, tt.method, tt.path, tt.body)
			after := revisionOf(t, h, "sandbox")

			if bumped := after > before; bumped != tt.wantBump {
				t.Errorf("revision %d -> %d (bumped=%v), want bumped=%v",
					before, after, bumped, tt.wantBump)
			}
		})
	}
}

// Counters are per space: a client polling one space must not be woken by
// work happening in another.
func TestSpaceRevisionIsPerSpace(t *testing.T) {
	t.Parallel()
	h := newTestServer(t).Handler()

	rec := doJSON(t, h, http.MethodPost, "/api/spaces", `{"name":"Other","color":"green"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create space = %d (body %s)", rec.Code, rec.Body)
	}
	var created struct {
		Space struct {
			ID string `json:"id"`
		} `json:"space"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.Space.ID == "" {
		t.Fatalf("could not read the new space id from %s (err %v)", rec.Body, err)
	}

	otherBefore := revisionOf(t, h, created.Space.ID)
	sandboxBefore := revisionOf(t, h, "sandbox")

	if rec := doJSON(t, h, http.MethodPost, "/api/spaces/sandbox/tasks",
		`{"id":"rev-per","title":"probe","status":"open","createdAt":"2026-08-08"}`); rec.Code != http.StatusCreated {
		t.Fatalf("create task = %d (body %s)", rec.Code, rec.Body)
	}

	if after := revisionOf(t, h, "sandbox"); after <= sandboxBefore {
		t.Errorf("sandbox revision %d -> %d, want it to move", sandboxBefore, after)
	}
	if after := revisionOf(t, h, created.Space.ID); after != otherBefore {
		t.Errorf("other space revision %d -> %d, want it untouched", otherBefore, after)
	}
}

func TestSpaceRevisionRequiresOwnership(t *testing.T) {
	t.Parallel()
	h := newTestServer(t).Handler()
	rec := doJSON(t, h, http.MethodGet, "/api/spaces/not-a-space/revision", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown space = %d, want 404 (body %s)", rec.Code, rec.Body)
	}
}

func TestSpaceIDFromPath(t *testing.T) {
	t.Parallel()
	tests := []struct{ path, want string }{
		{"/api/spaces/sandbox/tasks", "sandbox"},
		{"/api/spaces/sandbox/inbox/inb-1/convert", "sandbox"},
		{"/api/spaces/sandbox", "sandbox"},
		{"/api/spaces", ""},
		{"/api/settings", ""},
		{"/mcp", ""},
		{"/", ""},
	}
	for _, tt := range tests {
		if got := spaceIDFromPath(tt.path); got != tt.want {
			t.Errorf("spaceIDFromPath(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}
