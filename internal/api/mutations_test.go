package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/bgrewell/donezo/internal/store"
)

// doJSON runs one request against the handler, with body as the JSON
// payload when non-empty.
func doJSON(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rdr)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// getState fetches and parses a space's full state.
func getState(t *testing.T, h http.Handler, spaceID string) map[string]json.RawMessage {
	t.Helper()
	rec := doJSON(t, h, http.MethodGet, "/api/spaces/"+spaceID+"/state", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get state: status = %d (body %s)", rec.Code, rec.Body.String())
	}
	var state map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &state); err != nil {
		t.Fatalf("parse state: %v", err)
	}
	return state
}

// step is one seeding request executed before a table case's request.
type step struct {
	method string
	path   string
	body   string
}

// Fixture bodies reused across cases. The seeded space "sandbox" already
// contains project "loom" (see newTestServer).
const (
	projectBody = `{"id":"proj-new","name":"New","color":"green","purpose":"p","outcome":"o",` +
		`"currentFocus":"cf","nextAction":"na","altNextActions":[],"status":"active",` +
		`"resumeContext":"rc","tags":[]}`
	activityBody = `{"id":"act-9","projectId":"loom","date":"2026-07-20","type":"work",` +
		`"title":"Fix","details":"d","source":"manual","tags":[],"links":[]}`
	taskBody     = `{"id":"tsk-1","title":"Do it","status":"open","createdAt":"2026-07-26"}`
	noteBody     = `{"id":"note-1","title":"T","body":"B","createdAt":"2026-07-26"}`
	reminderBody = `{"id":"rem-1","text":"Ping","remindAt":"2026-07-27T09:00:00"}`
	inboxBody    = `{"id":"inb-1","raw":"call dan","capturedAt":"2026-07-26T08:00:00",` +
		`"suggestedKind":"task","status":"pending"}`
)

