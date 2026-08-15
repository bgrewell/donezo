package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bgrewell/donezo/internal/notify"
	"github.com/bgrewell/donezo/internal/store"
)

// Reminder delivery (#52). A reminder that only exists in the app arrives
// when you are already looking at the app, which is the one moment you did
// not need it. This is the loop that takes it somewhere else.
//
// The shape follows RunTrashSweep deliberately: a blocking loop the caller
// owns, sweeping every live space, where one space's failure is logged and
// the pass continues. See RunTrashSweep for why that discipline matters
// around store Close.

// dispatchInterval is how often due reminders are looked for.
//
// A minute, because a reminder set for 14:00 that arrives at 14:04 is a
// worse reminder, and the pass is cheap: one small query per space over
// rows a person has not dealt with yet.
const dispatchInterval = time.Minute

// maxDeliveryAttempts is how many failed passes a reminder survives before
// donezo stops trying.
//
// Five spreads over five minutes, which covers a relay restart, and stops
// well short of a permanently bad destination being retried forever. The
// reminder is still in the app; what is given up on is delivering it.
const maxDeliveryAttempts = 5

// maxDeliveriesPerUserPass bounds how many reminders one account has
// delivered in a single dispatch pass. It caps the billed sends a member can
// trigger per minute and stops one account's backlog from starving everyone
// else's reminders in the single dispatch goroutine. Generous for real use —
// few people have twenty reminders due in the same minute — and the overflow
// is not dropped, only deferred to the next pass.
const maxDeliveriesPerUserPass = 20

// DefaultReminderMaxLateness is how overdue a reminder may be and still be
// delivered. It mirrors config.DefaultReminderMaxLatenessHours, which is the
// CLI's default for the same value; this is the one that applies when a
// Server is built without the option, as the tests do.
const DefaultReminderMaxLateness = 24 * time.Hour

// RunReminderDispatch delivers due reminders now and then every minute,
// returning when ctx is done. It blocks, so the caller owns the goroutine.
//
// With nothing configured it returns immediately: an instance with no
// channels is a supported instance, and waking every minute to rediscover
// that would be noise in the log and nothing else.
func (s *Server) RunReminderDispatch(ctx context.Context) {
	if !s.notifiers.Any() {
		s.logger.Printf("reminder delivery is off; no email or SMS provider is configured")
		return
	}
	var configured []string
	for _, c := range notify.Channels {
		if sender, ok := s.notifiers.Sender(c); ok {
			configured = append(configured, fmt.Sprintf("%s via %s", c, sender.Describe()))
		}
	}
	s.logger.Printf("reminder delivery is on: %s", strings.Join(configured, "; "))

	// A pass at startup, for the same reason the trash sweep does one: an
	// instance restarted around the time a reminder was due would otherwise
	// wait a full interval, and one restarted every day at the same time
	// could miss the same reminder every day.
	s.dispatchReminders(ctx)
	ticker := time.NewTicker(dispatchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.dispatchReminders(ctx)
		}
	}
}

// dispatchReminders makes one pass over every live space.
func (s *Server) dispatchReminders(ctx context.Context) {
	spaces, err := s.core.ListSpaces(ctx)
	if err != nil {
		s.logger.Printf("reminder dispatch: listing spaces: %v", err)
		return
	}
	// One user usually owns several spaces; their contacts and timezone are
	// the same for all of them, so they are resolved once per pass.
	recipients := make(map[int64]*recipient)
	// Deliveries per user this pass. The pass sends synchronously in one
	// goroutine, so an account with a large backlog of due reminders — or one
	// deliberately created to spend the operator's SMS budget — would
	// otherwise fire them all at once and block every other account's
	// reminders behind them. Each user gets at most maxDeliveriesPerUserPass
	// this minute; the rest stay pending and go out on later passes.
	sentThisPass := make(map[int64]int)
	for _, sp := range spaces {
		if sp.ArchivedAt != nil {
			// An archived space is read-only, and delivering would mean
			// writing notified_at back into it. Its reminders wait, which is
			// consistent with everything else being frozen.
			continue
		}
		rcpt, ok := recipients[sp.UserID]
		if !ok {
			rcpt = s.resolveRecipient(ctx, sp.UserID)
			recipients[sp.UserID] = rcpt
		}
		if len(rcpt.contacts) == 0 {
			// Nowhere to send. Deliberately no marking: when they do add a
			// destination, reminders from the last day still arrive, and
			// older ones are dropped by the lateness bound rather than by
			// having been quietly consumed while there was no way to send.
			continue
		}
		if err := s.dispatchSpace(ctx, sp, rcpt, sentThisPass); err != nil {
			s.logger.Printf("reminder dispatch: space %s: %v", sp.ID, err)
		}
	}
}

