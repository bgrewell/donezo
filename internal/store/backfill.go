package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// backfillCompletions is the Go half of space migration 0008. It gives tasks
// and projects that were finished before completed_at existed the best date
// the data can support, marked completed_source = 'inferred' so a summary can
// say so and the person can correct it.
//
// There was never a record of when a task was done, but checking one off has
// long offered to log an activity from its title (always, over MCP) — and
// that activity is written seconds after the check-off. So a done task is
// paired with a live, unplanned activity on the same project whose title
// matches (ignoring case and spacing) and that is dated no earlier than the
// task was created. Tasks are visited oldest first and each takes the earliest
// activity still unclaimed, so three "Weekly review" tasks pair with three
// different weekly-review activities, in order, rather than all landing on the
// first. A pairing also fills in the activity's task_id, the link a check-off
// now records directly.
//
// A task with no match keeps a NULL completed_at. Falling back to created_at
// or anything else would put it in some period confidently and wrongly; an
// honest "done, date unknown" is the better answer.
//
// A completed project takes the date of its latest milestone if it has one,
// otherwise its updated_at — weaker, which is what 'inferred' is for.
//
// Counts are recorded in meta under completionBackfillKey, so what the
// backfill did on a given database can be looked up afterwards.
func backfillCompletions(ctx context.Context, tx *sql.Tx, now string) error {
	matched, unmatched, err := backfillTaskCompletions(ctx, tx)
	if err != nil {
		return err
	}
	projects, err := backfillProjectCompletions(ctx, tx)
	if err != nil {
		return err
	}
	report, err := json.Marshal(map[string]any{
		"at":             now,
		"tasksMatched":   matched,
		"tasksUnmatched": unmatched,
		"projects":       projects,
	})
	if err != nil {
		return fmt.Errorf("backfill: report: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO meta (key, value) VALUES (?, ?)
		 ON CONFLICT (key) DO UPDATE SET value = excluded.value`,
		completionBackfillKey, string(report)); err != nil {
		return fmt.Errorf("backfill: record report: %w", err)
	}
	return nil
}

// completionBackfillKey is the meta key holding backfillCompletions' counts.
const completionBackfillKey = "completion_backfill"

// CompletionsRecordedSince returns when this space began recording
// completion times — the moment migration 0008 ran on it, an RFC 3339 UTC
// instant — or "" if that is unknown. Every task finished since then was
// stamped, so a done task with no completion date was finished before it;
// a summary uses this to keep such tasks out of later periods.
func (s *SpaceStore) CompletionsRecordedSince(ctx context.Context, spaceID string) (string, error) {
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return "", err
	}
	return completionsRecordedSince(ctx, db)
}

// completionsRecordedSince is CompletionsRecordedSince via q.
func completionsRecordedSince(ctx context.Context, q rowQuerier) (string, error) {
	var raw string
	err := q.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, completionBackfillKey).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("store: read %s: %w", completionBackfillKey, err)
	}
	var report struct {
		At string `json:"at"`
	}
	if err := json.Unmarshal([]byte(raw), &report); err != nil {
		return "", nil // unreadable is unknown, not an error worth failing a read over
	}
	return report.At, nil
}

// Completion sources: how a completed_at came to be.
const (
	CompletedRecorded = "recorded"
	CompletedInferred = "inferred"
	CompletedManual   = "manual"
)

// matchTitle normalizes a title for pairing: case-folded with runs of
// whitespace collapsed, so "Fix  login" and "fix login " pair but an edited
// title does not.
func matchTitle(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// inferredCompletedAt picks the instant to record for a task completed via
// an activity. The activity's created_at is the moment it was logged — which
// for a check-off is the moment of completion — unless the person backdated
// the activity, in which case the date they gave is the better claim and is
// recorded at noon UTC, which falls on that calendar date for nearly every
// time zone. A day either side counts as not backdated, because date is the
// person's local date and created_at is UTC.
func inferredCompletedAt(date, createdAt string) string {
	day, err := time.Parse("2006-01-02", date)
	if err != nil {
		return createdAt
	}
	noon := day.Add(12 * time.Hour).UTC().Format(time.RFC3339)
	created, err := time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return noon
	}
	created = created.UTC()
	createdDay := time.Date(created.Year(), created.Month(), created.Day(), 0, 0, 0, 0, time.UTC)
	gap := createdDay.Sub(day)
	if gap < 0 {
		gap = -gap
	}
	if gap > 24*time.Hour {
		return noon
	}
	return created.Format(time.RFC3339)
}

// backfillTaskCompletions pairs done tasks with the activities logged when
// they were checked off. Returns how many tasks were and were not paired.
func backfillTaskCompletions(ctx context.Context, tx *sql.Tx) (matched, unmatched int, err error) {
	type candidate struct {
		id, date, createdAt string
		used                bool
	}
	// Activities by project and normalized title, earliest first.
	groups := map[string][]*candidate{}
	rows, err := tx.QueryContext(ctx,
		`SELECT id, project_id, title, date, created_at FROM activities
		 WHERE deleted_at IS NULL AND COALESCE(planned, 0) = 0 AND task_id IS NULL
		 ORDER BY date, created_at, rowid`)
	if err != nil {
		return 0, 0, fmt.Errorf("backfill: list activities: %w", err)
	}
	for rows.Next() {
		var c candidate
		var projectID, title string
		if err := rows.Scan(&c.id, &projectID, &title, &c.date, &c.createdAt); err != nil {
			closeQuietly(rows)
			return 0, 0, fmt.Errorf("backfill: scan activity: %w", err)
		}
		key := projectID + "\x00" + matchTitle(title)
		groups[key] = append(groups[key], &c)
	}
	if err := rows.Err(); err != nil {
		closeQuietly(rows)
		return 0, 0, fmt.Errorf("backfill: list activities: %w", err)
	}
	closeQuietly(rows)

	type task struct{ id, projectID, title, createdAt string }
	var tasks []task
	// Trashed tasks are included: one restored later should come back with
	// its date. Unfiled tasks are not — they never had an activity to find.
	rows, err = tx.QueryContext(ctx,
		`SELECT id, project_id, title, created_at FROM tasks
		 WHERE status = 'done' AND completed_at IS NULL AND project_id IS NOT NULL
		 ORDER BY created_at, rowid`)
	if err != nil {
		return 0, 0, fmt.Errorf("backfill: list tasks: %w", err)
	}
	for rows.Next() {
		var t task
		if err := rows.Scan(&t.id, &t.projectID, &t.title, &t.createdAt); err != nil {
			closeQuietly(rows)
			return 0, 0, fmt.Errorf("backfill: scan task: %w", err)
		}
		tasks = append(tasks, t)
	}
	if err := rows.Err(); err != nil {
		closeQuietly(rows)
		return 0, 0, fmt.Errorf("backfill: list tasks: %w", err)
	}
	closeQuietly(rows)

	// Done tasks with no project are counted as unmatched too: they are done
	// and undated, which is what the count is for.
	var unfiled int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tasks WHERE status = 'done' AND completed_at IS NULL AND project_id IS NULL`,
	).Scan(&unfiled); err != nil {
		return 0, 0, fmt.Errorf("backfill: count unfiled: %w", err)
	}
	unmatched = unfiled

	for _, t := range tasks {
		var pick *candidate
		for _, c := range groups[t.projectID+"\x00"+matchTitle(t.title)] {
			// created_at and date are both yyyy-MM-dd, so they compare as
			// strings.
			if !c.used && c.date >= t.createdAt {
				pick = c
				break
			}
		}
		if pick == nil {
			unmatched++
			continue
		}
		pick.used = true
		if _, err := tx.ExecContext(ctx,
			`UPDATE tasks SET completed_at = ?, completed_source = ? WHERE id = ?`,
			inferredCompletedAt(pick.date, pick.createdAt), CompletedInferred, t.id); err != nil {
			return 0, 0, fmt.Errorf("backfill: task %q: %w", t.id, err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE activities SET task_id = ? WHERE id = ?`, t.id, pick.id); err != nil {
			return 0, 0, fmt.Errorf("backfill: activity %q: %w", pick.id, err)
		}
		matched++
	}
	return matched, unmatched, nil
}

// backfillProjectCompletions dates completed projects from their latest
// milestone, else their updated_at. Returns how many were dated.
func backfillProjectCompletions(ctx context.Context, tx *sql.Tx) (int, error) {
	res, err := tx.ExecContext(ctx,
		`UPDATE projects SET
		   completed_source = ?,
		   completed_at = COALESCE(
		     (SELECT MAX(a.date) || 'T12:00:00Z' FROM activities a
		      WHERE a.project_id = projects.id AND a.type = 'milestone'
		        AND a.deleted_at IS NULL AND COALESCE(a.planned, 0) = 0),
		     updated_at)
		 WHERE status = 'completed' AND completed_at IS NULL`,
		CompletedInferred)
	if err != nil {
		return 0, fmt.Errorf("backfill: projects: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("backfill: projects: %w", err)
	}
	return int(n), nil
}
