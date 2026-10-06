package summary

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bgrewell/donezo/internal/store"
)

func ptr[T any](v T) *T { return &v }

// fixtureNow is Friday 2026-10-09, noon UTC.
var fixtureNow = time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)

// fixture is one space with a week's work in 2026-10-05..11 and a little
// either side of it, built so every filter has something to exclude.
func fixture() Input {
	act := func(id, project, date, typ string, hours float64, tags ...string) store.ActivityEntry {
		a := store.ActivityEntry{
			ID: id, ProjectID: project, Date: date, Type: typ, Title: "act " + id,
			Details: "details of " + id, Source: "manual", Tags: tags, Links: []store.ActivityLink{},
		}
		if hours > 0 {
			a.EffortHours = ptr(hours)
		}
		return a
	}
	task := func(id string, project *string, at, src string) store.TaskItem {
		t := store.TaskItem{ID: id, ProjectID: project, Title: "task " + id, Status: "done", CreatedAt: "2026-09-01", Details: "more on " + id}
		if at != "" {
			t.CompletedAt, t.CompletedSource = ptr(at), ptr(src)
		}
		return t
	}
	planned := act("a6", "loom", "2026-10-09", "work", 5)
	planned.Planned = ptr(true)
	open := task("t5", ptr("loom"), "", "")
	open.Status = "open"

	return Input{
		State: store.SpaceState{
			Projects: []store.Project{
				{ID: "loom", Name: "Loom", Color: "blue", Status: "active", Tags: []string{"go"}, NextAction: "ship it"},
				{ID: "ops", Name: "Ops", Color: "green", Status: "active", Tags: []string{"infra"}},
				{ID: "misc", Name: "Miscellaneous", Color: "steel", Status: "active", Catchall: true},
				{ID: "iso", Name: "ISO Kit", Color: "tan", Status: "completed",
					CompletedAt: ptr("2026-10-06T18:00:00Z"), CompletedSource: ptr(store.CompletedInferred)},
			},
			Activities: []store.ActivityEntry{
				act("a1", "loom", "2026-10-05", "work", 2),
				act("a2", "loom", "2026-10-06", "decision", 0),
				act("a3", "ops", "2026-10-07", "meeting", 1.5, "incident"),
				act("a4", "misc", "2026-10-08", "work", 0.5),
				act("a5", "loom", "2026-10-04", "work", 3),
				planned,
				act("a7", "ops", "2026-09-30", "work", 1),
			},
			Tasks: []store.TaskItem{
				task("t1", ptr("loom"), "2026-10-06T15:00:00Z", store.CompletedRecorded),
				task("t2", ptr("loom"), "", ""),
				task("t3", nil, "2026-10-07T10:00:00Z", store.CompletedManual),
				task("t4", ptr("ops"), "2026-10-01T10:00:00Z", store.CompletedRecorded),
				open,
				task("t6", ptr("loom"), "2026-10-08T12:00:00Z", store.CompletedInferred),
			},
		},
		StatusChanges: []store.ProjectStatusChange{
			{ProjectID: "loom", FromStatus: "active", ToStatus: "waiting", ChangedAt: "2026-09-20T09:00:00Z"},
			{ProjectID: "ops", FromStatus: "active", ToStatus: "blocked", ChangedAt: "2026-10-06T09:00:00Z"},
			{ProjectID: "ops", FromStatus: "blocked", ToStatus: "active", ChangedAt: "2026-10-08T09:00:00Z"},
		},
	}
}

func week(t *testing.T) Period {
	t.Helper()
	p, err := ResolvePeriod("", "2026-10-05", "2026-10-11", time.Monday, fixtureNow, time.UTC)
	if err != nil {
		t.Fatalf("period: %v", err)
	}
	return p
}

func build(t *testing.T, opt Options) Summary {
	t.Helper()
	s, err := Build(fixture(), week(t), opt, fixtureNow, time.UTC)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return s
}

func projectIDs(s Summary) []string {
	var ids []string
	for _, p := range s.Projects {
		ids = append(ids, p.ID)
	}
	return ids
}

func find(t *testing.T, s Summary, id string) ProjectSummary {
	t.Helper()
	for _, p := range s.Projects {
		if p.ID == id {
			return p
		}
	}
	t.Fatalf("project %s not in summary (have %v)", id, projectIDs(s))
	return ProjectSummary{}
}