// recipient is one user's delivery context for a pass.
type recipient struct {
	// loc is the zone their naive-local reminder times are read in.
	loc *time.Location
	// contacts are their verified destinations.
	contacts []store.UserContact
}

// resolveRecipient collects one user's timezone and verified destinations.
//
// Every failure degrades to "cannot deliver for this user" rather than
// stopping the pass: one unreadable settings row must not hold up everybody
// else's reminders.
func (s *Server) resolveRecipient(ctx context.Context, userID int64) *recipient {
	r := &recipient{loc: s.location}
	settings, err := s.core.GetUserSettings(ctx, userID)
	switch {
	case err != nil:
		s.logger.Printf("reminder dispatch: reading settings for user %d: %v", userID, err)
	case settings.Timezone != "":
		if loc, err := time.LoadLocation(settings.Timezone); err == nil {
			r.loc = loc
		} else {
			s.logger.Printf("reminder dispatch: stored timezone %q for user %d is unusable: %v",
				settings.Timezone, userID, err)
		}
	}
	contacts, err := s.core.ListVerifiedContacts(ctx, userID)
	if err != nil {
		s.logger.Printf("reminder dispatch: reading contacts for user %d: %v", userID, err)
		return r
	}
	// A destination on a channel this instance cannot send on is not an
	// error — someone configured SMS, the operator has not — but it must not
	// count as somewhere to send, or a reminder would be marked delivered
	// having reached nobody.
	for _, c := range contacts {
		if s.notifiers.Configured(notify.Channel(c.Channel)) {
			r.contacts = append(r.contacts, c)
		}
	}
	return r
}

// dispatchSpace delivers whatever is due in one space, up to the owner's
// remaining per-pass budget in sentThisPass.
func (s *Server) dispatchSpace(ctx context.Context, sp store.Space, rcpt *recipient, sentThisPass map[int64]int) error {
	pending, err := s.spaces.PendingReminders(ctx, sp.ID)
	if err != nil {
		return err
	}
	now := s.clock()
	for _, p := range pending {
		due, err := reminderDueAt(p.RemindAt, rcpt.loc)
		if err != nil {
			// An unparseable time cannot become due, so retrying it every
			// minute forever is the one outcome guaranteed to be useless.
			s.logger.Printf("reminder dispatch: reminder %s in %s has an unreadable time %q: %v",
				p.ID, sp.ID, p.RemindAt, err)
			s.skipDelivery(ctx, sp.ID, p.ID)
			continue
		}
		if due.After(now) {
			continue
		}
		if s.reminderMaxLateness > 0 && now.Sub(due) > s.reminderMaxLateness {
			// Downtime. Sending a week of missed reminders in one burst, at
			// whatever hour the server came back, is worse than not sending
			// them: they arrive with no context and bury anything current.
			s.logger.Printf("reminder dispatch: reminder %s in %s was due %s ago; too late to send",
				p.ID, sp.ID, now.Sub(due).Round(time.Minute))
			// A recurring reminder is not finished by a missed occurrence: skip
			// the stale one but re-arm for its next future slot. A one-shot is
			// simply retired.
			if p.Repeat != nil {
				s.rescheduleRecurring(ctx, sp, p, due, now, rcpt.loc)
			} else {
				s.skipDelivery(ctx, sp.ID, p.ID)
			}
			continue
		}
		// This one would be delivered. Stop once the owner has had their
		// share this pass; the rest stay pending (notified_at untouched) and
		// go out on a later pass, so a big backlog cannot monopolise the
		// dispatch loop or the send budget.
		if sentThisPass[sp.UserID] >= maxDeliveriesPerUserPass {
			s.logger.Printf("reminder dispatch: user %d hit the per-pass delivery cap (%d); %d+ reminders wait for the next pass",
				sp.UserID, maxDeliveriesPerUserPass, 1)
			break
		}
		sentThisPass[sp.UserID]++
		s.deliver(ctx, sp, p, due, rcpt)
	}
	return nil
}

