package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bgrewell/donezo/internal/store"
)

// A calendar day is not an instant. These tests pin every tool that dates
// something to the caller's own zone, because the failure they guard against
// is invisible in the result: the entry looks perfectly ordinary, it is simply
// filed on the wrong day, and only shows up later as a timeline that disagrees
// with the person's memory.

// eveningClock is 2026-07-25 20:30 in Los Angeles and 2026-07-26 03:30 in UTC.
// Every assertion below turns on the two disagreeing: a date of 2026-07-26
// means the zone was ignored.
func eveningClock() time.Time {
	return time.Date(2026, 7, 26, 3, 30, 0, 0, time.UTC)
}

// lateClock is 2026-07-25 23:00 UTC, which is already 2026-07-26 09:00 in
// Sydney. Needed because at eveningClock Sydney and UTC agree on the day, so
// a Sydney assertion there would pass with the UTC bug still in place.
func lateClock() time.Time {
	return time.Date(2026, 7, 25, 23, 0, 0, 0, time.UTC)
}

const (
	losAngeles = "America/Los_Angeles"
	laDay      = "2026-07-25" // the day it is where the person is
	utcDay     = "2026-07-26" // the day it is in UTC at eveningClock
	sydney     = "Australia/Sydney"
	sydneyDay  = "2026-07-26" // at lateClock; UTC still says the 25th
	lateUTCDay = "2026-07-25"
)

// setTimezone stores an IANA zone on the fixture's user.
func (f *fixture) setTimezone(t *testing.T, name string) {
	t.Helper()
	if _, err := f.core.PatchUserSettings(context.Background(), f.user.ID,
		func(s *store.UserSettings) error {
			s.Timezone = name
			return nil
		}); err != nil {
		t.Fatalf("set timezone %q: %v", name, err)
	}
}

// dateOf runs a tool and returns the calendar date the entity it created
// landed on, read back from the store rather than the tool's reply.
type datedTool struct {
	name string
	tool string
	args string
	// read returns the stored date for the entity the tool created.
	read func(t *testing.T, f *fixture) string
}

func firstActivityDate(t *testing.T, f *fixture) string {
	t.Helper()
	acts, err := f.spaces.ListActivities(context.Background(), "sandbox")
	if err != nil {
		t.Fatalf("list activities: %v", err)
	}
	if len(acts) != 1 {
		t.Fatalf("activities = %d, want exactly 1", len(acts))
	}
	return acts[0].Date
}

func firstTaskCreatedAt(t *testing.T, f *fixture) string {
	t.Helper()
	tasks, err := f.spaces.ListTasks(context.Background(), "sandbox")
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("tasks = %d, want exactly 1", len(tasks))
	}
	return tasks[0].CreatedAt
}

// seededNoteID is the note every subtest plants so the convert_note cases
// have something to convert. Note readers skip it: it is scenery, not the
// thing the tool under test created.
const seededNoteID = "n-dated"

func firstNoteCreatedAt(t *testing.T, f *fixture) string {
	t.Helper()
	all, err := f.spaces.ListNotes(context.Background(), "sandbox")
	if err != nil {
		t.Fatalf("list notes: %v", err)
	}
	notes := make([]store.NoteItem, 0, len(all))
	for _, n := range all {
		if n.ID != seededNoteID {
			notes = append(notes, n)
		}
	}
	if len(notes) != 1 {
		t.Fatalf("created notes = %d, want exactly 1 (all: %+v)", len(notes), all)
	}
	return notes[0].CreatedAt
}

