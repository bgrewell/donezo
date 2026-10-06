package summary

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/bgrewell/donezo/internal/store"
)

// Detail levels: how much of each item a summary carries.
const (
	// DetailHeadline is counts and hours only — the shape of the period.
	DetailHeadline = "headline"
	// DetailItems adds what happened, one line per item. The default.
	DetailItems = "items"
	// DetailFull adds each item's details and links, for writing it up.
	DetailFull = "full"
)

// Details lists the detail levels.
var Details = []string{DetailHeadline, DetailItems, DetailFull}

// stallStatuses are the project statuses a summary reports as stalled time.
var stallStatuses = map[string]bool{"waiting": true, "blocked": true}

// Options selects and shapes a summary. The zero value is "everything in the
// period, items detail" — except planned work, which is a plan rather than
// something that happened.
type Options struct {
	// ProjectIDs limits the summary to these projects; empty means all.
	ProjectIDs []string
	// Types limits activities to these types; empty means all. It does not
	// filter completed tasks, which have no type.
	Types []string
	// Tags limits the summary to work tagged with any of these, on the
	// activity itself or on its project. Completed tasks match through their
	// project's tags.
	Tags []string
	// IncludePlanned counts planned (future/tentative) activities too.
	IncludePlanned bool
	// ExcludeCatchall leaves out the space's Miscellaneous project.
	ExcludeCatchall bool
	// Compare adds the previous period of the same length, totals only.
	Compare bool
	// Detail is one of Details; empty is DetailItems.
	Detail string
	// ItemLimit, when positive, caps every list in the result at ItemLimit
	// entries: the projects at the busiest, and each project's items,
	// completions, status changes and stalls (and the unfiled completions)
	// at the most recent. The rest are counted in the *Omitted fields and a
	// note; totals always count everything. For callers with a context
	// budget — an MCP client — where a year at full detail would not fit.
	ItemLimit int
}

// Validate checks the enumerated options.
func (o Options) Validate() error {
	if o.Detail != "" && !oneOf(o.Detail, Details) {
		return fmt.Errorf("detail must be one of %s", strings.Join(Details, ", "))
	}
	for _, t := range o.Types {
		if !oneOf(t, activityTypes) {
			return fmt.Errorf("types must be among %s", strings.Join(activityTypes, ", "))
		}
	}
	return nil
}

// activityTypes mirrors ActivityType.
var activityTypes = []string{"work", "research", "meeting", "decision", "blocker", "milestone"}

// Input is what a summary is built from.
type Input struct {
	State         store.SpaceState
	StatusChanges []store.ProjectStatusChange
	// RecordedSince is when the space began recording completion times (an
	// RFC 3339 instant; "" if unknown). Undated done tasks were finished
	// before it, so they are only counted for a period that starts earlier.
	RecordedSince string
}

// Summary is what got done over a period.
type Summary struct {
	Period Period `json:"period"`
	Totals Totals `json:"totals"`
	// Projects that saw any activity, completion or status change in the
	// period, busiest first; the catch-all sorts last.
	Projects []ProjectSummary `json:"projects"`
	// ProjectsOmitted counts the least busy projects left out under
	// Options.ItemLimit; their work is still in Totals.
	ProjectsOmitted int `json:"projectsOmitted,omitempty"`
	// UnfiledTasksCompleted are tasks with no project finished in the period.
	UnfiledTasksCompleted []TaskRef `json:"unfiledTasksCompleted,omitempty"`
	// UnfiledOmitted counts older unfiled completions left out under
	// Options.ItemLimit.
	UnfiledOmitted int `json:"unfiledOmitted,omitempty"`
	// Previous is the comparison period, when asked for.
	Previous *Comparison `json:"previous,omitempty"`
	// Notes flag what the numbers cannot see, so a reader — or a model
	// writing it up — does not mistake a gap in the record for idle time.
	Notes []string `json:"notes,omitempty"`
}

// Totals are the period's headline numbers.
type Totals struct {
	Activities int `json:"activities"`
	// Hours sums logged effort; activities without an estimate add nothing.
	Hours float64 `json:"hours"`
	// ActiveDays counts distinct days with at least one activity.
	ActiveDays        int            `json:"activeDays"`
	TasksCompleted    int            `json:"tasksCompleted"`
	ProjectsCompleted int            `json:"projectsCompleted"`
	ByType            map[string]int `json:"byType"`
	// UndatedDone counts done tasks in scope that have no completion date at
	// all — finished at some point, so possibly in this period.
	UndatedDone int `json:"undatedDone"`
	// EstimatedCompletions counts the tasks and projects completed in the
	// period whose date is an estimate (completedSource inferred).
	EstimatedCompletions int `json:"estimatedCompletions"`
}

