package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bgrewell/donezo/internal/notify"
	"github.com/bgrewell/donezo/internal/store"
)

// recordingSender captures deliveries instead of making them.
type recordingSender struct {
	channel notify.Channel
	mu      sync.Mutex
	sent    []sentMessage
	err     error
}

type sentMessage struct {
	to  string
	msg notify.Message
}

func (r *recordingSender) Channel() notify.Channel { return r.channel }
func (r *recordingSender) Describe() string        { return string(r.channel) + " (test)" }
func (r *recordingSender) Send(_ context.Context, to string, msg notify.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	r.sent = append(r.sent, sentMessage{to: to, msg: msg})
	return nil
}

func (r *recordingSender) deliveries() []sentMessage {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]sentMessage(nil), r.sent...)
}

// TestReminderDueAt is the timezone contract, and it is the part of this
// feature most likely to be silently wrong: remindAt is a naive local wall
// clock, so reading it as UTC would fire a 2pm reminder at 7am for anyone
// west of Greenwich.
//
// No test here names the host's own zone — the fallback when the resolution
// is missing is time.Local, so a fixture in the developer's zone passes with
// the logic deleted.
func TestReminderDueAt(t *testing.T) {
	losAngeles, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}

	tests := []struct {
		name     string
		remindAt string
		loc      *time.Location
		want     time.Time
		wantErr  string
	}{
		{
			name:     "naive local resolves in the user's zone",
			remindAt: "2026-08-15T14:00:00",
			loc:      losAngeles,
			// 14:00 PDT is 21:00 UTC.
			want: time.Date(2026, 8, 15, 21, 0, 0, 0, time.UTC),
		},
		{
			name:     "the same wall clock is a different instant elsewhere",
			remindAt: "2026-08-15T14:00:00",
			loc:      tokyo,
			// 14:00 JST is 05:00 UTC.
			want: time.Date(2026, 8, 15, 5, 0, 0, 0, time.UTC),
		},
		{
			name:     "winter reads the standard offset, not the summer one",
			remindAt: "2026-01-15T14:00:00",
			loc:      losAngeles,
			// 14:00 PST is 22:00 UTC — an hour later than the August case,
			// which is the whole point of resolving in the zone.
			want: time.Date(2026, 1, 15, 22, 0, 0, 0, time.UTC),
		},
		{
			name:     "an explicit offset is honoured",
			remindAt: "2026-08-15T14:00:00+09:00",
			loc:      losAngeles,
			want:     time.Date(2026, 8, 15, 5, 0, 0, 0, time.UTC),
		},
		{
			name:     "Z is an instant",
			remindAt: "2026-08-15T14:00:00Z",
			loc:      losAngeles,
			want:     time.Date(2026, 8, 15, 14, 0, 0, 0, time.UTC),
		},
		{
			name:     "minute precision is accepted",
			remindAt: "2026-08-15T14:00",
			loc:      tokyo,
			want:     time.Date(2026, 8, 15, 5, 0, 0, 0, time.UTC),
		},
		{name: "empty", remindAt: "", loc: tokyo, wantErr: "no time"},
		{name: "nonsense", remindAt: "saturday afternoon", loc: tokyo, wantErr: "not an ISO datetime"},
		{name: "date only", remindAt: "2026-08-15", loc: tokyo, wantErr: "not an ISO datetime"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := reminderDueAt(tt.remindAt, tt.loc)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("reminderDueAt(%q) = %v, %v; want error containing %q", tt.remindAt, got, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("reminderDueAt(%q): %v", tt.remindAt, err)
			}
			if !got.Equal(tt.want) {
				t.Fatalf("reminderDueAt(%q) in %s = %s, want %s",
					tt.remindAt, tt.loc, got.UTC(), tt.want)
			}
		})
	}
}

// TestReminderDueAtAcrossDST pins the behaviour a fixed offset would get
// wrong: the same wall-clock string on either side of a transition is two
// different instants, and "2pm" means 2pm on both days.
func TestReminderDueAtAcrossDST(t *testing.T) {
	// Sydney, so the transition runs the opposite way to the developer's
	// zone and to UTC.
	sydney, err := time.LoadLocation("Australia/Sydney")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	before, err := reminderDueAt("2026-04-04T14:00:00", sydney) // AEDT
	if err != nil {
		t.Fatalf("before: %v", err)
	}
	after, err := reminderDueAt("2026-04-05T14:00:00", sydney) // AEST
	if err != nil {
		t.Fatalf("after: %v", err)
	}
	gap := after.Sub(before)
	if gap != 25*time.Hour {
		t.Fatalf("2pm to 2pm across the DST end = %v, want 25h — the offset was applied as a constant", gap)
	}
}

