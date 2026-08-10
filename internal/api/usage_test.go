package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/bgrewell/donezo/internal/store"
)

// adminServer is newTestServer with the authenticated user promoted to
// admin, which is what the usage panel requires.
func adminServer(t *testing.T) *Server {
	t.Helper()
	s := newTestServer(t)
	ben, err := s.core.GetUserByUsername(t.Context(), "ben")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	ben.Role = store.RoleAdmin
	s.auth = StaticAuthenticator{User: ben}
	return s
}

// anonymousAuthenticator authenticates nobody, for the 401 path.
type anonymousAuthenticator struct{}

func (anonymousAuthenticator) Authenticate(*http.Request) (store.User, error) {
	return store.User{}, store.ErrNotFound
}

// usageBody decodes the usage response.
func usageBody(t *testing.T, body []byte) store.InstanceUsage {
	t.Helper()
	var stats store.InstanceUsage
	if err := json.Unmarshal(body, &stats); err != nil {
		t.Fatalf("decode usage %s: %v", body, err)
	}
	return stats
}

// The boundary that matters: a member must not read the instance's figures,
// and it must be the server saying so, not the UI declining to render.
func TestUsageStatsRequiresAdmin(t *testing.T) {
	s := newTestServer(t)
	member, err := s.core.GetUserByUsername(t.Context(), "other")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if member.Role == store.RoleAdmin {
		t.Fatalf("fixture user %q is an admin; this test proves nothing", member.Username)
	}
	s.auth = StaticAuthenticator{User: member}

	rec := doJSON(t, s.Handler(), http.MethodGet, "/api/admin/usage", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("member GET /api/admin/usage = %d, want 403: %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "\"users\"") {
		t.Fatalf("refusal leaked the payload: %s", rec.Body)
	}
}

func TestUsageStatsRequiresAuthentication(t *testing.T) {
	s := newTestServer(t)
	s.auth = anonymousAuthenticator{}
	rec := doJSON(t, s.Handler(), http.MethodGet, "/api/admin/usage", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous GET = %d, want 401: %s", rec.Code, rec.Body)
	}
}

func TestUsageStatsCountsEntities(t *testing.T) {
	s := adminServer(t)
	ctx := context.Background()

	// A project with some fields left empty, so adoption is not trivially 1.
	if _, err := s.spaces.CreateProject(ctx, "sandbox", store.Project{
		ID: "bare", Name: "Bare", Color: "blue", Status: "paused",
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	due := "2026-01-01"
	if _, err := s.spaces.CreateTask(ctx, "sandbox", store.TaskItem{
		ID: "tsk-1", Title: "Dated", Status: "open", Due: &due, ProjectID: strptr("loom"),
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if _, err := s.spaces.CreateTask(ctx, "sandbox", store.TaskItem{
		ID: "tsk-2", Title: "Undated", Status: "done",
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	effort := 1.5
	if _, err := s.spaces.CreateActivity(ctx, "sandbox", store.ActivityEntry{
		ID: "act-1", ProjectID: "loom", Date: "2026-07-26", Type: "work", Title: "Did a thing",
		Source: "manual", EffortHours: &effort, Tags: []string{"infra", "go"},
	}); err != nil {
		t.Fatalf("create activity: %v", err)
	}

	rec := doJSON(t, s.Handler(), http.MethodGet, "/api/admin/usage", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("admin GET = %d: %s", rec.Code, rec.Body)
	}
	stats := usageBody(t, rec.Body.Bytes())

	if len(stats.Users) != 2 {
		t.Fatalf("got %d users, want both fixture accounts", len(stats.Users))
	}
	if stats.Totals.Projects.Total != 2 {
		t.Fatalf("projects total = %d, want 2", stats.Totals.Projects.Total)
	}
	if stats.Totals.Tasks.Total != 2 {
		t.Fatalf("tasks total = %d, want 2", stats.Totals.Tasks.Total)
	}
	if stats.Totals.TasksDone != 1 || stats.Totals.TasksOpen != 1 {
		t.Fatalf("tasks done/open = %d/%d, want 1/1", stats.Totals.TasksDone, stats.Totals.TasksOpen)
	}
	if got := stats.Totals.Fields["taskDue"]; got.Total != 2 || got.Set != 1 {
		t.Fatalf("taskDue adoption = %+v, want 1 of 2", got)
	}
	// The fixture project has a next action, the bare one does not — an
	// adoption figure that reads 2 of 2 would mean the emptiness check is
	// not working.
	if got := stats.Totals.Fields["nextAction"]; got.Total != 2 || got.Set != 1 {
		t.Fatalf("nextAction adoption = %+v, want 1 of 2", got)
	}
	if got := stats.Totals.Fields["effortHours"]; got.Set != 1 {
		t.Fatalf("effortHours adoption = %+v, want 1 set", got)
	}
	if stats.Totals.ActivityTypes["work"] != 1 {
		t.Fatalf("activity types = %v, want one work", stats.Totals.ActivityTypes)
	}
	if stats.Totals.ProjectStatuses["paused"] != 1 || stats.Totals.ProjectStatuses["active"] != 1 {
		t.Fatalf("project statuses = %v, want one each", stats.Totals.ProjectStatuses)
	}
	if stats.Totals.DistinctTags != 2 {
		t.Fatalf("distinct tags = %d, want 2", stats.Totals.DistinctTags)
	}
	if stats.GeneratedAt == "" {
		t.Fatal("no generatedAt")
	}
	if len(stats.NotDerivable) == 0 {
		t.Fatal("nothing listed as not derivable; a zero would read as 'never used'")
	}
}

// The privacy rule from #45, which is invisible until it bites: a project's
// id is its NAME slugified, so ids are content. Nothing in this response may
// carry one.
func TestUsageStatsCarriesNoContent(t *testing.T) {
	s := adminServer(t)
	ctx := context.Background()
	// A project whose name — and therefore whose id — is unmistakable.
	if _, err := s.spaces.CreateProject(ctx, "sandbox", store.Project{
		ID: "divorce-paperwork", Name: "Divorce paperwork", Color: "rose", Status: "active",
		Purpose: "private", Outcome: "private", CurrentFocus: "private",
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := s.spaces.CreateTask(ctx, "sandbox", store.TaskItem{
		ID: "tsk-1", Title: "Call the solicitor", Status: "open",
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if _, err := s.spaces.CreateNote(ctx, "sandbox", store.NoteItem{
		ID: "note-1", Title: "Bank details", Body: "sort code 00-00-00",
	}); err != nil {
		t.Fatalf("create note: %v", err)
	}

	rec := doJSON(t, s.Handler(), http.MethodGet, "/api/admin/usage", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("admin GET = %d: %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	for _, forbidden := range []string{
		"divorce-paperwork",  // the project id, which is the name
		"Divorce paperwork",  // the name itself
		"Call the solicitor", // a task title
		"Bank details",       // a note title
		"sort code",          // a note body
		"sandbox",            // the space id, which is derived from its name
		"Sandbox",            // the space name
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("usage response contains %q, which is content:\n%s", forbidden, body)
		}
	}
	// And it did count them, so the absence above is redaction rather than
	// an empty response.
	stats := usageBody(t, rec.Body.Bytes())
	if stats.Totals.Notes.Total != 1 || stats.Totals.Tasks.Total != 1 {
		t.Fatalf("nothing was counted: notes=%d tasks=%d",
			stats.Totals.Notes.Total, stats.Totals.Tasks.Total)
	}
}

func TestUsageStatsSeparatesUsers(t *testing.T) {
	s := adminServer(t)
	ctx := context.Background()
	// A task in ben's space only; "other" owns a space with nothing in it.
	if _, err := s.spaces.CreateTask(ctx, "sandbox", store.TaskItem{
		ID: "tsk-1", Title: "Ben's task", Status: "open",
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}

	rec := doJSON(t, s.Handler(), http.MethodGet, "/api/admin/usage", "")
	stats := usageBody(t, rec.Body.Bytes())

	byUser := map[string]store.UserUsage{}
	for _, u := range stats.Users {
		byUser[u.Username] = u
	}
	if got := byUser["ben"].Tasks.Total; got != 1 {
		t.Fatalf("ben's tasks = %d, want 1", got)
	}
	if got := byUser["other"].Tasks.Total; got != 0 {
		t.Fatalf("other's tasks = %d, want 0 — figures are leaking across accounts", got)
	}
	if byUser["ben"].Spaces != 1 || byUser["other"].Spaces != 1 {
		t.Fatalf("space counts = %d/%d, want 1 each", byUser["ben"].Spaces, byUser["other"].Spaces)
	}
}

func TestUsageStatsIgnoresTrashedRows(t *testing.T) {
	s := adminServer(t)
	ctx := context.Background()
	if _, err := s.spaces.CreateTask(ctx, "sandbox", store.TaskItem{
		ID: "tsk-live", Title: "Live", Status: "open",
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if _, err := s.spaces.CreateTask(ctx, "sandbox", store.TaskItem{
		ID: "tsk-gone", Title: "Trashed", Status: "open",
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := s.spaces.DeleteTask(ctx, "sandbox", "tsk-gone"); err != nil {
		t.Fatalf("delete task: %v", err)
	}

	rec := doJSON(t, s.Handler(), http.MethodGet, "/api/admin/usage", "")
	stats := usageBody(t, rec.Body.Bytes())
	// Counting trashed rows would overstate every figure on the panel, and
	// the bystander is what proves the filter is scoped rather than absent.
	if stats.Totals.Tasks.Total != 1 {
		t.Fatalf("tasks total = %d, want 1 — trashed rows are being counted", stats.Totals.Tasks.Total)
	}
}

// strptr is a local helper for the optional project link.
func strptr(s string) *string { return &s }