// ProjectSummary is one project's share of the period.
type ProjectSummary struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Color    string `json:"color"`
	Status   string `json:"status"`
	Catchall bool   `json:"catchall,omitempty"`

	Activities     int            `json:"activities"`
	Hours          float64        `json:"hours"`
	ActiveDays     int            `json:"activeDays"`
	ByType         map[string]int `json:"byType"`
	TasksCompleted int            `json:"tasksCompleted"`
	// CompletedOn is the local day the project was completed, when that was
	// in the period.
	CompletedOn     string `json:"completedOn,omitempty"`
	CompletedSource string `json:"completedSource,omitempty"`

	// Items is what happened, oldest first (items and full detail only).
	Items []Item `json:"items,omitempty"`
	// ItemsOmitted counts older items left out under Options.ItemLimit.
	ItemsOmitted int `json:"itemsOmitted,omitempty"`
	// Completed are the tasks finished in the period (items and full only).
	Completed []TaskRef `json:"completed,omitempty"`
	// CompletedOmitted counts older completions left out under ItemLimit.
	CompletedOmitted int `json:"completedOmitted,omitempty"`
	// StatusChanges in the period, oldest first.
	StatusChanges []StatusChange `json:"statusChanges,omitempty"`
	// StatusChangesOmitted counts older changes left out under ItemLimit.
	StatusChangesOmitted int `json:"statusChangesOmitted,omitempty"`
	// Stalled are the stretches of the period spent waiting or blocked, as
	// far as the status history records.
	Stalled []Span `json:"stalled,omitempty"`
	// StalledOmitted counts older stretches left out under ItemLimit.
	StalledOmitted int `json:"stalledOmitted,omitempty"`

	// Where it stands now: what is next and what it waits on.
	NextAction string `json:"nextAction,omitempty"`
	WaitingOn  string `json:"waitingOn,omitempty"`
}

// Item is one activity.
type Item struct {
	ID          string               `json:"id"`
	Date        string               `json:"date"`
	Type        string               `json:"type"`
	Title       string               `json:"title"`
	EffortHours *float64             `json:"effortHours,omitempty"`
	Planned     bool                 `json:"planned,omitempty"`
	TaskID      string               `json:"taskId,omitempty"`
	Details     string               `json:"details,omitempty"`
	Links       []store.ActivityLink `json:"links,omitempty"`
}

// TaskRef is one completed task.
type TaskRef struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// CompletedOn is the local day it was finished.
	CompletedOn string `json:"completedOn"`
	// CompletedSource says how sure that day is: recorded, inferred, manual.
	CompletedSource string `json:"completedSource,omitempty"`
	Details         string `json:"details,omitempty"`
}

// StatusChange is a project status change, on the local day it happened.
type StatusChange struct {
	From string `json:"from,omitempty"`
	To   string `json:"to"`
	On   string `json:"on"`
}

// Span is a stretch of local days spent in one status, clipped to the period.
type Span struct {
	Status string `json:"status"`
	From   string `json:"from"`
	To     string `json:"to"`
	// Ongoing is true when the status had not changed again by the end of
	// the record.
	Ongoing bool `json:"ongoing,omitempty"`
}

// Comparison is a previous period's totals.
type Comparison struct {
	Period Period `json:"period"`
	Totals Totals `json:"totals"`
}

