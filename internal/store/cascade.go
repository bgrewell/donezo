package store

import (
	"context"
	"fmt"
)

// This file holds the transactional cascade delete behind
// DELETE /api/spaces/{id}/projects/{pid}. The distinction it encodes is
// deliberate: content a project owns (activities, tasks, notes) dies with
// it, while loose references to it (inbox suggestions, reminder links)
// are detached — a raw capture and a reminder's text stand alone.

// ProjectCascadeResult reports, per table, what one DeleteProjectCascade
// removed or detached. Field names marshal to the wire shape the frontend
// shows in its confirmation aftermath.
type ProjectCascadeResult struct {
	// Project is 1: the deleted project row itself.
	Project int64 `json:"project"`
	// Activities is the count of deleted activity rows.
	Activities int64 `json:"activities"`
	// Tasks is the count of deleted task rows.
	Tasks int64 `json:"tasks"`
	// Notes is the count of deleted note rows.
	Notes int64 `json:"notes"`
	// DetachedInbox is the count of inbox items whose
	// suggested_project_id was nulled, not deleted.
	DetachedInbox int64 `json:"detachedInbox"`
	// DetachedReminders is the count of reminders whose project_id was
	// nulled, not deleted.
	DetachedReminders int64 `json:"detachedReminders"`
}

// DeleteProjectCascade removes a project and every row it owns —
// activities, tasks, and notes referencing it — and detaches the loose
// references: inbox items lose their suggested_project_id and reminders
// lose their project_id, both nulled rather than deleted. Everything runs
// in one transaction, so a failure (including an unknown project id)
// leaves the space untouched. Returns per-table counts, or ErrNotFound if
// the project does not exist.
func (s *SpaceStore) DeleteProjectCascade(ctx context.Context, spaceID, id string) (ProjectCascadeResult, error) {
	if err := requireID("project", id); err != nil {
		return ProjectCascadeResult{}, err
	}
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return ProjectCascadeResult{}, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return ProjectCascadeResult{}, fmt.Errorf("store: delete project %q cascade: begin: %w", id, err)
	}
	defer rollbackQuietly(tx)
	// Owned content and reminder detachment must precede the project row:
	// activities, tasks, notes, and reminders all carry foreign keys into
	// projects. The inbox detach has no foreign key but belongs in the
	// same transaction. A missing project surfaces at the final delete and
	// rolls all of this back.
	var res ProjectCascadeResult
	steps := []struct {
		dst   *int64
		query string
	}{
		{&res.Activities, `DELETE FROM activities WHERE project_id = ?`},
		{&res.Tasks, `DELETE FROM tasks WHERE project_id = ?`},
		{&res.Notes, `DELETE FROM notes WHERE project_id = ?`},
		{&res.DetachedInbox, `UPDATE inbox SET suggested_project_id = NULL WHERE suggested_project_id = ?`},
		{&res.DetachedReminders, `UPDATE reminders SET project_id = NULL WHERE project_id = ?`},
	}
	for _, step := range steps {
		n, err := execCount(ctx, tx, step.query, id)
		if err != nil {
			return ProjectCascadeResult{}, fmt.Errorf("store: delete project %q cascade: %w", id, err)
		}
		*step.dst = n
	}
	del, err := tx.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, id)
	if err != nil {
		return ProjectCascadeResult{}, fmt.Errorf("store: delete project %q cascade: %w", id, classifyConstraint(err))
	}
	if err := notFoundIfZero(del, "project", id); err != nil {
		return ProjectCascadeResult{}, err
	}
	res.Project = 1
	if err := tx.Commit(); err != nil {
		return ProjectCascadeResult{}, fmt.Errorf("store: delete project %q cascade: commit: %w", id, err)
	}
	return res, nil
}

// execCount runs one write statement via ex and returns its affected-row
// count.
func execCount(ctx context.Context, ex execer, query string, args ...any) (int64, error) {
	res, err := ex.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