// dispatchFixture is a server with one space, one verified contact, and a
// sender that records instead of sending.
type dispatchFixture struct {
	server  *Server
	email   *recordingSender
	sms     *recordingSender
	spaceID string
	userID  int64
	now     time.Time
	loc     *time.Location
}

// newDispatchFixture builds the fixture. The user's timezone is Tokyo,
// chosen because it differs from UTC and from this host.
func newDispatchFixture(t *testing.T) *dispatchFixture {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	f := &dispatchFixture{
		email: &recordingSender{channel: notify.ChannelEmail},
		sms:   &recordingSender{channel: notify.ChannelSMS},
		// 2026-08-15 15:00 JST == 06:00 UTC.
		now: time.Date(2026, 8, 15, 6, 0, 0, 0, time.UTC),
		loc: loc,
	}
	f.server = newTestServer(t,
		WithNotifiers(notify.NewRegistry(f.email, f.sms)),
		WithClock(func() time.Time { return f.now }),
	)

	ctx := context.Background()
	user, err := f.server.core.GetUserByUsername(ctx, "ben")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	f.userID = user.ID
	if _, err := f.server.core.PatchUserSettings(ctx, user.ID, func(s *store.UserSettings) error {
		s.Timezone = "Asia/Tokyo"
		return nil
	}); err != nil {
		t.Fatalf("set timezone: %v", err)
	}
	// newTestServer's own space, renamed here only so the assertion on the
	// message body has something recognisable to look for.
	f.spaceID = "sandbox"
	f.addContact(t, notify.ChannelEmail, "ben@example.com", true)
	return f
}

// addContact adds a destination, verified or not.
func (f *dispatchFixture) addContact(t *testing.T, channel notify.Channel, address string, verified bool) store.UserContact {
	t.Helper()
	ctx := context.Background()
	id := fmt.Sprintf("ctc-%s-%s", channel, address)
	c, err := f.server.core.CreateUserContact(ctx, store.UserContact{
		ID: id, UserID: f.userID, Channel: string(channel), Address: address,
	})
	if err != nil {
		t.Fatalf("create contact: %v", err)
	}
	if verified {
		if _, err := f.server.core.StartContactChallenge(ctx, f.userID, c.ID, "hash"); err != nil {
			t.Fatalf("start challenge: %v", err)
		}
		if _, err := f.server.core.VerifyUserContact(ctx, f.userID, c.ID, "hash"); err != nil {
			t.Fatalf("verify contact: %v", err)
		}
	}
	return c
}

// addReminder puts a reminder in the space at a naive local time.
func (f *dispatchFixture) addReminder(t *testing.T, id, text, remindAt string) {
	t.Helper()
	if _, err := f.server.spaces.CreateReminder(context.Background(), f.spaceID, store.Reminder{
		ID: id, Text: text, RemindAt: remindAt,
	}); err != nil {
		t.Fatalf("create reminder: %v", err)
	}
}

// pendingIDs lists the reminders still awaiting delivery.
func (f *dispatchFixture) pendingIDs(t *testing.T) []string {
	t.Helper()
	pending, err := f.server.spaces.PendingReminders(context.Background(), f.spaceID)
	if err != nil {
		t.Fatalf("pending reminders: %v", err)
	}
	ids := make([]string, 0, len(pending))
	for _, p := range pending {
		ids = append(ids, p.ID)
	}
	return ids
}

func TestDispatchDeliversDueReminder(t *testing.T) {
	f := newDispatchFixture(t)
	// 14:00 JST, an hour before the fixture's now.
	f.addReminder(t, "rem-1", "Clean up the deck", "2026-08-15T14:00:00")

	f.server.dispatchReminders(context.Background())

	sent := f.email.deliveries()
	if len(sent) != 1 {
		t.Fatalf("delivered %d messages, want 1", len(sent))
	}
	if sent[0].to != "ben@example.com" {
		t.Fatalf("delivered to %q", sent[0].to)
	}
	if !strings.Contains(sent[0].msg.Subject, "Clean up the deck") {
		t.Fatalf("subject = %q, want the reminder text", sent[0].msg.Subject)
	}
	if len(f.pendingIDs(t)) != 0 {
		t.Fatalf("reminder still pending after delivery")
	}
}