// Build summarizes in over period p.
func Build(in Input, p Period, opt Options, now time.Time, loc *time.Location) (Summary, error) {
	if err := opt.Validate(); err != nil {
		return Summary{}, err
	}
	if opt.Detail == "" {
		opt.Detail = DetailItems
	}
	sc := newScope(in.State, opt)
	if err := sc.checkProjects(opt.ProjectIDs); err != nil {
		return Summary{}, err
	}

	out := Summary{Period: p, Projects: []ProjectSummary{}}
	out.Totals, out.Projects, out.UnfiledTasksCompleted = sc.collect(in, p, opt, now, loc, true)
	if opt.Compare {
		prev := p.Previous(loc)
		totals, _, _ := sc.collect(in, prev, opt, now, loc, false)
		out.Previous = &Comparison{Period: prev, Totals: totals}
	}
	omitted := 0
	if n := opt.ItemLimit; n > 0 {
		if len(out.Projects) > n {
			out.ProjectsOmitted = len(out.Projects) - n
			out.Projects = out.Projects[:n]
			omitted += out.ProjectsOmitted
		}
		for i := range out.Projects {
			ps := &out.Projects[i]
			ps.Items, ps.ItemsOmitted = latest(ps.Items, n)
			ps.Completed, ps.CompletedOmitted = latest(ps.Completed, n)
			ps.StatusChanges, ps.StatusChangesOmitted = latest(ps.StatusChanges, n)
			ps.Stalled, ps.StalledOmitted = latest(ps.Stalled, n)
			omitted += ps.ItemsOmitted + ps.CompletedOmitted + ps.StatusChangesOmitted + ps.StalledOmitted
		}
		out.UnfiledTasksCompleted, out.UnfiledOmitted = latest(out.UnfiledTasksCompleted, n)
		omitted += out.UnfiledOmitted
	}
	// Notes read only the totals, which the cap never touches.
	out.Notes = notes(out)
	if omitted > 0 {
		out.Notes = append(out.Notes, fmt.Sprintf(
			"Every list here is capped at %d entries (the busiest projects; the most recent of everything else), so %d are left out of the lists — the *Omitted fields say where. Totals still count everything. Narrow the period or the projects to see them.",
			opt.ItemLimit, omitted))
	}
	return out, nil
}

// latest keeps the last n entries of an oldest-first list and reports how
// many were dropped.
func latest[T any](list []T, n int) ([]T, int) {
	if len(list) <= n {
		return list, 0
	}
	return list[len(list)-n:], len(list) - n
}

// scope is the filter, applied the same way to every period.
type scope struct {
	projects map[string]store.Project
	opt      Options
	types    map[string]bool
	tags     map[string]bool
	only     map[string]bool
}

func newScope(st store.SpaceState, opt Options) *scope {
	sc := &scope{projects: map[string]store.Project{}, opt: opt}
	for _, p := range st.Projects {
		sc.projects[p.ID] = p
	}
	sc.types = set(opt.Types)
	sc.tags = set(lower(opt.Tags))
	sc.only = set(opt.ProjectIDs)
	return sc
}

func (sc *scope) checkProjects(ids []string) error {
	for _, id := range ids {
		if _, ok := sc.projects[id]; !ok {
			return fmt.Errorf("project %q not found", id)
		}
	}
	return nil
}

// projectInScope applies the project-level filters. projectID may be empty
// (an unfiled task), which only the project filter excludes.
func (sc *scope) projectInScope(projectID string) bool {
	if projectID == "" {
		return len(sc.only) == 0 && len(sc.tags) == 0
	}
	p, ok := sc.projects[projectID]
	if !ok {
		return false
	}
	if len(sc.only) > 0 && !sc.only[projectID] {
		return false
	}
	if sc.opt.ExcludeCatchall && p.Catchall {
		return false
	}
	return true
}

func (sc *scope) tagMatch(own []string, projectID string) bool {
	if len(sc.tags) == 0 {
		return true
	}
	for _, t := range own {
		if sc.tags[strings.ToLower(t)] {
			return true
		}
	}
	for _, t := range sc.projects[projectID].Tags {
		if sc.tags[strings.ToLower(t)] {
			return true
		}
	}
	return false
}

func (sc *scope) activityInScope(a store.ActivityEntry) bool {
	if a.Planned != nil && *a.Planned && !sc.opt.IncludePlanned {
		return false
	}
	if len(sc.types) > 0 && !sc.types[a.Type] {
		return false
	}
	return sc.projectInScope(a.ProjectID) && sc.tagMatch(a.Tags, a.ProjectID)
}

func (sc *scope) taskInScope(t store.TaskItem) bool {
	pid := ""
	if t.ProjectID != nil {
		pid = *t.ProjectID
	}
	return t.Status == "done" && sc.projectInScope(pid) && (pid == "" || sc.tagMatch(nil, pid))
}

