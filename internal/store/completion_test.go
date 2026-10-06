package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// migrationsBefore hides every space migration at or after version max, so a
// test can stand a database up at the schema that existed before it.
type migrationsBefore struct {
	fs.FS
	max int
}

func (m migrationsBefore) ReadDir(name string) ([]fs.DirEntry, error) {
	entries, err := fs.ReadDir(m.FS, name)
	if err != nil {
		return nil, err
	}
	kept := entries[:0]
	for _, e := range entries {
		prefix, _, _ := strings.Cut(e.Name(), "_")
		if v, err := strconv.Atoi(prefix); err == nil && v < m.max {
			kept = append(kept, e)
		}
	}
	return kept, nil
}

// The backfill is the only chance old data gets at a completion date, so each
// of its rules is pinned here against rows written through the 0007 schema.
func TestBackfillCompletions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, err := openDB(filepath.Join(t.TempDir(), "space.db"))
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}
	defer closeQuietly2(t, db)
	now := func() string { return fixedNow }
	if _, err := migrate(ctx, db, migrationsBefore{spaceMigrationFS, 8}, "migrations/space", now); err != nil {
		t.Fatalf("migrate to 0007: %v", err)
	}

	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	project := func(id, status, updatedAt string) {
		exec(`INSERT INTO projects (id, name, color, purpose, outcome, current_focus, next_action,
			status, resume_context, created_at, updated_at)
			VALUES (?, ?, 'blue', '', '', '', '', ?, '', '2026-06-01T00:00:00Z', ?)`,
			id, id, status, updatedAt)
	}
	activity := func(id, projectID, typ, title, date, createdAt string, planned bool) {
		var p any
		if planned {
			p = 1
		}
		exec(`INSERT INTO activities (id, project_id, date, type, title, details, source,
			planned, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, '', 'manual', ?, ?, ?)`,
			id, projectID, date, typ, title, p, createdAt, createdAt)
	}
	task := func(id string, projectID any, title, status, createdAt string) {
		exec(`INSERT INTO tasks (id, project_id, title, status, created_at) VALUES (?, ?, ?, ?, ?)`,
			id, projectID, title, status, createdAt)
	}

	project("p1", "active", "2026-07-01T00:00:00Z")
	project("p2", "completed", "2026-07-25T08:00:00Z")
	project("p3", "completed", "2026-07-22T08:00:00Z")
	project("p4", "active", "2026-07-01T00:00:00Z")

	// Matched despite case and spacing; logged the same day.
	task("t-plain", "p1", "Fix login", "done", "2026-07-01")
	activity("a-plain", "p1", "work", "fix  Login ", "2026-07-03", "2026-07-03T15:04:05Z", false)
	// Repeated titles pair in order rather than all landing on the first.
	task("t-rev1", "p1", "Weekly review", "done", "2026-07-01")
	task("t-rev2", "p1", "Weekly review", "done", "2026-07-08")
	activity("a-rev1", "p1", "work", "Weekly review", "2026-07-04", "2026-07-04T10:00:00Z", false)
	activity("a-rev2", "p1", "work", "Weekly review", "2026-07-11", "2026-07-11T10:00:00Z", false)
	// Backdated: the date the person gave wins, at noon UTC.
	task("t-back", "p1", "Write report", "done", "2026-07-01")
	activity("a-back", "p1", "work", "Write report", "2026-07-02", "2026-07-20T09:00:00Z", false)
	// An activity from before the task existed cannot be its completion.
	task("t-early", "p1", "Old thing", "done", "2026-07-10")
	activity("a-early", "p1", "work", "Old thing", "2026-07-05", "2026-07-05T09:00:00Z", false)
	// Same title on another project is a different piece of work.
	task("t-other", "p1", "Deploy", "done", "2026-07-01")
	activity("a-other", "p4", "work", "Deploy", "2026-07-02", "2026-07-02T09:00:00Z", false)
	// Planned work is not a record of anything done.
	task("t-plan", "p1", "Ship it", "done", "2026-07-01")
	activity("a-plan", "p1", "work", "Ship it", "2026-07-02", "2026-07-02T09:00:00Z", true)
	// Open tasks are left alone even with a matching activity.
	task("t-open", "p1", "Still going", "open", "2026-07-01")
	activity("a-open", "p1", "work", "Still going", "2026-07-02", "2026-07-02T09:00:00Z", false)
	// Unfiled done tasks never had an activity to find.
	task("t-unfiled", nil, "Errand", "done", "2026-07-01")

	// p2 has a milestone (and a later planned one, which must not count); p3
	// has none and falls back to updated_at.
	activity("m-p2", "p2", "milestone", "Launched", "2026-07-20", "2026-07-20T18:00:00Z", false)
	activity("m-p2-plan", "p2", "milestone", "v2", "2026-08-30", "2026-07-20T18:00:00Z", true)

	if _, err := migrate(ctx, db, spaceMigrationFS, "migrations/space", now); err != nil {
		t.Fatalf("migrate to head: %v", err)
	}

	type got struct{ at, src sql.NullString }
	readTask := func(id string) got {
		t.Helper()
		var g got
		if err := db.QueryRowContext(ctx,
			`SELECT completed_at, completed_source FROM tasks WHERE id = ?`, id).Scan(&g.at, &g.src); err != nil {
			t.Fatalf("read task %s: %v", id, err)
		}
		return g
	}
	for _, tt := range []struct{ task, wantAt, wantActivity string }{
		{"t-plain", "2026-07-03T15:04:05Z", "a-plain"},
		{"t-rev1", "2026-07-04T10:00:00Z", "a-rev1"},
		{"t-rev2", "2026-07-11T10:00:00Z", "a-rev2"},
		{"t-back", "2026-07-02T12:00:00Z", "a-back"},
	} {
		g := readTask(tt.task)
		if g.at.String != tt.wantAt || g.src.String != CompletedInferred {
			t.Errorf("%s: completed = %v/%v, want %s/inferred", tt.task, g.at, g.src, tt.wantAt)
		}
		var link sql.NullString
		if err := db.QueryRowContext(ctx,
			`SELECT task_id FROM activities WHERE id = ?`, tt.wantActivity).Scan(&link); err != nil {
			t.Fatalf("read activity %s: %v", tt.wantActivity, err)
		}
		if link.String != tt.task {
			t.Errorf("%s: task_id = %v, want %s", tt.wantActivity, link, tt.task)
		}
	}
	for _, id := range []string{"t-early", "t-other", "t-plan", "t-open", "t-unfiled"} {
		if g := readTask(id); g.at.Valid || g.src.Valid {
			t.Errorf("%s: completed = %v/%v, want both NULL", id, g.at, g.src)
		}
	}

	for _, tt := range []struct{ project, wantAt string }{
		{"p2", "2026-07-20T12:00:00Z"},
		{"p3", "2026-07-22T08:00:00Z"},
	} {
		var g got
		if err := db.QueryRowContext(ctx,
			`SELECT completed_at, completed_source FROM projects WHERE id = ?`, tt.project).Scan(&g.at, &g.src); err != nil {
			t.Fatalf("read project %s: %v", tt.project, err)
		}
		if g.at.String != tt.wantAt || g.src.String != CompletedInferred {
			t.Errorf("%s: completed = %v/%v, want %s/inferred", tt.project, g.at, g.src, tt.wantAt)
		}
	}

	var raw string
	if err := db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, completionBackfillKey).Scan(&raw); err != nil {
		t.Fatalf("read report: %v", err)
	}
	var report struct {
		At             string `json:"at"`
		TasksMatched   int    `json:"tasksMatched"`
		TasksUnmatched int    `json:"tasksUnmatched"`
		Projects       int    `json:"projects"`
	}
	if err := json.Unmarshal([]byte(raw), &report); err != nil {
		t.Fatalf("decode report %q: %v", raw, err)
	}
	if report.At != fixedNow || report.TasksMatched != 4 || report.TasksUnmatched != 4 || report.Projects != 2 {
		t.Errorf("report = %+v, want 4 matched, 4 unmatched, 2 projects at %s", report, fixedNow)
	}
}