// datedTools is every tool that puts a date on something without being told
// one. Each has to resolve it in the caller's zone; a new one that forgets is
// exactly the bug this file exists for, so add it here when you add it there.
//
// convert_note earns its place the hard way: it and the zone parameter landed
// in two branches that were each green and merged cleanly into a tree that did
// not compile. A table that already named the tool would have made the
// omission a test failure rather than a broken main.
var datedTools = []datedTool{
	{
		name: "log_activity", tool: "log_activity",
		args: `{"space_id":"sandbox","project_id":"loom","title":"evening work"}`,
		read: firstActivityDate,
	},
	{
		name: "create_task", tool: "create_task",
		args: `{"space_id":"sandbox","title":"a task"}`,
		read: firstTaskCreatedAt,
	},
	{
		name: "create_note", tool: "create_note",
		args: `{"space_id":"sandbox","body":"a note"}`,
		read: firstNoteCreatedAt,
	},
	{
		name: "classify_inbox_item to task", tool: "classify_inbox_item",
		args: `{"space_id":"sandbox","inbox_id":"inb-seed","kind":"task"}`,
		read: firstTaskCreatedAt,
	},
	{
		name: "classify_inbox_item to note", tool: "classify_inbox_item",
		args: `{"space_id":"sandbox","inbox_id":"inb-seed","kind":"note"}`,
		read: firstNoteCreatedAt,
	},
	{
		name: "classify_inbox_item to activity", tool: "classify_inbox_item",
		args: `{"space_id":"sandbox","inbox_id":"inb-seed","kind":"activity","project_id":"loom"}`,
		read: firstActivityDate,
	},
	{
		name: "convert_note to task", tool: "convert_note",
		args: `{"space_id":"sandbox","note_id":"n-dated","kind":"task"}`,
		read: firstTaskCreatedAt,
	},
	{
		name: "convert_note to activity", tool: "convert_note",
		args: `{"space_id":"sandbox","note_id":"n-dated","kind":"activity","project_id":"loom"}`,
		read: firstActivityDate,
	},
}

// Every dating tool must use the caller's stored zone, not UTC and not the
// instance's. The instance default is deliberately set to the WRONG side of
// the date line here, so a tool that ignores the user's setting is caught
// rather than accidentally landing on the right answer.
func TestDatedToolsUseTheCallersTimezone(t *testing.T) {
	t.Parallel()
	for _, tc := range datedTools {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, WithClock(eveningClock), WithLocation(time.UTC))
			f.setTimezone(t, losAngeles)
			f.seedInbox(t, "something captured")
			f.seedNote(t, seededNoteID, "a note that turned out to be work", "", nil)

			if text, isErr := f.callTool(t, f.rw, tc.tool, tc.args); isErr {
				t.Fatalf("%s: %s", tc.tool, text)
			}
			if got := tc.read(t, f); got != laDay {
				t.Errorf("date = %q, want %q — 20:30 in Los Angeles, not %s in UTC", got, laDay, utcDay)
			}
		})
	}
}

// complete_task logs an activity of its own, on the same rule. It is separate
// because it needs a task to complete first.
func TestCompleteTaskLogsInTheCallersTimezone(t *testing.T) {
	t.Parallel()
	f := newFixture(t, WithClock(eveningClock), WithLocation(time.UTC))
	f.setTimezone(t, losAngeles)
	loom := "loom"
	if _, err := f.spaces.CreateTask(context.Background(), "sandbox", store.TaskItem{
		ID: "tsk-tz", ProjectID: &loom, Title: "finish it", Status: "open", CreatedAt: laDay,
	}); err != nil {
		t.Fatalf("seed task: %v", err)
	}

	if text, isErr := f.callTool(t, f.rw, "complete_task", `{"space_id":"sandbox","task_id":"tsk-tz"}`); isErr {
		t.Fatalf("complete_task: %s", text)
	}
	if got := firstActivityDate(t, f); got != laDay {
		t.Errorf("logged activity date = %q, want %q", got, laDay)
	}
}

