package mcp

import (
	"context"
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

func firstNoteCreatedAt(t *testing.T, f *fixture) string {
	t.Helper()
	notes, err := f.spaces.ListNotes(context.Background(), "sandbox")
	if err != nil {
		t.Fatalf("list notes: %v", err)
	}
	if len(notes) != 1 {
		t.Fatalf("notes = %d, want exactly 1", len(notes))
	}
	return notes[0].CreatedAt
}

// datedTools is every tool that puts a date on something without being told
// one. Each has to resolve it in the caller's zone; a new one that forgets is
// exactly the bug this file exists for, so add it here when you add it there.
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