func TestEntityMutationEndpoints(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		seed       []step
		method     string
		path       string
		body       string
		wantStatus int
		// wantInBody, when set, must appear in the response body.
		wantInBody string
		// checkState, when set, runs against the sandbox state after the
		// request (round-trip verification through the real store).
		checkState func(t *testing.T, state map[string]json.RawMessage)
	}{
		// ── creates: happy paths ────────────────────────────────────────
		{
			name: "create project round-trips", method: http.MethodPost,
			path: "/api/spaces/sandbox/projects", body: projectBody,
			wantStatus: http.StatusCreated, wantInBody: `"id":"proj-new"`,
			checkState: func(t *testing.T, state map[string]json.RawMessage) {
				t.Helper()
				if !strings.Contains(string(state["projects"]), `"id":"proj-new"`) {
					t.Errorf("project missing from state: %s", state["projects"])
				}
			},
		},
		{
			name: "create activity round-trips", method: http.MethodPost,
			path: "/api/spaces/sandbox/activities", body: activityBody,
			wantStatus: http.StatusCreated, wantInBody: `"id":"act-9"`,
			checkState: func(t *testing.T, state map[string]json.RawMessage) {
				t.Helper()
				if !strings.Contains(string(state["activities"]), `"id":"act-9"`) {
					t.Errorf("activity missing from state: %s", state["activities"])
				}
			},
		},
		{
			name: "create task round-trips", method: http.MethodPost,
			path: "/api/spaces/sandbox/tasks", body: taskBody,
			wantStatus: http.StatusCreated, wantInBody: `"id":"tsk-1"`,
			checkState: func(t *testing.T, state map[string]json.RawMessage) {
				t.Helper()
				if !strings.Contains(string(state["tasks"]), `"id":"tsk-1"`) {
					t.Errorf("task missing from state: %s", state["tasks"])
				}
			},
		},
		{
			name: "create note round-trips", method: http.MethodPost,
			path: "/api/spaces/sandbox/notes", body: noteBody,
			wantStatus: http.StatusCreated, wantInBody: `"id":"note-1"`,
			checkState: func(t *testing.T, state map[string]json.RawMessage) {
				t.Helper()
				if !strings.Contains(string(state["notes"]), `"id":"note-1"`) {
					t.Errorf("note missing from state: %s", state["notes"])
				}
			},
		},
		{
			name: "create reminder round-trips", method: http.MethodPost,
			path: "/api/spaces/sandbox/reminders", body: reminderBody,
			wantStatus: http.StatusCreated, wantInBody: `"id":"rem-1"`,
			checkState: func(t *testing.T, state map[string]json.RawMessage) {
				t.Helper()
				if !strings.Contains(string(state["reminders"]), `"id":"rem-1"`) {
					t.Errorf("reminder missing from state: %s", state["reminders"])
				}
			},
		},
		{
			name: "create inbox item round-trips", method: http.MethodPost,
			path: "/api/spaces/sandbox/inbox", body: inboxBody,
			wantStatus: http.StatusCreated, wantInBody: `"id":"inb-1"`,
			checkState: func(t *testing.T, state map[string]json.RawMessage) {
				t.Helper()
				if !strings.Contains(string(state["inbox"]), `"id":"inb-1"`) {
					t.Errorf("inbox item missing from state: %s", state["inbox"])
				}
			},
		},
		// ── creates: duplicate ids ──────────────────────────────────────
		{
			name: "duplicate project id is 409", method: http.MethodPost,
			path:       "/api/spaces/sandbox/projects",
			body:       strings.Replace(projectBody, "proj-new", "loom", 1),
			wantStatus: http.StatusConflict, wantInBody: "already exists",
		},
		{
			name:   "duplicate task id is 409",
			seed:   []step{{http.MethodPost, "/api/spaces/sandbox/tasks", taskBody}},
			method: http.MethodPost, path: "/api/spaces/sandbox/tasks", body: taskBody,
			wantStatus: http.StatusConflict, wantInBody: "already exists",
		},
		{
			name:   "duplicate inbox id is 409",
			seed:   []step{{http.MethodPost, "/api/spaces/sandbox/inbox", inboxBody}},
			method: http.MethodPost, path: "/api/spaces/sandbox/inbox", body: inboxBody,
			wantStatus: http.StatusConflict, wantInBody: "already exists",
		},
		// ── creates: validation rejections ──────────────────────────────
		{
			name: "project status outside the union is 400", method: http.MethodPost,
			path:       "/api/spaces/sandbox/projects",
			body:       strings.Replace(projectBody, `"status":"active"`, `"status":"zombie"`, 1),
			wantStatus: http.StatusBadRequest, wantInBody: "status must be one of active, waiting, blocked, paused, completed",
		},
		{
			name: "project color outside the ramp is 400", method: http.MethodPost,
			path:       "/api/spaces/sandbox/projects",
			body:       strings.Replace(projectBody, `"color":"green"`, `"color":"magenta"`, 1),
			wantStatus: http.StatusBadRequest, wantInBody: "color must be one of",
		},
		{
			name: "missing id is 400", method: http.MethodPost,
			path:       "/api/spaces/sandbox/tasks",
			body:       `{"title":"Do it","status":"open","createdAt":"2026-07-26"}`,
			wantStatus: http.StatusBadRequest, wantInBody: "id is required",
		},
		{
			name: "uppercase id is 400", method: http.MethodPost,
			path:       "/api/spaces/sandbox/tasks",
			body:       strings.Replace(taskBody, "tsk-1", "TSK-1", 1),
			wantStatus: http.StatusBadRequest, wantInBody: "id must be 1-64 characters",
		},
		{
			name: "activity type outside the union is 400", method: http.MethodPost,
			path:       "/api/spaces/sandbox/activities",
			body:       strings.Replace(activityBody, `"type":"work"`, `"type":"vibes"`, 1),
			wantStatus: http.StatusBadRequest, wantInBody: "type must be one of work, research, meeting, decision, blocker, milestone",
		},
		{
			name: "activity date not yyyy-MM-dd is 400", method: http.MethodPost,
			path:       "/api/spaces/sandbox/activities",
			body:       strings.Replace(activityBody, "2026-07-20", "20/07/2026", 1),
			wantStatus: http.StatusBadRequest, wantInBody: "date must be a yyyy-MM-dd date",
		},
		{
			name: "activity referencing unknown project is 400", method: http.MethodPost,
			path:       "/api/spaces/sandbox/activities",
			body:       strings.Replace(activityBody, `"projectId":"loom"`, `"projectId":"ghost"`, 1),
			wantStatus: http.StatusBadRequest, wantInBody: "projectId does not match an existing project",
		},
		{
			name: "task status outside the union is 400", method: http.MethodPost,
			path:       "/api/spaces/sandbox/tasks",
			body:       strings.Replace(taskBody, `"status":"open"`, `"status":"later"`, 1),
			wantStatus: http.StatusBadRequest, wantInBody: "status must be one of open, waiting, someday, done",
		},
		{
			name: "reminder datetime malformed is 400", method: http.MethodPost,
			path:       "/api/spaces/sandbox/reminders",
			body:       strings.Replace(reminderBody, "2026-07-27T09:00:00", "tomorrow", 1),
			wantStatus: http.StatusBadRequest, wantInBody: "remindAt must be an ISO datetime",
		},
		{
			name: "inbox suggestedKind outside the union is 400", method: http.MethodPost,
			path:       "/api/spaces/sandbox/inbox",
			body:       strings.Replace(inboxBody, `"suggestedKind":"task"`, `"suggestedKind":"wish"`, 1),
			wantStatus: http.StatusBadRequest, wantInBody: "suggestedKind must be one of task, note, reminder, activity, project",
		},
		{
			name: "unknown field on create is 400", method: http.MethodPost,
			path:       "/api/spaces/sandbox/tasks",
			body:       strings.Replace(taskBody, `"title"`, `"titel"`, 1),
			wantStatus: http.StatusBadRequest, wantInBody: `unknown field 'titel'`,
		},
		{
			name: "malformed JSON is 400", method: http.MethodPost,
			path: "/api/spaces/sandbox/tasks", body: `{"id":`,
			wantStatus: http.StatusBadRequest, wantInBody: "invalid JSON body",
		},
		// ── creates: foreign-space isolation ────────────────────────────
		{
			name: "create project in foreign space is 404", method: http.MethodPost,
			path: "/api/spaces/private/projects", body: projectBody,
			wantStatus: http.StatusNotFound, wantInBody: "space not found",
		},
		{
			name: "create activity in foreign space is 404", method: http.MethodPost,
			path: "/api/spaces/private/activities", body: activityBody,
			wantStatus: http.StatusNotFound, wantInBody: "space not found",
		},
		{
			name: "create task in foreign space is 404", method: http.MethodPost,
			path: "/api/spaces/private/tasks", body: taskBody,
			wantStatus: http.StatusNotFound, wantInBody: "space not found",
		},
		{
			name: "create note in foreign space is 404", method: http.MethodPost,
			path: "/api/spaces/private/notes", body: noteBody,
			wantStatus: http.StatusNotFound, wantInBody: "space not found",
		},
		{
			name: "create reminder in foreign space is 404", method: http.MethodPost,
			path: "/api/spaces/private/reminders", body: reminderBody,
			wantStatus: http.StatusNotFound, wantInBody: "space not found",
		},
		{
			name: "create inbox item in foreign space is 404", method: http.MethodPost,
			path: "/api/spaces/private/inbox", body: inboxBody,
			wantStatus: http.StatusNotFound, wantInBody: "space not found",
		},
		// ── patches ─────────────────────────────────────────────────────
		{
			name:   "patch project next-action lifecycle subset",
			method: http.MethodPatch, path: "/api/spaces/sandbox/projects/loom",
			body: `{"nextAction":"ship it","altNextActions":["write docs"],` +
				`"resumeContext":"mid-refactor","status":"waiting","waitingOn":"Dan"}`,
			wantStatus: http.StatusOK, wantInBody: `"waitingOn":"Dan"`,
			checkState: func(t *testing.T, state map[string]json.RawMessage) {
				t.Helper()
				projects := string(state["projects"])
				for _, want := range []string{`"nextAction":"ship it"`, `"altNextActions":["write docs"]`,
					`"resumeContext":"mid-refactor"`, `"status":"waiting"`, `"waitingOn":"Dan"`} {
					if !strings.Contains(projects, want) {
						t.Errorf("state missing %s: %s", want, projects)
					}
				}
			},
		},
		{
			name:   "patch project null clears waitingOn",
			seed:   []step{{http.MethodPatch, "/api/spaces/sandbox/projects/loom", `{"waitingOn":"Dan"}`}},
			method: http.MethodPatch, path: "/api/spaces/sandbox/projects/loom",
			body: `{"waitingOn":null}`, wantStatus: http.StatusOK,
			checkState: func(t *testing.T, state map[string]json.RawMessage) {
				t.Helper()
				if strings.Contains(string(state["projects"]), "waitingOn") {
					t.Errorf("waitingOn not cleared: %s", state["projects"])
				}
			},
		},
		{
			name:   "patch project unknown field is 400",
			method: http.MethodPatch, path: "/api/spaces/sandbox/projects/loom",
			body: `{"nickname":"loomy"}`, wantStatus: http.StatusBadRequest,
			wantInBody: `unknown field 'nickname'`,
		},
		{
			name:   "patch project status outside the union is 400",
			method: http.MethodPatch, path: "/api/spaces/sandbox/projects/loom",
			body: `{"status":"zombie"}`, wantStatus: http.StatusBadRequest,
			wantInBody: "status must be one of",
		},
		{
			name:   "patch unknown project is 404",
			method: http.MethodPatch, path: "/api/spaces/sandbox/projects/ghost",
			body: `{"name":"x"}`, wantStatus: http.StatusNotFound, wantInBody: "project not found",
		},
		{
			name: "patch task completes and clears due",
			seed: []step{{http.MethodPost, "/api/spaces/sandbox/tasks",
				strings.Replace(taskBody, `"createdAt"`, `"due":"2026-08-01","createdAt"`, 1)}},
			method: http.MethodPatch, path: "/api/spaces/sandbox/tasks/tsk-1",
			body: `{"status":"done","due":null}`, wantStatus: http.StatusOK,
			checkState: func(t *testing.T, state map[string]json.RawMessage) {
				t.Helper()
				tasks := string(state["tasks"])
				if !strings.Contains(tasks, `"status":"done"`) || strings.Contains(tasks, "due") {
					t.Errorf("task patch not applied: %s", tasks)
				}
			},
		},
		{
			name:   "patch task to unknown project is 400",
			seed:   []step{{http.MethodPost, "/api/spaces/sandbox/tasks", taskBody}},
			method: http.MethodPatch, path: "/api/spaces/sandbox/tasks/tsk-1",
			body: `{"projectId":"ghost"}`, wantStatus: http.StatusBadRequest,
			wantInBody: "projectId does not match an existing project",
		},
		{
			name:   "patch activity planned flag",
			seed:   []step{{http.MethodPost, "/api/spaces/sandbox/activities", activityBody}},
			method: http.MethodPatch, path: "/api/spaces/sandbox/activities/act-9",
			body: `{"planned":true,"type":"milestone"}`, wantStatus: http.StatusOK,
			wantInBody: `"planned":true`,
		},
		{
			name:   "patch reminder marks done",
			seed:   []step{{http.MethodPost, "/api/spaces/sandbox/reminders", reminderBody}},
			method: http.MethodPatch, path: "/api/spaces/sandbox/reminders/rem-1",
			body: `{"done":true}`, wantStatus: http.StatusOK, wantInBody: `"done":true`,
		},
		{
			name:   "patch inbox item dismisses",
			seed:   []step{{http.MethodPost, "/api/spaces/sandbox/inbox", inboxBody}},
			method: http.MethodPatch, path: "/api/spaces/sandbox/inbox/inb-1",
			body: `{"status":"dismissed"}`, wantStatus: http.StatusOK,
			wantInBody: `"status":"dismissed"`,
		},
		{
			name:   "patch task in foreign space is 404",
			method: http.MethodPatch, path: "/api/spaces/private/tasks/tsk-1",
			body: `{"status":"done"}`, wantStatus: http.StatusNotFound,
			wantInBody: "space not found",
		},
		// ── deletes ─────────────────────────────────────────────────────
		{
			name:   "delete activity round-trips",
			seed:   []step{{http.MethodPost, "/api/spaces/sandbox/activities", activityBody}},
			method: http.MethodDelete, path: "/api/spaces/sandbox/activities/act-9",
			wantStatus: http.StatusNoContent,
			checkState: func(t *testing.T, state map[string]json.RawMessage) {
				t.Helper()
				if string(state["activities"]) != "[]" {
					t.Errorf("activities after delete = %s, want []", state["activities"])
				}
			},
		},
		{
			name:   "delete unknown activity is 404",
			method: http.MethodDelete, path: "/api/spaces/sandbox/activities/ghost",
			wantStatus: http.StatusNotFound, wantInBody: "activity not found",
		},
		{
			name:   "delete activity in foreign space is 404",
			method: http.MethodDelete, path: "/api/spaces/private/activities/act-9",
			wantStatus: http.StatusNotFound, wantInBody: "space not found",
		},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := newTestServer(t).Handler()
			for _, sd := range tt.seed {
				if rec := doJSON(t, h, sd.method, sd.path, sd.body); rec.Code >= 400 {
					t.Fatalf("seed %s %s: status = %d (body %s)", sd.method, sd.path, rec.Code, rec.Body.String())
				}
			}
			rec := doJSON(t, h, tt.method, tt.path, tt.body)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.wantInBody != "" && !strings.Contains(rec.Body.String(), tt.wantInBody) {
				t.Errorf("body = %s, want it to contain %q", rec.Body.String(), tt.wantInBody)
			}
			if rec.Code >= 400 {
				checkErrorEnvelope(t, rec.Body.Bytes())
			}
			if tt.checkState != nil {
				tt.checkState(t, getState(t, h, "sandbox"))
			}
		})
	}
}