// A completed_at given as a bare date is the caller's local day, recorded at
// their local noon — so it reads back as that day in their zone, which an
// instant at UTC midnight would not for anyone west of Greenwich.
func TestUpdateTaskCompletedAtIsTheCallersDay(t *testing.T) {
	t.Parallel()
	f := newFixture(t, WithClock(eveningClock), WithLocation(time.UTC))
	f.setTimezone(t, losAngeles)
	loom := "loom"
	if _, err := f.spaces.CreateTask(context.Background(), "sandbox", store.TaskItem{
		ID: "tsk-c", ProjectID: &loom, Title: "fix it", Status: "open", CreatedAt: "2026-06-01",
	}); err != nil {
		t.Fatalf("seed task: %v", err)
	}
	call := func(args string) (string, bool) {
		t.Helper()
		return f.callTool(t, f.rw, "update_task", `{"space_id":"sandbox","task_id":"tsk-c",`+args+`}`)
	}
	read := func() store.TaskItem {
		t.Helper()
		task, err := f.spaces.GetTask(context.Background(), "sandbox", "tsk-c")
		if err != nil {
			t.Fatalf("get task: %v", err)
		}
		return task
	}

	if text, isErr := call(`"completed_at":"2026-07-01"`); !isErr || !strings.Contains(text, "only be set on a done task") {
		t.Errorf("open task: isErr=%v text=%s", isErr, text)
	}
	if text, isErr := call(`"status":"done","completed_at":"2026-07-01"`); isErr {
		t.Fatalf("update_task: %s", text)
	}
	if got := read(); got.CompletedAt == nil || *got.CompletedAt != "2026-07-01T19:00:00Z" ||
		got.CompletedSource == nil || *got.CompletedSource != store.CompletedManual {
		t.Errorf("completed = %v/%v, want 2026-07-01T19:00:00Z (noon in Los Angeles), manual",
			got.CompletedAt, got.CompletedSource)
	}
	if text, isErr := call(`"completed_at":"tomorrowish"`); !isErr || !strings.Contains(text, "completed_at must be") {
		t.Errorf("bad value: isErr=%v text=%s", isErr, text)
	}
	if text, isErr := call(`"completed_at":""`); isErr {
		t.Fatalf("clear: %s", text)
	}
	if got := read(); got.Status != "done" || got.CompletedAt != nil ||
		got.CompletedSource == nil || *got.CompletedSource != store.CompletedManual {
		t.Errorf("after clear = %+v, want done with no date and source manual", got)
	}
}