func TestInferredCompletedAt(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, date, createdAt, want string }{
		{"same day", "2026-07-03", "2026-07-03T15:04:05Z", "2026-07-03T15:04:05Z"},
		{"local evening, next UTC day", "2026-07-03", "2026-07-04T02:00:00Z", "2026-07-04T02:00:00Z"},
		{"offset normalized", "2026-07-03", "2026-07-03T10:00:00+02:00", "2026-07-03T08:00:00Z"},
		{"backdated", "2026-07-01", "2026-07-09T09:00:00Z", "2026-07-01T12:00:00Z"},
		{"unparseable created_at", "2026-07-01", "garbage", "2026-07-01T12:00:00Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := inferredCompletedAt(tt.date, tt.createdAt); got != tt.want {
				t.Errorf("inferredCompletedAt(%q, %q) = %q, want %q", tt.date, tt.createdAt, got, tt.want)
			}
		})
	}
}

// Every way a task's status moves, and what completed_at does in response.
func TestTaskCompletionStamping(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestSpaceStore(t)
	mustCreateProject(t, s, "p1")

	patch := func(id string, f func(*TaskItem)) TaskItem {
		t.Helper()
		got, err := s.PatchTask(ctx, testSpace, id, func(t *TaskItem) error { f(t); return nil })
		if err != nil {
			t.Fatalf("PatchTask: %v", err)
		}
		stored, err := s.GetTask(ctx, testSpace, id)
		if err != nil {
			t.Fatalf("GetTask: %v", err)
		}
		if !equalPtr(got.CompletedAt, stored.CompletedAt) || !equalPtr(got.CompletedSource, stored.CompletedSource) {
			t.Fatalf("returned %v/%v but stored %v/%v", got.CompletedAt, got.CompletedSource,
				stored.CompletedAt, stored.CompletedSource)
		}
		return stored
	}
	want := func(step string, got TaskItem, at, src string) {
		t.Helper()
		if deref(got.CompletedAt) != at || deref(got.CompletedSource) != src {
			t.Errorf("%s: completed = %q/%q, want %q/%q", step, deref(got.CompletedAt),
				deref(got.CompletedSource), at, src)
		}
	}

	if _, err := s.CreateTask(ctx, testSpace, TaskItem{
		ID: "t1", ProjectID: ptr("p1"), Title: "x", Status: "open", CreatedAt: "2026-07-01",
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	got := patch("t1", func(t *TaskItem) { t.Status = "done" })
	want("check off", got, fixedNow, CompletedRecorded)

	got = patch("t1", func(t *TaskItem) { t.Title = "renamed" })
	want("unrelated edit", got, fixedNow, CompletedRecorded)

	got = patch("t1", func(t *TaskItem) {
		t.CompletedAt, t.CompletedSource = ptr("2026-07-02T09:00:00Z"), ptr(CompletedManual)
	})
	want("manual correction", got, "2026-07-02T09:00:00Z", CompletedManual)

	got = patch("t1", func(t *TaskItem) { t.CompletedAt, t.CompletedSource = nil, nil })
	want("cleared to unknown stays unknown", got, "", "")

	// A person's clear keeps its manual mark, through unrelated edits too.
	got = patch("t1", func(t *TaskItem) { t.CompletedAt, t.CompletedSource = nil, ptr(CompletedManual) })
	want("marked unknown by the person", got, "", CompletedManual)
	got = patch("t1", func(t *TaskItem) { t.Details = "edited" })
	want("unknown survives an edit", got, "", CompletedManual)

	got = patch("t1", func(t *TaskItem) { t.Status = "open" })
	want("reopen", got, "", "")

	got = patch("t1", func(t *TaskItem) { t.Status = "done" })
	want("done again", got, fixedNow, CompletedRecorded)

	got = patch("t1", func(t *TaskItem) { t.CompletedAt, t.CompletedSource = ptr("2026-07-05T00:00:00Z"), nil })
	want("instant without a source is the person's", got, "2026-07-05T00:00:00Z", CompletedManual)

	created, err := s.CreateTask(ctx, testSpace, TaskItem{
		ID: "t2", Title: "born done", Status: "done", CreatedAt: "2026-07-01",
	})
	if err != nil {
		t.Fatalf("CreateTask done: %v", err)
	}
	want("created done", created, fixedNow, CompletedRecorded)

	created, err = s.CreateTask(ctx, testSpace, TaskItem{
		ID: "t3", Title: "open with a stray date", Status: "open", CreatedAt: "2026-07-01",
		CompletedAt: ptr("2026-07-01T00:00:00Z"), CompletedSource: ptr(CompletedManual),
	})
	if err != nil {
		t.Fatalf("CreateTask open: %v", err)
	}
	want("created open", created, "", "")
}

// A project's completion follows its status like a task's does, and every
// status change lands in the history — which a purge must clear out, or the
// foreign key would wedge the trash.
func TestProjectCompletionAndStatusHistory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestSpaceStore(t)
	mustCreateProject(t, s, "p1")

	setStatus := func(status string) Project {
		t.Helper()
		p, err := s.PatchProject(ctx, testSpace, "p1", func(p *Project) error { p.Status = status; return nil })
		if err != nil {
			t.Fatalf("PatchProject %s: %v", status, err)
		}
		return p
	}
	if p := setStatus("completed"); deref(p.CompletedAt) != fixedNow || deref(p.CompletedSource) != CompletedRecorded {
		t.Errorf("completed: %v/%v, want stamped", p.CompletedAt, p.CompletedSource)
	}
	if _, err := s.PatchProject(ctx, testSpace, "p1", func(p *Project) error { p.Name = "renamed"; return nil }); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if p := setStatus("cancelled"); p.CompletedAt != nil || p.CompletedSource != nil {
		t.Errorf("cancelled: %v/%v, want cleared — cancelled is not finished", p.CompletedAt, p.CompletedSource)
	}

	changes, err := s.ListProjectStatusChanges(ctx, testSpace)
	if err != nil {
		t.Fatalf("ListProjectStatusChanges: %v", err)
	}
	var trail []string
	for _, c := range changes {
		trail = append(trail, c.FromStatus+">"+c.ToStatus)
	}
	if got := strings.Join(trail, " "); got != "active>completed completed>cancelled" {
		t.Errorf("history = %q, want the two real changes and nothing for the rename", got)
	}

	if _, err := s.SoftDeleteProject(ctx, testSpace, "p1"); err != nil {
		t.Fatalf("SoftDeleteProject: %v", err)
	}
	if _, err := s.EmptyTrash(ctx, testSpace); err != nil {
		t.Fatalf("EmptyTrash with status history: %v", err)
	}
}

// The direct delete has to take a project's status history with it, or the
// foreign key refuses to delete a project that has nothing else attached.
func TestDeleteProjectWithStatusHistory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestSpaceStore(t)
	mustCreateProject(t, s, "p1")
	if _, err := s.PatchProject(ctx, testSpace, "p1", func(p *Project) error { p.Status = "paused"; return nil }); err != nil {
		t.Fatalf("PatchProject: %v", err)
	}
	if err := s.DeleteProject(ctx, testSpace, "p1"); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}
	if _, err := s.GetProject(ctx, testSpace, "p1"); err == nil {
		t.Error("project still there after DeleteProject")
	}
}