func TestConvertInboxEndpoint(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantInBody string
		checkState func(t *testing.T, state map[string]json.RawMessage)
	}{
		{
			name:       "converts to a task atomically",
			body:       fmt.Sprintf(`{"kind":"task","task":%s}`, taskBody),
			wantStatus: http.StatusOK, wantInBody: `"status":"converted"`,
			checkState: func(t *testing.T, state map[string]json.RawMessage) {
				t.Helper()
				if !strings.Contains(string(state["inbox"]), `"status":"converted"`) {
					t.Errorf("inbox not marked converted: %s", state["inbox"])
				}
				if !strings.Contains(string(state["tasks"]), `"id":"tsk-1"`) {
					t.Errorf("task not created: %s", state["tasks"])
				}
			},
		},
		{
			name:       "converts to a project",
			body:       fmt.Sprintf(`{"kind":"project","project":%s}`, projectBody),
			wantStatus: http.StatusOK, wantInBody: `"id":"proj-new"`,
			checkState: func(t *testing.T, state map[string]json.RawMessage) {
				t.Helper()
				if !strings.Contains(string(state["projects"]), `"id":"proj-new"`) {
					t.Errorf("project not created: %s", state["projects"])
				}
			},
		},
		{
			name: "failed insert rolls the conversion back",
			body: fmt.Sprintf(`{"kind":"activity","activity":%s}`,
				strings.Replace(activityBody, `"projectId":"loom"`, `"projectId":"ghost"`, 1)),
			wantStatus: http.StatusBadRequest,
			wantInBody: "projectId does not match an existing project",
			checkState: func(t *testing.T, state map[string]json.RawMessage) {
				t.Helper()
				if !strings.Contains(string(state["inbox"]), `"status":"pending"`) {
					t.Errorf("inbox item not left pending after rollback: %s", state["inbox"])
				}
				if string(state["activities"]) != "[]" {
					t.Errorf("activities after rollback = %s, want []", state["activities"])
				}
			},
		},
		{
			name:       "duplicate created id rolls back with 409",
			body:       fmt.Sprintf(`{"kind":"project","project":%s}`, strings.Replace(projectBody, "proj-new", "loom", 1)),
			wantStatus: http.StatusConflict, wantInBody: "project id already exists",
			checkState: func(t *testing.T, state map[string]json.RawMessage) {
				t.Helper()
				if !strings.Contains(string(state["inbox"]), `"status":"pending"`) {
					t.Errorf("inbox item not left pending after rollback: %s", state["inbox"])
				}
			},
		},
		{
			name:       "kind outside the union is 400",
			body:       `{"kind":"wish"}`,
			wantStatus: http.StatusBadRequest, wantInBody: "kind must be one of",
		},
		{
			name:       "kind without matching payload is 400",
			body:       `{"kind":"task"}`,
			wantStatus: http.StatusBadRequest, wantInBody: "kind task requires a task payload",
		},
		{
			name:       "payload of another kind is 400",
			body:       fmt.Sprintf(`{"kind":"task","task":%s,"note":%s}`, taskBody, noteBody),
			wantStatus: http.StatusBadRequest, wantInBody: "only the payload matching kind",
		},
		{
			name:       "invalid payload is 400",
			body:       fmt.Sprintf(`{"kind":"task","task":%s}`, strings.Replace(taskBody, `"status":"open"`, `"status":"later"`, 1)),
			wantStatus: http.StatusBadRequest, wantInBody: "status must be one of",
		},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := newTestServer(t).Handler()
			if rec := doJSON(t, h, http.MethodPost, "/api/spaces/sandbox/inbox", inboxBody); rec.Code != http.StatusCreated {
				t.Fatalf("seed inbox: status = %d (body %s)", rec.Code, rec.Body.String())
			}
			rec := doJSON(t, h, http.MethodPost, "/api/spaces/sandbox/inbox/inb-1/convert", tt.body)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.wantInBody != "" && !strings.Contains(rec.Body.String(), tt.wantInBody) {
				t.Errorf("body = %s, want it to contain %q", rec.Body.String(), tt.wantInBody)
			}
			if tt.checkState != nil {
				tt.checkState(t, getState(t, h, "sandbox"))
			}
		})
	}

	t.Run("unknown inbox id is 404", func(t *testing.T) {
		t.Parallel()
		h := newTestServer(t).Handler()
		rec := doJSON(t, h, http.MethodPost, "/api/spaces/sandbox/inbox/ghost/convert",
			fmt.Sprintf(`{"kind":"task","task":%s}`, taskBody))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404 (body %s)", rec.Code, rec.Body.String())
		}
	})
}