// The bug this guards is the one that makes the feature useless: reading the
// naive local time as UTC. In Tokyo that fires nine hours early, so a
// reminder set for later today would go out now.
func TestDispatchHoldsReminderNotYetDueInTheUsersZone(t *testing.T) {
	f := newDispatchFixture(t)
	// 22:00 JST is 13:00 UTC — still ahead of the fixture's 06:00 UTC now,
	// but BEHIND it if the string were read as UTC.
	f.addReminder(t, "rem-1", "Take the bins out", "2026-08-15T22:00:00")

	f.server.dispatchReminders(context.Background())

	if sent := f.email.deliveries(); len(sent) != 0 {
		t.Fatalf("delivered %d messages for a reminder that is not due yet: %+v", len(sent), sent)
	}
	if got := f.pendingIDs(t); len(got) != 1 {
		t.Fatalf("pending = %v, want the reminder still waiting", got)
	}
}

func TestDispatchSkipsRemindersTooLateToSend(t *testing.T) {
	f := newDispatchFixture(t)
	// Three days ago in the user's zone, well past the 24h bound.
	f.addReminder(t, "rem-old", "Water the plants", "2026-08-12T09:00:00")

	f.server.dispatchReminders(context.Background())

	if sent := f.email.deliveries(); len(sent) != 0 {
		t.Fatalf("delivered a reminder from three days ago: %+v", sent)
	}
	// Marked, so it is not reconsidered every minute forever.
	if got := f.pendingIDs(t); len(got) != 0 {
		t.Fatalf("pending = %v, want the stale reminder marked as handled", got)
	}
}

func TestDispatchDeliversWithinTheLatenessBound(t *testing.T) {
	f := newDispatchFixture(t)
	// Two hours late: the server was down, and this is still worth having.
	f.addReminder(t, "rem-1", "Call the plumber", "2026-08-15T13:00:00")

	f.server.dispatchReminders(context.Background())

	if len(f.email.deliveries()) != 1 {
		t.Fatalf("a two-hour-late reminder was not delivered")
	}
}

func TestDispatchDeliversOnlyOnce(t *testing.T) {
	f := newDispatchFixture(t)
	f.addReminder(t, "rem-1", "Clean up the deck", "2026-08-15T14:00:00")

	f.server.dispatchReminders(context.Background())
	f.server.dispatchReminders(context.Background())
	f.server.dispatchReminders(context.Background())

	if n := len(f.email.deliveries()); n != 1 {
		t.Fatalf("delivered %d times, want exactly 1 — a reminder that repeats every minute is worse than none", n)
	}
}

func TestDispatchIgnoresUnverifiedContacts(t *testing.T) {
	f := newDispatchFixture(t)
	// An unverified number. Sending here would let anyone signed in point
	// this instance at a stranger's phone.
	f.addContact(t, notify.ChannelSMS, "+15551234567", false)
	f.addReminder(t, "rem-1", "Clean up the deck", "2026-08-15T14:00:00")

	f.server.dispatchReminders(context.Background())

	if sent := f.sms.deliveries(); len(sent) != 0 {
		t.Fatalf("delivered to an unverified destination: %+v", sent)
	}
	if len(f.email.deliveries()) != 1 {
		t.Fatalf("the verified destination should still have been delivered to")
	}
}

func TestDispatchDeliversToEveryVerifiedChannel(t *testing.T) {
	f := newDispatchFixture(t)
	f.addContact(t, notify.ChannelSMS, "+15551234567", true)
	f.addReminder(t, "rem-1", "Clean up the deck", "2026-08-15T14:00:00")

	f.server.dispatchReminders(context.Background())

	if len(f.email.deliveries()) != 1 {
		t.Fatalf("email not delivered")
	}
	if len(f.sms.deliveries()) != 1 {
		t.Fatalf("sms not delivered")
	}
}

func TestDispatchRetriesAfterFailureThenGivesUp(t *testing.T) {
	f := newDispatchFixture(t)
	f.email.err = errors.New("relay refused")
	f.addReminder(t, "rem-1", "Clean up the deck", "2026-08-15T14:00:00")

	// Failing passes leave it pending, so a relay that comes back delivers.
	f.server.dispatchReminders(context.Background())
	if got := f.pendingIDs(t); len(got) != 1 {
		t.Fatalf("pending = %v, want the reminder retained after a failure", got)
	}

	for i := 0; i < maxDeliveryAttempts; i++ {
		f.server.dispatchReminders(context.Background())
	}
	if got := f.pendingIDs(t); len(got) != 0 {
		t.Fatalf("pending = %v, want donezo to stop retrying a permanently failing destination", got)
	}
}