func TestCompletionInstant(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		t      time.Time
		want   string
		wantOK bool
	}{
		{"past, normalized to UTC", time.Date(2026, 7, 20, 9, 0, 0, 0, time.FixedZone("x", 2*3600)), "2026-07-20T07:00:00Z", true},
		{"now", now, "2026-07-26T12:00:00Z", true},
		{"just ahead clamps to now", now.Add(2 * time.Minute), "2026-07-26T12:00:00Z", true},
		{"hours ahead", now.Add(2 * time.Hour), "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := CompletionInstant(tt.t, now)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("CompletionInstant(%v) = %q, %v; want %q, %v", tt.t, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func equalPtr(a, b *string) bool { return deref(a) == deref(b) && (a == nil) == (b == nil) }

// A space records when it began stamping completions — the moment migration
// 0008 ran on it, by the store clock — so a summary can tell which periods
// undated done tasks could belong to.
func TestCompletionsRecordedSince(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestSpaceStore(t)
	if err := s.EnsureSpace(ctx, testSpace); err != nil {
		t.Fatalf("EnsureSpace: %v", err)
	}
	got, err := s.CompletionsRecordedSince(ctx, testSpace)
	if err != nil || got != fixedNow {
		t.Errorf("CompletionsRecordedSince = %q, %v; want %q", got, err, fixedNow)
	}
}