func TestBuildDefaults(t *testing.T) {
	t.Parallel()
	s := build(t, Options{})

	want := Totals{
		Activities: 4, Hours: 4, ActiveDays: 4, TasksCompleted: 3, ProjectsCompleted: 1,
		ByType: map[string]int{"work": 2, "decision": 1, "meeting": 1}, UndatedDone: 1,
		EstimatedCompletions:    2, // t6 and iso
		ActivitiesWithoutEffort: 1, // a2
	}
	if !reflect.DeepEqual(s.Totals, want) {
		t.Errorf("totals = %+v\nwant     %+v", s.Totals, want)
	}
	// Busiest first by hours; a project with only a completion still shows;
	// the catch-all is last whatever its numbers.
	if got := strings.Join(projectIDs(s), ","); got != "loom,ops,iso,misc" {
		t.Errorf("projects = %s, want loom,ops,iso,misc", got)
	}

	loom := find(t, s, "loom")
	if loom.Activities != 2 || loom.Hours != 2 || loom.TasksCompleted != 2 || loom.NextAction != "ship it" {
		t.Errorf("loom = %+v", loom)
	}
	if len(loom.Items) != 2 || loom.Items[0].ID != "a1" || loom.Items[0].Details != "" {
		t.Errorf("loom items = %+v, want a1,a2 without details at items level", loom.Items)
	}
	if len(loom.Completed) != 2 || loom.Completed[0].ID != "t1" || loom.Completed[1].CompletedOn != "2026-10-08" {
		t.Errorf("loom completed = %+v", loom.Completed)
	}
	// Waiting since before the period and never changed back: the whole
	// period so far, still ongoing.
	if want := []Span{{Status: "waiting", From: "2026-10-05", To: "2026-10-09", Ongoing: true}}; !reflect.DeepEqual(loom.Stalled, want) {
		t.Errorf("loom stalled = %+v, want %+v", loom.Stalled, want)
	}
	if len(loom.StatusChanges) != 0 {
		t.Errorf("loom status changes = %+v, want none in period", loom.StatusChanges)
	}

	ops := find(t, s, "ops")
	if want := []Span{{Status: "blocked", From: "2026-10-06", To: "2026-10-08"}}; !reflect.DeepEqual(ops.Stalled, want) {
		t.Errorf("ops stalled = %+v, want %+v", ops.Stalled, want)
	}
	if len(ops.StatusChanges) != 2 || ops.StatusChanges[0].On != "2026-10-06" {
		t.Errorf("ops status changes = %+v", ops.StatusChanges)
	}

	iso := find(t, s, "iso")
	if iso.CompletedOn != "2026-10-06" || iso.CompletedSource != store.CompletedInferred {
		t.Errorf("iso = %+v", iso)
	}
	if len(s.UnfiledTasksCompleted) != 1 || s.UnfiledTasksCompleted[0].ID != "t3" {
		t.Errorf("unfiled = %+v", s.UnfiledTasksCompleted)
	}
	if s.Previous != nil {
		t.Error("previous present without Compare")
	}

	notes := strings.Join(s.Notes, " | ")
	if !strings.Contains(notes, "1 done task(s)") || !strings.Contains(notes, "2 completion date(s) here are estimates") {
		t.Errorf("notes = %q", notes)
	}
}

