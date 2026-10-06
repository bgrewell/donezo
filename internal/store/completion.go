package store

import (
	"context"
	"fmt"
	"time"
)

// The rules for completed_at live here, applied by the store on every write
// path that can change a status, so the web UI, MCP and conversions all get
// them without having to remember.

// stampTaskCompletion keeps a task's completion fields consistent with its
// status. wasDone is the status before this write (false for a new task).
//
//   - Becoming done with no completed_at given: stamped now, as recorded.
//   - Not done: both cleared, so reopening forgets the old date and finishing
//     again records the new one.
//   - Staying done: left exactly as the write has it. That includes no
//     instant, which is how a person says "I don't know when" after clearing
//     a wrong inferred date — re-stamping it with today would be a new wrong
//     date. Their clear carries source manual; an old task never dated has
//     none, and the two mean different things to a summary.
//
// An instant without a source is treated as the person's.
func stampTaskCompletion(t *TaskItem, wasDone bool, now string) {
	t.CompletedAt, t.CompletedSource = stampCompletion(t.Status == "done", wasDone, t.CompletedAt, t.CompletedSource, now)
}

// stampProjectCompletion is stampTaskCompletion for projects, keyed on the
// completed status. Cancelled is deliberately not completion: a summary of
// what got finished should not count what was abandoned.
func stampProjectCompletion(p *Project, wasCompleted bool, now string) {
	p.CompletedAt, p.CompletedSource = stampCompletion(p.Status == "completed", wasCompleted, p.CompletedAt, p.CompletedSource, now)
}

// stampCompletion is the rule both stamps share.
func stampCompletion(isDone, wasDone bool, at, src *string, now string) (*string, *string) {
	if !isDone {
		return nil, nil
	}
	if at == nil {
		if wasDone {
			return nil, src
		}
		a, s := now, CompletedRecorded
		return &a, &s
	}
	if src == nil {
		s := CompletedManual
		return at, &s
	}
	return at, src
}

// completionSkew is how far ahead of the server's clock a completion time
// may be and still be taken as "now": room for a client clock that runs a
// little fast, not for anything finished tomorrow.
const completionSkew = 5 * time.Minute

// CompletionInstant checks a completion time set by a person and returns it
// in the stored form, an RFC 3339 UTC instant. A time in the future is
// refused (ok false) — a summary must never find work in a period that has
// not happened yet — except within completionSkew of now, which is clamped to
// now. Shared by the API and MCP so both draw the line in the same place.
func CompletionInstant(t, now time.Time) (string, bool) {
	if t.After(now.Add(completionSkew)) {
		return "", false
	}
	if t.After(now) {
		t = now
	}
	return t.UTC().Format(time.RFC3339), true
}

// logProjectStatus appends to project_status_changes when a project's status
// actually changed. A write that leaves the status alone records nothing.
func logProjectStatus(ctx context.Context, ex execer, projectID, from, to, at string) error {
	if from == to {
		return nil
	}
	if _, err := ex.ExecContext(ctx,
		`INSERT INTO project_status_changes (project_id, from_status, to_status, changed_at)
		 VALUES (?, ?, ?, ?)`, projectID, from, to, at); err != nil {
		return fmt.Errorf("store: log status of project %q: %w", projectID, err)
	}
	return nil
}

// ProjectStatusChange is one row of a project's status history.
type ProjectStatusChange struct {
	ProjectID  string `json:"projectId"`
	FromStatus string `json:"fromStatus"`
	ToStatus   string `json:"toStatus"`
	// ChangedAt is an RFC 3339 UTC instant.
	ChangedAt string `json:"changedAt"`
}

// ListProjectStatusChanges returns the status history of every live project
// in the space, oldest first.
func (s *SpaceStore) ListProjectStatusChanges(ctx context.Context, spaceID string) ([]ProjectStatusChange, error) {
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	return listProjectStatusChanges(ctx, db)
}

// listProjectStatusChanges is ListProjectStatusChanges via q, so a snapshot can read it inside one transaction.
func listProjectStatusChanges(ctx context.Context, q querier) ([]ProjectStatusChange, error) {
	db := q
	rows, err := db.QueryContext(ctx,
		`SELECT c.project_id, COALESCE(c.from_status, ''), c.to_status, c.changed_at
		 FROM project_status_changes c JOIN projects p ON p.id = c.project_id
		 WHERE p.deleted_at IS NULL ORDER BY c.changed_at, c.id`)
	if err != nil {
		return nil, fmt.Errorf("store: list status changes: %w", err)
	}
	defer closeQuietly(rows)
	out := []ProjectStatusChange{}
	for rows.Next() {
		var c ProjectStatusChange
		if err := rows.Scan(&c.ProjectID, &c.FromStatus, &c.ToStatus, &c.ChangedAt); err != nil {
			return nil, fmt.Errorf("store: scan status change: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list status changes: %w", err)
	}
	return out, nil
}

// SummarySnapshot is everything a work summary reads — the space's projects,
// activities and tasks, its project status history, and when it began
// recording completions — taken in one transaction, so they agree. The rest
// of SpaceState is left empty. Read separately, a status change
// landing between them could pair a project still "active" in the state with
// a history that says it went blocked.
func (s *SpaceStore) SummarySnapshot(ctx context.Context, spaceID string) (SpaceState, []ProjectStatusChange, string, error) {
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return SpaceState{}, nil, "", err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return SpaceState{}, nil, "", fmt.Errorf("store: summary snapshot: begin: %w", err)
	}
	defer rollbackQuietly(tx)
	var st SpaceState
	if st.Projects, err = listProjects(ctx, tx); err != nil {
		return SpaceState{}, nil, "", err
	}
	if st.Activities, err = listActivities(ctx, tx); err != nil {
		return SpaceState{}, nil, "", err
	}
	if st.Tasks, err = listTasks(ctx, tx); err != nil {
		return SpaceState{}, nil, "", err
	}
	// Notes, reminders and inbox items play no part in a summary; reading
	// them would be three more scans for data that is thrown away.
	st.Notes, st.Reminders, st.Inbox = []NoteItem{}, []Reminder{}, []InboxItem{}
	changes, err := listProjectStatusChanges(ctx, tx)
	if err != nil {
		return SpaceState{}, nil, "", err
	}
	since, err := completionsRecordedSince(ctx, tx)
	if err != nil {
		return SpaceState{}, nil, "", err
	}
	return st, changes, since, nil
}