func TestSpaceLifecycleEndpoints(t *testing.T) {
	t.Parallel()
	h := newTestServer(t).Handler()

	// Create: slug + random suffix id, appended position, usable database.
	rec := doJSON(t, h, http.MethodPost, "/api/spaces", `{"name":"Deep Work!","color":"violet"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create space: status = %d (body %s)", rec.Code, rec.Body.String())
	}
	var created struct {
		Space store.Space `json:"space"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("parse create response: %v", err)
	}
	sp := created.Space
	if !regexp.MustCompile(`^deep-work-[0-9a-f]{8}$`).MatchString(sp.ID) {
		t.Errorf("space id = %q, want deep-work-<8 hex>", sp.ID)
	}
	if sp.Name != "Deep Work!" || sp.Color != "violet" {
		t.Errorf("space = %+v", sp)
	}
	if sp.Position != 1 {
		t.Errorf("position = %d, want 1 (after sandbox at 0)", sp.Position)
	}
	// The database file was created via the SpaceStore machinery: state is
	// immediately servable and empty.
	state := getState(t, h, sp.ID)
	if string(state["projects"]) != "[]" {
		t.Errorf("new space projects = %s, want []", state["projects"])
	}

	// Patch: name, color, position.
	rec = doJSON(t, h, http.MethodPatch, "/api/spaces/"+sp.ID, `{"name":"Focus","color":"tan","position":3}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch space: status = %d (body %s)", rec.Code, rec.Body.String())
	}
	for _, want := range []string{`"name":"Focus"`, `"color":"tan"`, `"position":3`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("patch response missing %s: %s", want, rec.Body.String())
		}
	}

	// Archive stamps archivedAt; unarchive clears it.
	rec = doJSON(t, h, http.MethodPost, "/api/spaces/"+sp.ID+"/archive", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"archivedAt"`) {
		t.Fatalf("archive: status = %d body = %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, h, http.MethodPost, "/api/spaces/"+sp.ID+"/unarchive", "")
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `"archivedAt"`) {
		t.Fatalf("unarchive: status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestArchivedSpaceWriteGuard(t *testing.T) {
	t.Parallel()
	h := newTestServer(t).Handler()

	// Seed an activity and an inbox item so patch/delete/convert targets
	// exist, then archive the space.
	for _, s := range []step{
		{http.MethodPost, "/api/spaces/sandbox/activities", activityBody},
		{http.MethodPost, "/api/spaces/sandbox/inbox", inboxBody},
		{http.MethodPost, "/api/spaces/sandbox/archive", ""},
	} {
		rec := doJSON(t, h, s.method, s.path, s.body)
		if rec.Code >= http.StatusBadRequest {
			t.Fatalf("seed %s %s: status = %d (body %s)", s.method, s.path, rec.Code, rec.Body.String())
		}
	}

	// Every content mutation must refuse with 409 while archived.
	writes := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/api/spaces/sandbox/projects", projectBody},
		{http.MethodPatch, "/api/spaces/sandbox/projects/loom", `{"name":"Nope"}`},
		{http.MethodPost, "/api/spaces/sandbox/activities", activityBody},
		{http.MethodPatch, "/api/spaces/sandbox/activities/act-9", `{"title":"Nope"}`},
		{http.MethodDelete, "/api/spaces/sandbox/activities/act-9", ""},
		{http.MethodPost, "/api/spaces/sandbox/tasks", taskBody},
		{http.MethodPatch, "/api/spaces/sandbox/tasks/tsk-1", `{"title":"Nope"}`},
		{http.MethodPost, "/api/spaces/sandbox/notes", noteBody},
		{http.MethodPost, "/api/spaces/sandbox/reminders", reminderBody},
		{http.MethodPatch, "/api/spaces/sandbox/reminders/rem-1", `{"text":"Nope"}`},
		{http.MethodPost, "/api/spaces/sandbox/inbox", inboxBody},
		{http.MethodPatch, "/api/spaces/sandbox/inbox/inb-1", `{"status":"dismissed"}`},
		{http.MethodPost, "/api/spaces/sandbox/inbox/inb-1/convert",
			`{"kind":"task","task":` + taskBody + `}`},
	}
	for _, wr := range writes {
		wr := wr // capture (golangci-lint predates Go 1.22 loopvar)
		t.Run(wr.method+" "+wr.path, func(t *testing.T) {
			rec := doJSON(t, h, wr.method, wr.path, wr.body)
			if rec.Code != http.StatusConflict {
				t.Fatalf("status = %d, want 409 (body %s)", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "space is archived") {
				t.Errorf("body = %s, want it to name the archived space", rec.Body.String())
			}
			checkErrorEnvelope(t, rec.Body.Bytes())
		})
	}

	// Reads and the space lifecycle stay open: the user can still view the
	// archived space, rename it, and unarchive it.
	t.Run("reads and lifecycle still allowed", func(t *testing.T) {
		state := getState(t, h, "sandbox")
		if !strings.Contains(string(state["inbox"]), `"id":"inb-1"`) {
			t.Errorf("archived state inbox = %s, want the seeded capture", state["inbox"])
		}
		rec := doJSON(t, h, http.MethodPatch, "/api/spaces/sandbox", `{"name":"Renamed"}`)
		if rec.Code != http.StatusOK {
			t.Errorf("rename archived space: status = %d (body %s)", rec.Code, rec.Body.String())
		}
		rec = doJSON(t, h, http.MethodPost, "/api/spaces/sandbox/unarchive", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("unarchive: status = %d (body %s)", rec.Code, rec.Body.String())
		}
		// Unarchiving lifts the write barrier.
		rec = doJSON(t, h, http.MethodPost, "/api/spaces/sandbox/tasks", taskBody)
		if rec.Code != http.StatusCreated {
			t.Errorf("create task after unarchive: status = %d (body %s)", rec.Code, rec.Body.String())
		}
	})
}

func TestSpaceEndpointErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
		wantInBody string
	}{
		{
			name: "create without name", method: http.MethodPost, path: "/api/spaces",
			body: `{"name":"  ","color":"blue"}`, wantStatus: http.StatusBadRequest,
			wantInBody: "name is required",
		},
		{
			name: "create with color outside the ramp", method: http.MethodPost, path: "/api/spaces",
			body: `{"name":"X","color":"magenta"}`, wantStatus: http.StatusBadRequest,
			wantInBody: "color must be one of blue, green, tan, violet, rose, orange, steel",
		},
		{
			name: "create with unknown field", method: http.MethodPost, path: "/api/spaces",
			body: `{"name":"X","color":"blue","icon":"zap"}`, wantStatus: http.StatusBadRequest,
			wantInBody: `unknown field 'icon'`,
		},
		{
			name: "patch foreign space", method: http.MethodPatch, path: "/api/spaces/private",
			body: `{"name":"Mine Now"}`, wantStatus: http.StatusNotFound,
			wantInBody: "space not found",
		},
		{
			name: "patch unknown space", method: http.MethodPatch, path: "/api/spaces/ghost",
			body: `{"name":"X"}`, wantStatus: http.StatusNotFound, wantInBody: "space not found",
		},
		{
			name: "patch with unknown field", method: http.MethodPatch, path: "/api/spaces/sandbox",
			body: `{"owner":"me"}`, wantStatus: http.StatusBadRequest, wantInBody: `unknown field 'owner'`,
		},
		{
			name: "patch with negative position", method: http.MethodPatch, path: "/api/spaces/sandbox",
			body: `{"position":-2}`, wantStatus: http.StatusBadRequest,
			wantInBody: "position must not be negative",
		},
		{
			name: "archive foreign space", method: http.MethodPost, path: "/api/spaces/private/archive",
			wantStatus: http.StatusNotFound, wantInBody: "space not found",
		},
		{
			name: "unarchive foreign space", method: http.MethodPost, path: "/api/spaces/private/unarchive",
			wantStatus: http.StatusNotFound, wantInBody: "space not found",
		},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := newTestServer(t).Handler()
			rec := doJSON(t, h, tt.method, tt.path, tt.body)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tt.wantInBody) {
				t.Errorf("body = %s, want it to contain %q", rec.Body.String(), tt.wantInBody)
			}
			checkErrorEnvelope(t, rec.Body.Bytes())
		})
	}
}