// collect gathers one period. withDetail false skips everything only the
// main period reports (lists, status history), for the comparison.
func (sc *scope) collect(in Input, p Period, opt Options, now time.Time, loc *time.Location, withDetail bool) (Totals, []ProjectSummary, []TaskRef) {
	tot := Totals{ByType: map[string]int{}}
	byProject := map[string]*ProjectSummary{}
	days := map[string]map[string]bool{} // project -> active days
	allDays := map[string]bool{}
	get := func(id string) *ProjectSummary {
		if ps, ok := byProject[id]; ok {
			return ps
		}
		pr := sc.projects[id]
		ps := &ProjectSummary{
			ID: id, Name: pr.Name, Color: pr.Color, Status: pr.Status, Catchall: pr.Catchall,
			ByType: map[string]int{}, NextAction: pr.NextAction,
		}
		if pr.WaitingOn != nil {
			ps.WaitingOn = *pr.WaitingOn
		}
		byProject[id] = ps
		days[id] = map[string]bool{}
		return ps
	}
	lists := withDetail && opt.Detail != DetailHeadline
	full := opt.Detail == DetailFull

	acts := append([]store.ActivityEntry(nil), in.State.Activities...)
	sort.SliceStable(acts, func(i, j int) bool { return acts[i].Date < acts[j].Date })
	for _, a := range acts {
		if !p.Contains(a.Date) || !sc.activityInScope(a) {
			continue
		}
		ps := get(a.ProjectID)
		ps.Activities++
		ps.ByType[a.Type]++
		tot.Activities++
		tot.ByType[a.Type]++
		if a.EffortHours != nil {
			ps.Hours += *a.EffortHours
			tot.Hours += *a.EffortHours
		}
		days[a.ProjectID][a.Date] = true
		allDays[a.Date] = true
		if lists {
			it := Item{ID: a.ID, Date: a.Date, Type: a.Type, Title: a.Title, EffortHours: a.EffortHours}
			if a.Planned != nil && *a.Planned {
				it.Planned = true
			}
			if a.TaskID != nil {
				it.TaskID = *a.TaskID
			}
			if full {
				it.Details = a.Details
				it.Links = a.Links
			}
			ps.Items = append(ps.Items, it)
		}
	}

	var unfiled []TaskRef
	tasks := append([]store.TaskItem(nil), in.State.Tasks...)
	sort.SliceStable(tasks, func(i, j int) bool { return deref(tasks[i].CompletedAt) < deref(tasks[j].CompletedAt) })
	for _, t := range tasks {
		if !sc.taskInScope(t) {
			continue
		}
		if t.CompletedAt == nil {
			if undatedMayBelong(t, in.RecordedSince, p, loc) {
				tot.UndatedDone++
			}
			continue
		}
		on, ok := localDay(*t.CompletedAt, loc)
		if !ok || !p.Contains(on) {
			continue
		}
		tot.TasksCompleted++
		if deref(t.CompletedSource) == store.CompletedInferred {
			tot.EstimatedCompletions++
		}
		ref := TaskRef{ID: t.ID, Title: t.Title, CompletedOn: on, CompletedSource: deref(t.CompletedSource)}
		if full {
			ref.Details = t.Details
		}
		if t.ProjectID == nil {
			if lists {
				unfiled = append(unfiled, ref)
			}
			continue
		}
		ps := get(*t.ProjectID)
		ps.TasksCompleted++
		if lists {
			ps.Completed = append(ps.Completed, ref)
		}
	}

	for _, pr := range in.State.Projects {
		if pr.CompletedAt == nil || !sc.projectInScope(pr.ID) || !sc.tagMatch(nil, pr.ID) {
			continue
		}
		on, ok := localDay(*pr.CompletedAt, loc)
		if !ok || !p.Contains(on) {
			continue
		}
		tot.ProjectsCompleted++
		if deref(pr.CompletedSource) == store.CompletedInferred {
			tot.EstimatedCompletions++
		}
		ps := get(pr.ID)
		ps.CompletedOn = on
		ps.CompletedSource = deref(pr.CompletedSource)
	}

	if lists {
		sc.statusHistory(in.StatusChanges, in.RecordedSince, p, now, loc, get)
	}

	out := make([]ProjectSummary, 0, len(byProject))
	for id, ps := range byProject {
		ps.ActiveDays = len(days[id])
		ps.Hours = round2(ps.Hours)
		out = append(out, *ps)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Catchall != b.Catchall {
			return !a.Catchall
		}
		if a.Hours != b.Hours {
			return a.Hours > b.Hours
		}
		if a.Activities+a.TasksCompleted != b.Activities+b.TasksCompleted {
			return a.Activities+a.TasksCompleted > b.Activities+b.TasksCompleted
		}
		return a.Name < b.Name
	})
	tot.ActiveDays = len(allDays)
	tot.Hours = round2(tot.Hours)
	return tot, out, unfiled
}

