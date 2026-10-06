package store

import (
	"context"
	"fmt"
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
//   - Staying done: left exactly as the write has it. That includes nil, which
//     is how a person says "I don't know when" after clearing a wrong
//     inferred date — re-stamping it with today would be a new wrong date.
//
// The source is kept consistent with the instant: an instant without one is
// treated as the person's, and no instant means no source.
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
			return nil, nil
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
