package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// This file is the store half of reminder delivery (#52): finding the
// reminders that still need sending, and recording what happened when they
// were. See migrations/space/0004.

// PendingReminder is a reminder that has not been delivered yet, with the
// bookkeeping the dispatcher needs to decide what to do with it.
type PendingReminder struct {
	// Reminder is the reminder itself.
	Reminder
	// Attempts is how many times delivery has already failed for it.
	Attempts int
}

// maxPendingRemindersPerPass bounds how many undelivered reminders one pass
// loads from a space. Deliberately larger than the dispatcher's per-user
// per-pass delivery cap, so it never starves normal delivery, while still
// keeping the read (and memory) bounded if an account accumulates a huge
// backlog — a reminder someone did not deal with is usually a handful, but a
// hostile or runaway account should not be able to make one pass load
// unbounded rows. The oldest-due are taken first, so nothing is stranded.
const maxPendingRemindersPerPass = 200

// PendingReminders returns live, unfinished, undelivered reminders in a
// space, oldest first, up to maxPendingRemindersPerPass.
//
// Deliberately unfiltered by time. RemindAt is a naive local wall clock (see
// the time model in docs/api.md), so which of these is actually due depends
// on the owner's timezone — a fact this layer does not have and should not
// guess at. The dispatcher resolves that.
func (s *SpaceStore) PendingReminders(ctx context.Context, spaceID string) ([]PendingReminder, error) {
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx,
		`SELECT id, text, details, remind_at, project_id, done, notify_attempts, repeat_every, repeat_unit
		 FROM reminders
		 WHERE deleted_at IS NULL
		   AND notified_at IS NULL
		   AND (done IS NULL OR done = 0)
		 ORDER BY remind_at, rowid
		 LIMIT ?`, maxPendingRemindersPerPass)
	if err != nil {
		return nil, fmt.Errorf("store: pending reminders: %w", err)
	}
	defer closeQuietly(rows)

	out := []PendingReminder{}
	for rows.Next() {
		var p PendingReminder
		var projectID sql.NullString
		var done sql.NullBool
		var repeatEvery sql.NullInt64
		var repeatUnit sql.NullString
		if err := rows.Scan(&p.ID, &p.Text, &p.Details, &p.RemindAt, &projectID, &done, &p.Attempts, &repeatEvery, &repeatUnit); err != nil {
			return nil, fmt.Errorf("store: pending reminders: %w", err)
		}
		if projectID.Valid {
			pid := projectID.String
			p.ProjectID = &pid
		}
		if done.Valid {
			d := done.Bool
			p.Done = &d
		}
		if repeatEvery.Valid && repeatUnit.Valid {
			p.Repeat = &ReminderRepeat{Every: int(repeatEvery.Int64), Unit: repeatUnit.String}
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: pending reminders: %w", err)
	}
	return out, nil
}

// MarkReminderNotified records a delivered reminder so it is not sent again.
//
// The guard on notified_at is what makes this safe to call from more than
// one pass: the update touches nothing if another pass got there first, and
// the second caller learns that by getting ErrNotFound rather than by
// silently double-counting a delivery.
func (s *SpaceStore) MarkReminderNotified(ctx context.Context, spaceID, id string) error {
	if id == "" {
		return errors.New("store: reminder id is required")
	}
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return err
	}
	res, err := db.ExecContext(ctx,
		`UPDATE reminders SET notified_at = ? WHERE id = ? AND notified_at IS NULL`,
		s.opts.now(), id)
	if err != nil {
		return fmt.Errorf("store: mark reminder notified: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: mark reminder notified: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("store: reminder %q: %w", id, ErrNotFound)
	}
	return nil
}

// RescheduleReminder re-arms a recurring reminder for its next occurrence: it
// moves remind_at forward and clears the delivery bookkeeping (notified_at and
// notify_attempts) so the dispatcher treats the next occurrence as fresh.
//
// The WHERE clause is the stop condition for the whole recurrence. It re-arms
// only a reminder that is still live and not done, so one marked done or
// trashed between the dispatch read and this write is not resurrected — it
// simply stops recurring, which is the only way a recurring reminder ends. A
// no-op reports ErrNotFound, which the dispatcher treats as "it stopped", not
// as an error.
func (s *SpaceStore) RescheduleReminder(ctx context.Context, spaceID, id, nextRemindAt string) error {
	if id == "" {
		return errors.New("store: reminder id is required")
	}
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return err
	}
	res, err := db.ExecContext(ctx,
		`UPDATE reminders SET remind_at = ?, notified_at = NULL, notify_attempts = 0
		 WHERE id = ? AND deleted_at IS NULL AND (done IS NULL OR done = 0)`,
		nextRemindAt, id)
	if err != nil {
		return fmt.Errorf("store: reschedule reminder: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: reschedule reminder: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("store: reminder %q: %w", id, ErrNotFound)
	}
	return nil
}

// RecordReminderFailure counts a failed delivery and returns the new total.
//
// Failures are counted rather than swallowed so that something permanently
// undeliverable stops being retried every minute forever. The reminder stays
// undelivered either way: the count is a circuit breaker, not a receipt.
func (s *SpaceStore) RecordReminderFailure(ctx context.Context, spaceID, id string) (int, error) {
	if id == "" {
		return 0, errors.New("store: reminder id is required")
	}
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return 0, err
	}
	var attempts int
	err = db.QueryRowContext(ctx,
		`UPDATE reminders SET notify_attempts = notify_attempts + 1
		 WHERE id = ? RETURNING notify_attempts`, id).Scan(&attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("store: reminder %q: %w", id, ErrNotFound)
	}
	if err != nil {
		return 0, fmt.Errorf("store: record reminder failure: %w", err)
	}
	return attempts, nil
}

// SkipReminderDelivery marks a reminder as needing no delivery, without
// having delivered it.
//
// Two things use it: a reminder too old to be worth sending after downtime,
// and one that has failed too many times to keep trying. Both are cases
// where the honest state is "donezo is done trying", and leaving
// notified_at NULL would mean reconsidering it every minute for ever.
func (s *SpaceStore) SkipReminderDelivery(ctx context.Context, spaceID, id string) error {
	return s.MarkReminderNotified(ctx, spaceID, id)
}