// statusHistory adds each in-scope project's status changes in the period
// and the stretches it spent waiting or blocked. A project whose only news
// in the period is a status change, or a stall, still appears.
//
// The log covers every change since the space began recording them
// (recordedSince), so a project's status before its first logged change —
// or its current status, if it has none — is known to have held from the
// later of that moment and the project's creation. Before that it is
// unknown, and a stall is not stretched back into it.
func (sc *scope) statusHistory(changes []store.ProjectStatusChange, recordedSince string, p Period, now time.Time, loc *time.Location, get func(string) *ProjectSummary) {
	byProject := map[string][]store.ProjectStatusChange{}
	for _, c := range changes {
		byProject[c.ProjectID] = append(byProject[c.ProjectID], c)
	}
	today := now.In(loc).Format(day)
	ids := make([]string, 0, len(sc.projects))
	for id := range sc.projects {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, pid := range ids {
		pr := sc.projects[pid]
		if !sc.projectInScope(pid) || !sc.tagMatch(nil, pid) {
			continue
		}
		cs := byProject[pid]
		sort.SliceStable(cs, func(i, j int) bool { return cs[i].ChangedAt < cs[j].ChangedAt })

		status := pr.Status
		if len(cs) > 0 {
			status = cs[0].FromStatus
		}
		since := ""
		if recordedSince != "" {
			known := recordedSince
			if pr.CreatedAt > known {
				known = pr.CreatedAt
			}
			since, _ = localDay(known, loc)
		}

		var inPeriod []StatusChange
		var spans []Span
		for _, c := range cs {
			on, ok := localDay(c.ChangedAt, loc)
			if !ok {
				continue
			}
			if p.Contains(on) {
				inPeriod = append(inPeriod, StatusChange{From: c.FromStatus, To: c.ToStatus, On: on})
			}
			if stallStatuses[status] {
				spans = appendSpan(spans, status, since, on, false, p)
			}
			status, since = c.ToStatus, on
		}
		if stallStatuses[status] {
			spans = appendSpan(spans, status, since, today, true, p)
		}
		if len(inPeriod) == 0 && len(spans) == 0 {
			continue
		}
		ps := get(pid)
		ps.StatusChanges = inPeriod
		ps.Stalled = spans
	}
}

// appendSpan clips [from, to] to p and appends it when anything is left.
// An empty from means the start is unknown; the stretch is taken from the
// period start, the most that can be said without a record.
func appendSpan(spans []Span, status, from, to string, ongoing bool, p Period) []Span {
	if from == "" || from < p.From {
		from = p.From
	}
	if to > p.To {
		to = p.To
		ongoing = false
	}
	if from > to {
		return spans
	}
	return append(spans, Span{Status: status, From: from, To: to, Ongoing: ongoing})
}

// notes says what the record cannot. Phrased for a reader, not a log.
func notes(s Summary) []string {
	var out []string
	if s.Totals.UndatedDone > 0 {
		out = append(out, fmt.Sprintf(
			"%d done task(s) in scope have no recorded completion date, so they may or may not belong to this period.",
			s.Totals.UndatedDone))
	}
	inferred := s.Totals.EstimatedCompletions
	if inferred > 0 {
		out = append(out, fmt.Sprintf(
			"%d completion date(s) here are estimates, reconstructed for items finished before donezo recorded the moment.",
			inferred))
	}
	if s.Totals.Activities > 0 && s.Totals.Hours == 0 {
		out = append(out, "No effort estimates were logged, so hours are not a measure of this period.")
	}
	return out
}

// undatedMayBelong reports whether an undated done task could have been
// finished in p. One the person marked unknown (source manual) could belong
// anywhere. One never dated was finished before recording began — every
// completion since is stamped — so only a period starting on or before that
// day can hold it.
func undatedMayBelong(t store.TaskItem, recordedSince string, p Period, loc *time.Location) bool {
	if recordedSince == "" || deref(t.CompletedSource) == store.CompletedManual {
		return true
	}
	since, ok := localDay(recordedSince, loc)
	return !ok || p.From <= since
}

func localDay(instant string, loc *time.Location) (string, bool) {
	t, err := time.Parse(time.RFC3339, instant)
	if err != nil {
		return "", false
	}
	return t.In(loc).Format(day), true
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func set(vs []string) map[string]bool {
	m := make(map[string]bool, len(vs))
	for _, v := range vs {
		m[v] = true
	}
	return m
}

func lower(vs []string) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = strings.ToLower(v)
	}
	return out
}

func oneOf(v string, allowed []string) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}