// deliver sends one reminder to every verified destination.
//
// It is marked delivered when at least one channel accepted it. Marking on
// the first success rather than on all of them is the deliberate choice:
// the alternative is that a failing SMS provider makes the email arrive
// again every minute.
//
// due is the occurrence that just fired; a recurring reminder is re-armed from
// it rather than retired.
func (s *Server) deliver(ctx context.Context, sp store.Space, p store.PendingReminder, due time.Time, rcpt *recipient) {
	msg := s.composeReminder(sp, p)
	var delivered bool
	for _, c := range rcpt.contacts {
		sendCtx, cancel := context.WithTimeout(ctx, notify.DefaultTimeout)
		err := s.notifiers.Send(sendCtx, notify.Channel(c.Channel), c.Address, msg)
		cancel()
		if err != nil {
			s.logger.Printf("reminder dispatch: reminder %s to %s %s: %v",
				p.ID, c.Channel, notify.Redact(notify.Channel(c.Channel), c.Address), err)
			continue
		}
		delivered = true
		s.logger.Printf("reminder dispatch: reminder %s delivered to %s %s",
			p.ID, c.Channel, notify.Redact(notify.Channel(c.Channel), c.Address))
	}

	if delivered {
		s.settleDelivered(ctx, sp, p, due, rcpt.loc)
		return
	}

	attempts, err := s.spaces.RecordReminderFailure(ctx, sp.ID, p.ID)
	if err != nil {
		s.logger.Printf("reminder dispatch: recording failure for reminder %s: %v", p.ID, err)
		return
	}
	if attempts >= maxDeliveryAttempts {
		// A recurring reminder is not abandoned over one bad stretch: skip this
		// occurrence but re-arm for the next, so a transient outage does not end
		// the series. A one-shot is retired.
		if p.Repeat != nil {
			s.logger.Printf("reminder dispatch: recurring reminder %s undeliverable after %d attempts; re-arming for its next occurrence",
				p.ID, attempts)
			s.rescheduleRecurring(ctx, sp, p, due, s.clock(), rcpt.loc)
			return
		}
		s.logger.Printf("reminder dispatch: giving up on reminder %s after %d attempts; it stays in the app",
			p.ID, attempts)
		s.skipDelivery(ctx, sp.ID, p.ID)
	}
}

// settleDelivered records a successful send. A one-shot reminder is marked
// notified so it is never sent again; a recurring one is re-armed for its next
// occurrence instead, which is what keeps it coming back until it is done.
func (s *Server) settleDelivered(ctx context.Context, sp store.Space, p store.PendingReminder, due time.Time, loc *time.Location) {
	if p.Repeat != nil {
		s.rescheduleRecurring(ctx, sp, p, due, s.clock(), loc)
		return
	}
	if err := s.spaces.MarkReminderNotified(ctx, sp.ID, p.ID); err != nil {
		// Delivered but not marked: it will be sent again next pass. Loud,
		// because a duplicate reminder every minute is the kind of thing
		// someone silences by turning the whole feature off.
		s.logger.Printf("reminder dispatch: reminder %s was delivered but could not be marked: %v", p.ID, err)
	}
}

// rescheduleRecurring re-arms a recurring reminder for its next occurrence
// after now. A reminder marked done or trashed in the meantime stops instead:
// RescheduleReminder reports ErrNotFound, which is the intended end of the
// series, not a failure.
func (s *Server) rescheduleRecurring(ctx context.Context, sp store.Space, p store.PendingReminder, due, now time.Time, loc *time.Location) {
	if p.Repeat == nil {
		return
	}
	next := formatReminderTime(nextReminderOccurrence(due, *p.Repeat, now, loc))
	err := s.spaces.RescheduleReminder(ctx, sp.ID, p.ID, next)
	switch {
	case errors.Is(err, store.ErrNotFound):
		// Done or trashed between the read and now — it has stopped recurring.
	case err != nil:
		s.logger.Printf("reminder dispatch: re-arming recurring reminder %s: %v", p.ID, err)
	default:
		s.logger.Printf("reminder dispatch: recurring reminder %s re-armed for %s", p.ID, next)
	}
}

// skipDelivery marks a reminder as no longer worth delivering.
func (s *Server) skipDelivery(ctx context.Context, spaceID, id string) {
	if err := s.spaces.SkipReminderDelivery(ctx, spaceID, id); err != nil {
		s.logger.Printf("reminder dispatch: marking reminder %s undeliverable: %v", id, err)
	}
}