func TestBuildFilters(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name              string
		opt               Options
		activities, tasks int
		projects          string
		unfiled           int
		hours             float64
		extraCheck        func(t *testing.T, s Summary)
	}{
		{name: "exclude catch-all", opt: Options{ExcludeCatchall: true},
			activities: 3, tasks: 3, projects: "loom,ops,iso", unfiled: 1, hours: 3.5},
		// Types filter activities only; completions have no type.
		{name: "types", opt: Options{Types: []string{"meeting"}},
			activities: 1, tasks: 3, projects: "ops,loom,iso", unfiled: 1, hours: 1.5},
		// An activity's own tag; projects tagged otherwise contribute nothing,
		// and unfiled tasks cannot match a tag.
		{name: "activity tag", opt: Options{Tags: []string{"INCIDENT"}},
			activities: 1, tasks: 0, projects: "ops", unfiled: 0, hours: 1.5,
			extraCheck: func(t *testing.T, s Summary) {
				if ops := find(t, s, "ops"); len(ops.Stalled) != 0 {
					t.Errorf("ops matched by an activity tag only, yet carries status history: %+v", ops.Stalled)
				}
			}},
		// A project's tag brings in all of its work.
		{name: "project tag", opt: Options{Tags: []string{"go"}},
			activities: 2, tasks: 2, projects: "loom", unfiled: 0, hours: 2},
		{name: "one project", opt: Options{ProjectIDs: []string{"ops"}},
			activities: 1, tasks: 0, projects: "ops", unfiled: 0, hours: 1.5},
		{name: "planned included", opt: Options{IncludePlanned: true},
			activities: 5, tasks: 3, projects: "loom,ops,iso,misc", unfiled: 1, hours: 9,
			extraCheck: func(t *testing.T, s Summary) {
				items := find(t, s, "loom").Items
				if len(items) != 3 || !items[2].Planned {
					t.Errorf("loom items = %+v, want the planned one flagged", items)
				}
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := build(t, tt.opt)
			if s.Totals.Activities != tt.activities || s.Totals.TasksCompleted != tt.tasks || s.Totals.Hours != tt.hours {
				t.Errorf("totals = %+v, want %d activities, %d tasks, %vh", s.Totals, tt.activities, tt.tasks, tt.hours)
			}
			if got := strings.Join(projectIDs(s), ","); got != tt.projects {
				t.Errorf("projects = %s, want %s", got, tt.projects)
			}
			if len(s.UnfiledTasksCompleted) != tt.unfiled {
				t.Errorf("unfiled = %d, want %d", len(s.UnfiledTasksCompleted), tt.unfiled)
			}
			if tt.extraCheck != nil {
				tt.extraCheck(t, s)
			}
		})
	}
}

func TestBuildDetailAndCompare(t *testing.T) {
	t.Parallel()
	head := build(t, Options{Detail: DetailHeadline, Compare: true})
	for _, p := range head.Projects {
		if p.Items != nil || p.Completed != nil || p.StatusChanges != nil || p.Stalled != nil {
			t.Errorf("%s carries lists at headline detail: %+v", p.ID, p)
		}
	}
	// The estimate note does not depend on the lists being there.
	if !strings.Contains(strings.Join(head.Notes, " "), "2 completion date(s) here are estimates") {
		t.Errorf("headline notes = %q, want the estimate count", head.Notes)
	}
	if head.UnfiledTasksCompleted != nil || head.Totals.TasksCompleted != 3 {
		t.Errorf("headline: unfiled=%v totals=%+v", head.UnfiledTasksCompleted, head.Totals)
	}
	prev := head.Previous
	if prev == nil || prev.Period.From != "2026-09-28" || prev.Period.To != "2026-10-04" {
		t.Fatalf("previous = %+v", prev)
	}
	if prev.Totals.Activities != 2 || prev.Totals.Hours != 4 || prev.Totals.TasksCompleted != 1 {
		t.Errorf("previous totals = %+v, want a5+a7 (4h) and t4", prev.Totals)
	}

	full := build(t, Options{Detail: DetailFull})
	loom := find(t, full, "loom")
	if loom.Items[0].Details != "details of a1" || loom.Completed[0].Details != "more on t1" {
		t.Errorf("full detail missing details: %+v / %+v", loom.Items[0], loom.Completed[0])
	}
}

// Undated done tasks were finished before recording began, so a period that
// starts after that day cannot contain them — and should not say it might.
func TestBuildUndatedOnlyBeforeRecording(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		since string
		want  int
	}{
		{"", 1},                     // unknown: it may belong anywhere
		{"2026-10-07T09:00:00Z", 1}, // recording began mid-period
		{"2026-10-05T00:30:00Z", 1}, // on the period's first day
		{"2026-10-04T23:00:00Z", 0}, // before the period began
	} {
		in := fixture()
		in.RecordedSince = tt.since
		s, err := Build(in, week(t), Options{}, fixtureNow, time.UTC)
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		if s.Totals.UndatedDone != tt.want {
			t.Errorf("since %q: undated = %d, want %d", tt.since, s.Totals.UndatedDone, tt.want)
		}
		if hasNote := strings.Contains(strings.Join(s.Notes, " "), "no recorded completion date"); hasNote != (tt.want > 0) {
			t.Errorf("since %q: note present = %v", tt.since, hasNote)
		}
	}

	// A date the person cleared to unknown could be from any time, so it
	// stays in scope even for a period long after recording began.
	in := fixture()
	in.RecordedSince = "2026-09-01T00:00:00Z"
	for i := range in.State.Tasks {
		if in.State.Tasks[i].ID == "t2" {
			in.State.Tasks[i].CompletedSource = ptr(store.CompletedManual)
		}
	}
	s, err := Build(in, week(t), Options{}, fixtureNow, time.UTC)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if s.Totals.UndatedDone != 1 {
		t.Errorf("person-cleared date: undated = %d, want 1", s.Totals.UndatedDone)
	}
}