// Local noon is built as a wall-clock time, not midnight plus twelve hours:
// on a daylight-saving day those differ by an hour, and the web picker (which
// sets the hour) would store a different instant for the same day. Days after
// today are refused; today before noon is clamped to now.
func TestCompletedAtArgLocalDay(t *testing.T) {
	t.Parallel()
	la, err := time.LoadLocation(losAngeles)
	if err != nil {
		t.Fatalf("load %s: %v", losAngeles, err)
	}
	now := eveningClock() // 2026-07-25 20:30 in Los Angeles
	morning := time.Date(2026, 7, 25, 9, 15, 0, 0, la)
	tests := []struct {
		name, arg string
		now       time.Time
		want      string // "" with wantErr
		wantErr   bool
	}{
		{"spring forward", "2026-03-08", now, "2026-03-08T19:00:00Z", false},
		{"fall back", "2025-11-02", now, "2025-11-02T20:00:00Z", false},
		{"today after noon", laDay, now, "2026-07-25T19:00:00Z", false},
		{"today before noon is now", laDay, morning, "2026-07-25T16:15:00Z", false},
		{"tomorrow", "2026-07-26", now, "", true},
		{"instant an hour ahead", now.Add(time.Hour).UTC().Format(time.RFC3339), now, "", true},
		{"instant a minute ahead is now", now.Add(time.Minute).UTC().Format(time.RFC3339), now, "2026-07-26T03:30:00Z", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, bad := completedAtArg(tt.arg, la, tt.now)
			if tt.wantErr {
				if bad == "" {
					t.Errorf("completedAtArg(%q) = %v, want refused", tt.arg, deref(got))
				}
				return
			}
			if bad != "" || got == nil || *got != tt.want {
				t.Errorf("completedAtArg(%q) = %q (%s), want %q", tt.arg, deref(got), bad, tt.want)
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

// summarize_work reads periods in the caller's zone, and is a read tool — a
// read-only token can call it. At eveningClock it is still Saturday
// 2026-07-25 in Los Angeles, so "today" holds what was just logged there,
// where UTC would already be on Sunday.
func TestSummarizeWorkInTheCallersTimezone(t *testing.T) {
	t.Parallel()
	f := newFixture(t, WithClock(eveningClock), WithLocation(time.UTC))
	f.setTimezone(t, losAngeles)
	if text, isErr := f.callTool(t, f.rw, "log_activity",
		`{"space_id":"sandbox","project_id":"loom","title":"evening work","effort_hours":1.5}`); isErr {
		t.Fatalf("log_activity: %s", text)
	}

	text, isErr := f.callTool(t, f.ro, "summarize_work", `{"space_id":"sandbox","period":"today","compare":true}`)
	if isErr {
		t.Fatalf("summarize_work: %s", text)
	}
	var got struct {
		Period struct {
			From, To, Timezone string
		} `json:"period"`
		Totals struct {
			Activities int     `json:"activities"`
			Hours      float64 `json:"hours"`
		} `json:"totals"`
		Projects []struct {
			ID    string `json:"id"`
			Items []struct {
				Title string `json:"title"`
			} `json:"items"`
		} `json:"projects"`
		Previous *struct{} `json:"previous"`
	}
	parseToolJSON(t, text, &got)
	if got.Period.From != laDay || got.Period.Timezone != losAngeles {
		t.Errorf("period = %+v, want %s in %s", got.Period, laDay, losAngeles)
	}
	if got.Totals.Activities != 1 || got.Totals.Hours != 1.5 || len(got.Projects) != 1 ||
		len(got.Projects[0].Items) != 1 || got.Projects[0].Items[0].Title != "evening work" || got.Previous == nil {
		t.Errorf("summary = %s", text)
	}

	// Lists are held to the read-tool bound; totals still count everything.
	for i := 0; i < maxItems+5; i++ {
		if _, err := f.spaces.CreateActivity(context.Background(), "sandbox", store.ActivityEntry{
			ID: fmt.Sprintf("bulk-%02d", i), ProjectID: "loom", Date: "2026-07-20", Type: "work",
			Title: "bulk", Source: "manual", Tags: []string{}, Links: []store.ActivityLink{},
		}); err != nil {
			t.Fatalf("seed bulk activity: %v", err)
		}
	}
	text, isErr = f.callTool(t, f.ro, "summarize_work",
		`{"space_id":"sandbox","from":"2026-07-01","to":"2026-07-31","detail":"full"}`)
	if isErr {
		t.Fatalf("summarize_work bulk: %s", text)
	}
	var bulk struct {
		Totals struct {
			Activities int `json:"activities"`
		} `json:"totals"`
		Projects []struct {
			Items        []json.RawMessage `json:"items"`
			ItemsOmitted int               `json:"itemsOmitted"`
		} `json:"projects"`
	}
	parseToolJSON(t, text, &bulk)
	if bulk.Totals.Activities != maxItems+6 || len(bulk.Projects[0].Items) != maxItems || bulk.Projects[0].ItemsOmitted != 6 {
		t.Errorf("bulk: totals=%d items=%d omitted=%d, want %d / %d / 6",
			bulk.Totals.Activities, len(bulk.Projects[0].Items), bulk.Projects[0].ItemsOmitted, maxItems+6, maxItems)
	}

	for _, tc := range []struct{ args, want string }{
		{`{"space_id":"sandbox","period":"fortnight"}`, "period must be one of"},
		{`{"space_id":"sandbox","from":"2026-07-01"}`, "needs both"},
		{`{"space_id":"sandbox","detail":"verbose"}`, "detail must be one of"},
		{`{"space_id":"sandbox","project_ids":["ghost"]}`, "not found"},
		{`{"space_id":"sandbox","week_start":"friday"}`, "week start must be"},
	} {
		if text, isErr := f.callTool(t, f.ro, "summarize_work", tc.args); !isErr || !strings.Contains(text, tc.want) {
			t.Errorf("%s: isErr=%v text=%s, want %q", tc.args, isErr, text, tc.want)
		}
	}
}

// East of Greenwich the error runs the other way: the same instant is already
// tomorrow. A fix that just subtracted an offset would pass the Los Angeles
// cases and fail here.
func TestDatesAheadOfUTCResolveForward(t *testing.T) {
	t.Parallel()
	f := newFixture(t, WithClock(lateClock), WithLocation(time.UTC))
	f.setTimezone(t, sydney)

	if text, isErr := f.callTool(t, f.rw, "log_activity",
		`{"space_id":"sandbox","project_id":"loom","title":"morning work"}`); isErr {
		t.Fatalf("log_activity: %s", text)
	}
	if got := firstActivityDate(t, f); got != sydneyDay {
		t.Errorf("date = %q, want %q — 09:00 in Sydney, not %s in UTC", got, sydneyDay, lateUTCDay)
	}
}

// A user who has never had a browser report a zone — an MCP-only account —
// falls back to the instance's, which is the whole point of the flag.
func TestInstanceZoneIsTheFallback(t *testing.T) {
	t.Parallel()
	la, err := time.LoadLocation(losAngeles)
	if err != nil {
		t.Fatalf("load %s: %v", losAngeles, err)
	}
	f := newFixture(t, WithClock(eveningClock), WithLocation(la))
	// No stored timezone on the user at all.

	if text, isErr := f.callTool(t, f.rw, "log_activity",
		`{"space_id":"sandbox","project_id":"loom","title":"evening work"}`); isErr {
		t.Fatalf("log_activity: %s", text)
	}
	if got := firstActivityDate(t, f); got != laDay {
		t.Errorf("date = %q, want the instance zone's %q", got, laDay)
	}
}

// A stored name this host cannot resolve must not fail the write. Someone
// logging work should not be turned away because a preference went bad; the
// instance zone is a defensible answer and the write is not.
func TestUnusableStoredZoneFallsBackWithoutFailing(t *testing.T) {
	t.Parallel()
	la, err := time.LoadLocation(losAngeles)
	if err != nil {
		t.Fatalf("load %s: %v", losAngeles, err)
	}
	f := newFixture(t, WithClock(eveningClock), WithLocation(la))
	// Stored directly, bypassing the API's validation — which is the only
	// way this state arises: a database moved to a host with thinner tzdata.
	f.setTimezone(t, "Mars/Olympus_Mons")

	text, isErr := f.callTool(t, f.rw, "log_activity",
		`{"space_id":"sandbox","project_id":"loom","title":"evening work"}`)
	if isErr {
		t.Fatalf("an unusable zone must not fail the write: %s", text)
	}
	if got := firstActivityDate(t, f); got != laDay {
		t.Errorf("date = %q, want the instance zone's %q", got, laDay)
	}
}

// A date the caller supplies is theirs, and must survive untouched — the zone
// only fills in what was not said.
func TestExplicitDateIsNotReinterpreted(t *testing.T) {
	t.Parallel()
	f := newFixture(t, WithClock(eveningClock), WithLocation(time.UTC))
	f.setTimezone(t, losAngeles)

	if text, isErr := f.callTool(t, f.rw, "log_activity",
		`{"space_id":"sandbox","project_id":"loom","title":"last week","date":"2026-07-14"}`); isErr {
		t.Fatalf("log_activity: %s", text)
	}
	if got := firstActivityDate(t, f); got != "2026-07-14" {
		t.Errorf("date = %q, want the caller's 2026-07-14 unchanged", got)
	}
}

// capturedAt is a naive local wall-clock, matching what the browser writes
// (web/src/lib/time.ts nowLocalISO). It has to be, because every reader takes
// its first ten characters as a calendar day: the inbox shows captures as
// "today"/"yesterday", and Review's staleness filter counts days from it. A
// UTC value with a Z in the same field reads as the wrong day all evening and
// sorts wrongly against a browser-written one.
func TestCaptureUsesLocalWallClock(t *testing.T) {
	t.Parallel()
	f := newFixture(t, WithClock(eveningClock), WithLocation(time.UTC))
	f.setTimezone(t, losAngeles)

	if text, isErr := f.callTool(t, f.rw, "capture_to_inbox",
		`{"space_id":"sandbox","text":"remember this"}`); isErr {
		t.Fatalf("capture_to_inbox: %s", text)
	}
	items, err := f.spaces.ListInboxItems(context.Background(), "sandbox")
	if err != nil {
		t.Fatalf("list inbox: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("inbox = %d, want 1", len(items))
	}
	// 03:30 UTC is 20:30 the previous day in Los Angeles, and no offset.
	if want := "2026-07-25T20:30:00"; items[0].CapturedAt != want {
		t.Errorf("capturedAt = %q, want the local wall clock %q", items[0].CapturedAt, want)
	}
	if strings.HasSuffix(items[0].CapturedAt, "Z") {
		t.Errorf("capturedAt = %q carries a zone; the field is naive local", items[0].CapturedAt)
	}
	// And the day a reader would take from it must be the user's day.
	if day := items[0].CapturedAt[:10]; day != laDay {
		t.Errorf("capturedAt day = %q, want %q", day, laDay)
	}
}