func TestDispatchDoesNotConsumeRemindersWhenThereIsNowhereToSend(t *testing.T) {
	f := newDispatchFixture(t)
	if err := f.server.core.DeleteUserContact(context.Background(), f.userID, "ctc-email-ben@example.com"); err != nil {
		t.Fatalf("delete contact: %v", err)
	}
	f.addReminder(t, "rem-1", "Clean up the deck", "2026-08-15T14:00:00")

	f.server.dispatchReminders(context.Background())

	// Marking it here would mean that adding a destination later silently
	// loses the reminders from before.
	if got := f.pendingIDs(t); len(got) != 1 {
		t.Fatalf("pending = %v, want the reminder left alone when nothing is deliverable", got)
	}
}

func TestDispatchSkipsDoneReminders(t *testing.T) {
	f := newDispatchFixture(t)
	done := true
	if _, err := f.server.spaces.CreateReminder(context.Background(), f.spaceID, store.Reminder{
		ID: "rem-done", Text: "Already handled", RemindAt: "2026-08-15T14:00:00", Done: &done,
	}); err != nil {
		t.Fatalf("create reminder: %v", err)
	}

	f.server.dispatchReminders(context.Background())

	if sent := f.email.deliveries(); len(sent) != 0 {
		t.Fatalf("delivered a reminder that was already ticked off: %+v", sent)
	}
}

func TestDispatchSkipsArchivedSpaces(t *testing.T) {
	f := newDispatchFixture(t)
	f.addReminder(t, "rem-1", "Clean up the deck", "2026-08-15T14:00:00")
	if _, err := f.server.core.SetSpaceArchived(context.Background(), f.spaceID, true); err != nil {
		t.Fatalf("archive space: %v", err)
	}

	f.server.dispatchReminders(context.Background())

	if sent := f.email.deliveries(); len(sent) != 0 {
		t.Fatalf("delivered from an archived space: %+v", sent)
	}
}

func TestDispatchIncludesDetailsAndSpace(t *testing.T) {
	f := newDispatchFixture(t)
	if _, err := f.server.spaces.CreateReminder(context.Background(), f.spaceID, store.Reminder{
		ID: "rem-1", Text: "Clean up the deck", Details: "Bins curbside for pickup",
		RemindAt: "2026-08-15T14:00:00",
	}); err != nil {
		t.Fatalf("create reminder: %v", err)
	}
	f.server.publicURL = "https://donezo.example.com"

	f.server.dispatchReminders(context.Background())

	sent := f.email.deliveries()
	if len(sent) != 1 {
		t.Fatalf("delivered %d messages", len(sent))
	}
	body := sent[0].msg.Body
	for _, want := range []string{"Bins curbside for pickup", "https://donezo.example.com", "Sandbox"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body = %q, missing %q", body, want)
		}
	}
}

func TestRunReminderDispatchReturnsWhenNothingIsConfigured(t *testing.T) {
	s := newTestServer(t)

	done := make(chan struct{})
	go func() {
		defer close(done)
		// Background, not a cancelled context: the point is that it returns
		// on its own rather than sitting in a ticker loop forever.
		s.RunReminderDispatch(context.Background())
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunReminderDispatch did not return with no channels configured")
	}
}

// Finding #4: one pass delivers at most maxDeliveriesPerUserPass reminders
// for a user; the rest are deferred, not dropped. Without the cap, a member
// posting a large batch dated "now" would fire them all at once (billed
// sends, and the single dispatch goroutine blocked for everyone else).
func TestDispatchCapsDeliveriesPerUserPerPass(t *testing.T) {
	f := newDispatchFixture(t)
	// More due reminders than the per-pass cap, all due an hour ago.
	total := maxDeliveriesPerUserPass + 5
	for i := 0; i < total; i++ {
		f.addReminder(t, fmt.Sprintf("rem-%02d", i), fmt.Sprintf("Reminder %d", i), "2026-08-15T14:00:00")
	}

	f.server.dispatchReminders(context.Background())
	if got := len(f.email.deliveries()); got != maxDeliveriesPerUserPass {
		t.Fatalf("first pass delivered %d, want the cap %d", got, maxDeliveriesPerUserPass)
	}
	if got := len(f.pendingIDs(t)); got != total-maxDeliveriesPerUserPass {
		t.Fatalf("after first pass %d still pending, want %d (overflow deferred, not dropped)",
			got, total-maxDeliveriesPerUserPass)
	}

	// The next pass drains the rest.
	f.server.dispatchReminders(context.Background())
	if got := len(f.email.deliveries()); got != total {
		t.Fatalf("after second pass delivered %d, want all %d", got, total)
	}
	if got := len(f.pendingIDs(t)); got != 0 {
		t.Fatalf("after second pass %d still pending, want 0", got)
	}
}