func TestCrossSpaceInboxCapture(t *testing.T) {
	t.Parallel()
	h := newTestServer(t).Handler()

	// A second owned space, created through the API.
	rec := doJSON(t, h, http.MethodPost, "/api/spaces", `{"name":"Side","color":"steel"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create space: status = %d (body %s)", rec.Code, rec.Body.String())
	}
	var created struct {
		Space store.Space `json:"space"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("parse create response: %v", err)
	}

	// Capture into the second space while sandbox stays untouched: the
	// {id} segment alone picks the capture target across owned spaces.
	rec = doJSON(t, h, http.MethodPost, "/api/spaces/"+created.Space.ID+"/inbox", inboxBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("cross-space capture: status = %d (body %s)", rec.Code, rec.Body.String())
	}
	side := getState(t, h, created.Space.ID)
	if !strings.Contains(string(side["inbox"]), `"id":"inb-1"`) {
		t.Errorf("side space inbox = %s, want the capture", side["inbox"])
	}
	sandbox := getState(t, h, "sandbox")
	if string(sandbox["inbox"]) != "[]" {
		t.Errorf("sandbox inbox = %s, want [] (capture must not leak)", sandbox["inbox"])
	}
}

func TestMutationEndpointsRequireAuth(t *testing.T) {
	t.Parallel()
	// Default session authenticator, no cookie: every mutation endpoint
	// must refuse with 401 before touching any store.
	dir := t.TempDir()
	core, err := store.NewCoreStore(store.WithDataDir(dir), store.WithClock(fixedClock))
	if err != nil {
		t.Fatalf("NewCoreStore: %v", err)
	}
	spaces, err := store.NewSpaceStore(store.WithDataDir(dir), store.WithClock(fixedClock))
	if err != nil {
		t.Fatalf("NewSpaceStore: %v", err)
	}
	t.Cleanup(func() {
		if err := core.Close(); err != nil {
			t.Errorf("close core: %v", err)
		}
		if err := spaces.Close(); err != nil {
			t.Errorf("close spaces: %v", err)
		}
	})
	h := NewServer(core, spaces, WithLogger(log.New(io.Discard, "", 0))).Handler()

	endpoints := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/spaces"},
		{http.MethodPatch, "/api/spaces/sandbox"},
		{http.MethodPost, "/api/spaces/sandbox/archive"},
		{http.MethodPost, "/api/spaces/sandbox/unarchive"},
		{http.MethodPost, "/api/spaces/sandbox/projects"},
		{http.MethodPatch, "/api/spaces/sandbox/projects/loom"},
		{http.MethodPost, "/api/spaces/sandbox/activities"},
		{http.MethodPatch, "/api/spaces/sandbox/activities/act-1"},
		{http.MethodDelete, "/api/spaces/sandbox/activities/act-1"},
		{http.MethodPost, "/api/spaces/sandbox/tasks"},
		{http.MethodPatch, "/api/spaces/sandbox/tasks/t-1"},
		{http.MethodPost, "/api/spaces/sandbox/notes"},
		{http.MethodPost, "/api/spaces/sandbox/reminders"},
		{http.MethodPatch, "/api/spaces/sandbox/reminders/r-1"},
		{http.MethodPost, "/api/spaces/sandbox/inbox"},
		{http.MethodPatch, "/api/spaces/sandbox/inbox/i-1"},
		{http.MethodPost, "/api/spaces/sandbox/inbox/i-1/convert"},
	}
	for _, ep := range endpoints {
		ep := ep // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(ep.method+" "+ep.path, func(t *testing.T) {
			t.Parallel()
			rec := doJSON(t, h, ep.method, ep.path, "{}")
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401 (body %s)", rec.Code, rec.Body.String())
			}
			checkErrorEnvelope(t, rec.Body.Bytes())
		})
	}
}