// composeReminder turns a reminder into a message.
//
// The reminder's own text is the subject because it is the thing that has to
// survive a lock-screen preview or an SMS: everything else is context.
func (s *Server) composeReminder(sp store.Space, p store.PendingReminder) notify.Message {
	msg := notify.Message{Subject: strings.TrimSpace(p.Text)}
	if msg.Subject == "" {
		msg.Subject = "Reminder"
	}
	var body strings.Builder
	if details := strings.TrimSpace(p.Details); details != "" {
		body.WriteString(details)
		body.WriteString("\n\n")
	}
	if s.publicURL != "" {
		// Straight to the space, which is as precise as the app's routing
		// gets for a reminder — they live in the top bar, not on a page of
		// their own.
		fmt.Fprintf(&body, "%s/#/focus\n", strings.TrimSuffix(s.publicURL, "/"))
	}
	fmt.Fprintf(&body, "— donezo · %s", sp.Name)
	msg.Body = body.String()
	return msg
}

// maxRepeatCatchUp bounds how many interval steps nextReminderOccurrence will
// take to land past now. It is only reached after a long outage relative to a
// short interval (hourly reminders unsent for years), and its sole purpose is
// to stop a degenerate interval from looping forever; the value is far above
// any real gap.
const maxRepeatCatchUp = 200_000

// addRepeat advances t by one recurrence step. Hours add a fixed duration;
// days and weeks advance the wall clock via AddDate, so "every day at 2pm"
// stays 2pm across a daylight-saving change rather than drifting an hour.
func addRepeat(t time.Time, r store.ReminderRepeat) time.Time {
	switch r.Unit {
	case "hour":
		return t.Add(time.Duration(r.Every) * time.Hour)
	case "day":
		return t.AddDate(0, 0, r.Every)
	case "week":
		return t.AddDate(0, 0, 7*r.Every)
	default:
		// Unreachable: the unit is validated on the way in. Advancing a day is
		// a safe non-zero fallback that cannot wedge the catch-up loop.
		return t.AddDate(0, 0, 1)
	}
}

// nextReminderOccurrence is the first scheduled occurrence strictly after now.
//
// It keeps the reminder on its calendar slot rather than nudging from whenever
// it happened to be delivered: a weekly Saturday-2pm reminder re-arms for next
// Saturday 2pm, and one missed during downtime skips straight to the next
// future slot rather than firing a burst to catch up. The interval is applied
// in loc so day and week steps track the recipient's wall clock.
func nextReminderOccurrence(due time.Time, r store.ReminderRepeat, now time.Time, loc *time.Location) time.Time {
	if loc == nil {
		loc = time.Local
	}
	next := due.In(loc)
	for i := 0; i < maxRepeatCatchUp && !next.After(now); i++ {
		next = addRepeat(next, r)
	}
	if !next.After(now) {
		// The bound was hit (an implausibly large gap). Land one interval past
		// now so the series continues without another marathon of steps.
		next = addRepeat(now.In(loc), r)
	}
	return next
}

// reminderTimeLayout is the naive-local wall-clock format reminder times are
// stored in. formatReminderTime writes a re-armed occurrence back in it.
const reminderTimeLayout = "2006-01-02T15:04:05"

// formatReminderTime renders a re-armed occurrence as the naive-local string
// the store holds and reminderDueAt reads back. t is expected to already be in
// the recipient's location.
func formatReminderTime(t time.Time) string {
	return t.Format(reminderTimeLayout)
}

// reminderDueAt resolves a stored reminder time to the instant it fires.
//
// This is the whole subtlety of the feature. remindAt is a naive local wall
// clock — "2026-08-15T14:00:00" means two in the afternoon where the person
// is, and carries no offset to say where that is. Reading it as UTC would
// text somebody in California at seven in the morning for a two o'clock
// reminder, and the error would look like a scheduling bug rather than a
// timezone one.
//
// ParseInLocation is what makes it right, including across a DST boundary:
// the same wall-clock string on either side of a transition resolves to
// different instants, which is exactly what "2pm on Saturday" means.
//
// An explicit offset is honoured when one is present, because a client that
// sent a real instant meant a real instant.
func reminderDueAt(remindAt string, loc *time.Location) (time.Time, error) {
	if loc == nil {
		loc = time.Local
	}
	value := strings.TrimSpace(remindAt)
	if value == "" {
		return time.Time{}, errors.New("reminder has no time")
	}
	// Offset-bearing first: RFC 3339 also parses through ParseInLocation,
	// which would then ignore the very offset that makes it unambiguous.
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t, nil
	}
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04"} {
		if t, err := time.ParseInLocation(layout, value, loc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("%q is not an ISO datetime", value)
}