// A project already waiting or blocked when the status log began has no rows
// in it, but its stall is known from the later of that moment and the
// project's creation — and only from then.
func TestBuildStallWithoutHistory(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, since, created string
		want                 []Span
	}{
		{"blocked when recording began", "2026-10-07T08:00:00Z", "2026-09-01T00:00:00Z",
			[]Span{{Status: "blocked", From: "2026-10-07", To: "2026-10-09", Ongoing: true}}},
		{"created blocked after recording began", "2026-09-01T00:00:00Z", "2026-10-08T15:00:00Z",
			[]Span{{Status: "blocked", From: "2026-10-08", To: "2026-10-09", Ongoing: true}}},
		{"recording began before the period", "2026-09-01T00:00:00Z", "2026-08-01T00:00:00Z",
			[]Span{{Status: "blocked", From: "2026-10-05", To: "2026-10-09", Ongoing: true}}},
		{"recording start unknown", "", "2026-08-01T00:00:00Z",
			[]Span{{Status: "blocked", From: "2026-10-05", To: "2026-10-09", Ongoing: true}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			in := fixture()
			in.RecordedSince = tt.since
			in.State.Projects = append(in.State.Projects, store.Project{
				ID: "vendor", Name: "Vendor", Status: "blocked", CreatedAt: tt.created,
			})
			s, err := Build(in, week(t), Options{}, fixtureNow, time.UTC)
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			if got := find(t, s, "vendor").Stalled; !reflect.DeepEqual(got, tt.want) {
				t.Errorf("stalled = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// ItemLimit keeps each list to its most recent entries and says what it cut;
// totals still count everything.
func TestBuildItemLimit(t *testing.T) {
	t.Parallel()
	s := build(t, Options{ItemLimit: 1})
	loom := find(t, s, "loom")
	if len(loom.Items) != 1 || loom.Items[0].ID != "a2" || loom.ItemsOmitted != 1 {
		t.Errorf("loom items = %+v (omitted %d), want a2 with 1 omitted", loom.Items, loom.ItemsOmitted)
	}
	if len(loom.Completed) != 1 || loom.Completed[0].ID != "t6" || loom.CompletedOmitted != 1 {
		t.Errorf("loom completed = %+v (omitted %d), want t6 with 1 omitted", loom.Completed, loom.CompletedOmitted)
	}
	if s.Totals.Activities != 4 || s.Totals.TasksCompleted != 3 {
		t.Errorf("totals = %+v, want them uncapped", s.Totals)
	}
	// Only the busiest project survives a limit of one: 3 projects, plus one
	// item and one completion of loom's, are left out.
	if len(s.Projects) != 1 || s.ProjectsOmitted != 3 {
		t.Errorf("projects = %v (omitted %d), want loom with 3 omitted", projectIDs(s), s.ProjectsOmitted)
	}
	if !strings.Contains(strings.Join(s.Notes, " "), "so 5 are left out") {
		t.Errorf("notes = %q", s.Notes)
	}
	// Status changes and stalls are capped like everything else.
	in := fixture()
	in.StatusChanges = append(in.StatusChanges,
		store.ProjectStatusChange{ProjectID: "ops", FromStatus: "active", ToStatus: "paused", ChangedAt: "2026-10-09T09:00:00Z"})
	capped, err := Build(in, week(t), Options{ItemLimit: 2, ProjectIDs: []string{"ops"}}, fixtureNow, time.UTC)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	ops := find(t, capped, "ops")
	if len(ops.StatusChanges) != 2 || ops.StatusChangesOmitted != 1 || ops.StatusChanges[1].To != "paused" {
		t.Errorf("ops status changes = %+v (omitted %d), want the latest 2 with 1 omitted", ops.StatusChanges, ops.StatusChangesOmitted)
	}
	if none := build(t, Options{}); find(t, none, "loom").ItemsOmitted != 0 {
		t.Error("omitted without a limit")
	}
}

// The effort note is about estimates that are missing, not hours that add
// up to zero: an explicit zero is an estimate.
func TestBuildEffortNote(t *testing.T) {
	t.Parallel()
	mk := func(hours ...*float64) Input {
		in := Input{State: store.SpaceState{Projects: []store.Project{{ID: "p", Name: "P", Status: "active"}}}}
		for i, h := range hours {
			in.State.Activities = append(in.State.Activities, store.ActivityEntry{
				ID: fmt.Sprintf("a%d", i), ProjectID: "p", Date: "2026-10-06", Type: "work", Title: "x", EffortHours: h,
			})
		}
		return in
	}
	for _, tt := range []struct {
		name string
		in   Input
		want string // "" for no effort note
	}{
		{"all estimated", mk(ptr(1.0), ptr(2.0)), ""},
		{"estimated at zero", mk(ptr(0.0)), ""},
		{"some missing", mk(ptr(1.0), nil), "1 of 2 activities have no effort estimate"},
		{"none estimated", mk(nil, nil), "No effort estimates were logged"},
	} {
		s, err := Build(tt.in, week(t), Options{}, fixtureNow, time.UTC)
		if err != nil {
			t.Fatalf("%s: Build: %v", tt.name, err)
		}
		notes := strings.Join(s.Notes, " ")
		hasEffortNote := strings.Contains(notes, "effort estimate")
		if (tt.want == "") == hasEffortNote || (tt.want != "" && !strings.Contains(notes, tt.want)) {
			t.Errorf("%s: notes = %q, want %q", tt.name, notes, tt.want)
		}
	}
}

// Projects that tie on everything, name included, still come out in one
// order — by id — so a capped result keeps the same ones every time.
func TestBuildOrderIsDeterministic(t *testing.T) {
	t.Parallel()
	in := Input{State: store.SpaceState{}}
	for _, id := range []string{"p3", "p1", "p2"} {
		in.State.Projects = append(in.State.Projects, store.Project{ID: id, Name: "Same", Status: "active"})
		in.State.Activities = append(in.State.Activities, store.ActivityEntry{
			ID: "a-" + id, ProjectID: id, Date: "2026-10-06", Type: "work", Title: "x", EffortHours: ptr(1.0),
		})
	}
	for i := 0; i < 20; i++ {
		s, err := Build(in, week(t), Options{ItemLimit: 2}, fixtureNow, time.UTC)
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		if got := strings.Join(projectIDs(s), ","); got != "p1,p2" {
			t.Fatalf("run %d: projects = %s, want p1,p2", i, got)
		}
	}
}

func TestBuildRejects(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		opt  Options
		want string
	}{
		{"detail", Options{Detail: "verbose"}, "detail must be one of"},
		{"type", Options{Types: []string{"nap"}}, "types must be among"},
		{"project", Options{ProjectIDs: []string{"ghost"}}, `project "ghost" not found`},
	} {
		if _, err := Build(fixture(), week(t), tt.opt, fixtureNow, time.UTC); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: err = %v, want %q", tt.name, err, tt.want)
		}
	}
}

// A completion is placed on the person's day, not UTC's: 20:00 on Sunday in
// Los Angeles is already Monday in UTC, and belongs to the week that ends on
// that Sunday.
func TestBuildCompletionDayIsLocal(t *testing.T) {
	t.Parallel()
	la := mustLoad(t, "America/Los_Angeles")
	in := Input{State: store.SpaceState{
		Projects: []store.Project{{ID: "p", Name: "P", Status: "active"}},
		Tasks: []store.TaskItem{{ID: "late", ProjectID: ptr("p"), Title: "late", Status: "done",
			CreatedAt: "2026-10-01", CompletedAt: ptr("2026-10-12T03:00:00Z"), CompletedSource: ptr(store.CompletedRecorded)}},
	}}
	for _, tt := range []struct {
		loc  *time.Location
		want int
	}{{la, 1}, {time.UTC, 0}} {
		p, err := ResolvePeriod("", "2026-10-05", "2026-10-11", time.Monday, fixtureNow, tt.loc)
		if err != nil {
			t.Fatalf("period: %v", err)
		}
		s, err := Build(in, p, Options{}, fixtureNow, tt.loc)
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		if s.Totals.TasksCompleted != tt.want {
			t.Errorf("%s: tasks completed = %d, want %d", tt.loc, s.Totals.TasksCompleted, tt.want)
		}
	}
}
