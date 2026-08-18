package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// This file implements the trash (#16). Deleting marks a row and hides it from
// every read; restoring unmarks it; purging is the only thing that removes
// data. The point is that a mistaken delete — by a person or by an agent over
// MCP — stops being final.
//
// Two columns carry it. deleted_at is the marker and the clock. delete_batch
// groups the rows one action removed, which is what makes restoring a project
// safe: without it, restoring would have to unmark every child row referencing
// the project, including one the person had deleted separately a week earlier.
// A row deleted on its own gets a batch of its own, so restore is one
// operation whichever way the row arrived.
//
// A soft cascade needs no detachment bookkeeping. Deleting a project used to
// null the project_id on its reminders and inbox items, purely because the
// foreign key would not survive the row disappearing. A marked project is
// still there, so those references stay valid — the reminder simply reads as
// unfiled while its project is in the trash, and restoring puts it back.
// The real detach happens at purge, where the row really does go.

// trashedTables are the entity tables that carry the trash columns, in the
// order a purge must visit them: children before the projects they reference.
var trashedTables = []string{"activities", "tasks", "notes", "reminders", "inbox", "projects"}

// TrashEntity names one kind of trashed thing on the wire. These match the
// singular names the API and MCP surfaces already use for entities.
const (
	TrashProject  = "project"
	TrashActivity = "activity"
	TrashTask     = "task"
	TrashNote     = "note"
	TrashReminder = "reminder"
	TrashInbox    = "inbox_item"
)

// entityTable maps a wire entity name to its table.
var entityTable = map[string]string{
	TrashProject:  "projects",
	TrashActivity: "activities",
	TrashTask:     "tasks",
	TrashNote:     "notes",
	TrashReminder: "reminders",
	TrashInbox:    "inbox",
}

// tableEntity is entityTable inverted.
var tableEntity = func() map[string]string {
	m := make(map[string]string, len(entityTable))
	for entity, table := range entityTable {
		m[table] = entity
	}
	return m
}()

// labelColumn is the column each table's one-line description comes from, so
// a trash listing can say what a row was without the caller fetching it.
var labelColumn = map[string]string{
	"projects":   "name",
	"activities": "title",
	"tasks":      "title",
	"notes":      "title",
	"reminders":  "text",
	"inbox":      "raw",
}

// TrashItem is one entry in a trash listing: one DELETE, not one row.
//
// Rows are grouped by batch because that is what restore and purge act on. A
// per-row listing was the obvious first shape and it was wrong: deleting one
// project put sixteen rows on screen, the project itself buried among the
// activities it took, and every row carried a Restore button that did the
// same thing without saying so.
type TrashItem struct {
	// Entity is the wire name: project, activity, task, note, reminder,
	// inbox_item.
	Entity string `json:"entity"`
	ID     string `json:"id"`
	// Label is the row's one-line description, for showing what this was.
	Label string `json:"label"`
	// DeletedAt is when it was trashed (RFC 3339 UTC — an instant, not a
	// calendar day; see the note on time in docs/api.md).
	DeletedAt string `json:"deletedAt"`
	// Batch groups everything one delete removed. Restoring or purging any
	// member acts on the whole batch, so a cascaded project comes back whole.
	Batch string `json:"batch"`
	// BatchSize is how many rows this delete removed, including the one
	// described above. Greater than one only for a project that took content
	// with it — exactly when someone wants to know before restoring.
	BatchSize int `json:"batchSize"`
}

// newBatch returns an opaque id grouping one delete.
func newBatch() (string, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("store: generate delete batch: %w", err)
	}
	return "del-" + hex.EncodeToString(buf[:]), nil
}

// softDeleteRow marks one row in table, in its own batch. Returns ErrNotFound
// if the id does not exist or is already trashed — deleting something twice is
// the caller acting on a stale view, and reporting it is more useful than
// silently moving the deletion date.
func (s *SpaceStore) softDeleteRow(ctx context.Context, spaceID, table, id string) error {
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return err
	}
	batch, err := newBatch()
	if err != nil {
		return err
	}
	res, err := db.ExecContext(ctx,
		//nolint:gosec // table is from trashedTables, never caller input.
		`UPDATE `+table+` SET deleted_at = ?, delete_batch = ? WHERE id = ? AND deleted_at IS NULL`,
		s.opts.now(), batch, id)
	if err != nil {
		return fmt.Errorf("store: delete %s %q: %w", table, id, err)
	}
	return notFoundIfZero(res, tableEntity[table], id)
}

// SoftDeleteProject marks a project and everything it owns in one batch, so
// restoring brings back exactly what this delete removed.
//
// Reminders and inbox items are left entirely alone. They reference the
// project rather than belonging to it, and a marked project still exists for
// the reference to point at; they read as unfiled until it is restored or
// purged. Returns ErrNotFound if the project does not exist or is already
// trashed.
func (s *SpaceStore) SoftDeleteProject(ctx context.Context, spaceID, id string) (ProjectCascadeResult, error) {
	if err := requireID("project", id); err != nil {
		return ProjectCascadeResult{}, err
	}
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return ProjectCascadeResult{}, err
	}
	batch, err := newBatch()
	if err != nil {
		return ProjectCascadeResult{}, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return ProjectCascadeResult{}, fmt.Errorf("store: delete project %q: begin: %w", id, err)
	}
	defer rollbackQuietly(tx)

	now := s.opts.now()
	// The project row first: if it is missing or already trashed, nothing
	// else should be marked, and the rollback makes that so.
	res, err := tx.ExecContext(ctx,
		`UPDATE projects SET deleted_at = ?, delete_batch = ? WHERE id = ? AND deleted_at IS NULL`,
		now, batch, id)
	if err != nil {
		return ProjectCascadeResult{}, fmt.Errorf("store: delete project %q: %w", id, err)
	}
	if err := notFoundIfZero(res, "project", id); err != nil {
		return ProjectCascadeResult{}, err
	}

	out := ProjectCascadeResult{Project: 1}
	owned := []struct {
		table string
		count *int64
	}{
		{"activities", &out.Activities},
		{"tasks", &out.Tasks},
		{"notes", &out.Notes},
	}
	for _, o := range owned {
		// Only rows that are still live: one already in the trash keeps its
		// own batch, so restoring this project does not resurrect it.
		r, err := tx.ExecContext(ctx,
			//nolint:gosec // table is a literal from the slice above.
			`UPDATE `+o.table+` SET deleted_at = ?, delete_batch = ?
			 WHERE project_id = ? AND deleted_at IS NULL`,
			now, batch, id)
		if err != nil {
			return ProjectCascadeResult{}, fmt.Errorf("store: delete project %q: %s: %w", id, o.table, err)
		}
		n, err := r.RowsAffected()
		if err != nil {
			return ProjectCascadeResult{}, fmt.Errorf("store: delete project %q: %s rows: %w", id, o.table, err)
		}
		*o.count = n
	}
	if err := tx.Commit(); err != nil {
		return ProjectCascadeResult{}, fmt.Errorf("store: delete project %q: commit: %w", id, err)
	}
	return out, nil
}

// ListTrash returns everything currently trashed, most recently deleted first.
func (s *SpaceStore) ListTrash(ctx context.Context, spaceID string) ([]TrashItem, error) {
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	// One UNION rather than six round trips: the trash is read as a whole and
	// sorted across entities, so it has to be one result set anyway.
	parts := make([]string, 0, len(trashedTables))
	for _, table := range trashedTables {
		parts = append(parts, fmt.Sprintf(
			`SELECT '%s' AS entity, id, %s AS label, deleted_at, delete_batch FROM %s WHERE deleted_at IS NOT NULL`,
			tableEntity[table], labelColumn[table], table))
	}
	query := strings.Join(parts, " UNION ALL ") + ` ORDER BY deleted_at DESC, entity, id`
	rows, err := db.QueryContext(ctx, query) //nolint:gosec // built from fixed tables.
	if err != nil {
		return nil, fmt.Errorf("store: list trash: %w", err)
	}
	defer closeQuietly(rows)

	// One entry per batch, described by the row that best represents the
	// delete: the project if there is one, since "I deleted Loom" is what the
	// person did — the activities went along with it.
	order := []string{}
	byBatch := map[string]TrashItem{}
	sizes := map[string]int{}
	for rows.Next() {
		var it TrashItem
		if err := rows.Scan(&it.Entity, &it.ID, &it.Label, &it.DeletedAt, &it.Batch); err != nil {
			return nil, fmt.Errorf("store: scan trash: %w", err)
		}
		sizes[it.Batch]++
		prev, seen := byBatch[it.Batch]
		if !seen {
			order = append(order, it.Batch)
			byBatch[it.Batch] = it
			continue
		}
		if prev.Entity != TrashProject && it.Entity == TrashProject {
			byBatch[it.Batch] = it
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list trash: %w", err)
	}
	out := make([]TrashItem, 0, len(order))
	for _, batch := range order {
		it := byBatch[batch]
		it.BatchSize = sizes[batch]
		out = append(out, it)
	}
	return out, nil
}

// batchOf returns the delete batch a trashed row belongs to.
func batchOf(ctx context.Context, q rowQuerier, table, id string) (string, error) {
	var batch sql.NullString
	//nolint:gosec // table is looked up from entityTable, never caller input.
	err := q.QueryRowContext(ctx,
		`SELECT delete_batch FROM `+table+` WHERE id = ? AND deleted_at IS NOT NULL`, id).Scan(&batch)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("store: trashed %s %q: %w", tableEntity[table], id, ErrNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("store: read delete batch for %s %q: %w", table, id, err)
	}
	if !batch.Valid || batch.String == "" {
		// Every soft delete writes a batch; a marked row without one would
		// mean the columns were written by something other than this file.
		return "", fmt.Errorf("store: trashed %s %q has no delete batch", tableEntity[table], id)
	}
	return batch.String, nil
}

// RestoreItem restores the trashed row and everything deleted alongside it,
// returning the number of rows brought back. Restoring a project restores the
// content that went with it; restoring one of that content on its own is not
// possible, because it was not deleted on its own.
func (s *SpaceStore) RestoreItem(ctx context.Context, spaceID, entity, id string) (int64, error) {
	table, ok := entityTable[entity]
	if !ok {
		return 0, fmt.Errorf("store: restore: unknown entity %q", entity)
	}
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return 0, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("store: restore %s %q: begin: %w", entity, id, err)
	}
	defer rollbackQuietly(tx)

	batch, err := batchOf(ctx, tx, table, id)
	if err != nil {
		return 0, err
	}
	var restored int64
	for _, t := range trashedTables {
		//nolint:gosec // t is a literal from trashedTables.
		r, err := tx.ExecContext(ctx,
			`UPDATE `+t+` SET deleted_at = NULL, delete_batch = NULL WHERE delete_batch = ?`, batch)
		if err != nil {
			// classifyConstraint so a collision with the partial unique index —
			// restoring a catch-all when a newer live one already exists —
			// surfaces as ErrDuplicateID (a clean 409), not a raw 500.
			return 0, fmt.Errorf("store: restore batch %q: %s: %w", batch, t, classifyConstraint(err))
		}
		n, err := r.RowsAffected()
		if err != nil {
			return 0, fmt.Errorf("store: restore batch %q: %s rows: %w", batch, t, err)
		}
		restored += n
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("store: restore %s %q: commit: %w", entity, id, err)
	}
	return restored, nil
}

// PurgeItem permanently removes the trashed row and its whole batch.
func (s *SpaceStore) PurgeItem(ctx context.Context, spaceID, entity, id string) (int64, error) {
	table, ok := entityTable[entity]
	if !ok {
		return 0, fmt.Errorf("store: purge: unknown entity %q", entity)
	}
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return 0, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("store: purge %s %q: begin: %w", entity, id, err)
	}
	defer rollbackQuietly(tx)
	batch, err := batchOf(ctx, tx, table, id)
	if err != nil {
		return 0, err
	}
	n, err := purgeWhere(ctx, tx, `delete_batch = ?`, batch)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("store: purge %s %q: commit: %w", entity, id, err)
	}
	return n, nil
}

// EmptyTrash permanently removes everything currently trashed.
func (s *SpaceStore) EmptyTrash(ctx context.Context, spaceID string) (int64, error) {
	return s.purge(ctx, spaceID, `deleted_at IS NOT NULL`)
}

// PurgeExpired permanently removes everything trashed before the given
// timestamp. This is the retention sweep; before is an RFC 3339 UTC instant.
func (s *SpaceStore) PurgeExpired(ctx context.Context, spaceID, before string) (int64, error) {
	return s.purge(ctx, spaceID, `deleted_at IS NOT NULL AND deleted_at < ?`, before)
}

// purge runs one hard-delete pass in a transaction.
func (s *SpaceStore) purge(ctx context.Context, spaceID, where string, args ...any) (int64, error) {
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return 0, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("store: purge: begin: %w", err)
	}
	defer rollbackQuietly(tx)
	n, err := purgeWhere(ctx, tx, where, args...)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("store: purge: commit: %w", err)
	}
	return n, nil
}

// purgeWhere hard-deletes matching rows from every trashed table inside tx.
//
// This is where the detachment the soft cascade did not need finally happens:
// a project row is about to go for real, so nothing may still reference it.
//
// The set of things that can reference a purged project is WIDER than it was
// under the old hard cascade, and that is the subtle part. The hard cascade
// deleted a project's activities, tasks and notes in the same statement, so
// they could never outlive it and only the loose references — reminders and
// inbox items — needed detaching. Soft delete breaks that: a task trashed on
// its own keeps its own batch and is not part of the project's, so purging
// the project by batch leaves it behind, pointing at a row that is going
// away. The foreign key then fails and the entire purge rolls back — which
// wedges Empty trash and the retention sweep for the whole space, not just
// this one delete.
//
// So each table is handled by what its reference can survive:
//   - activities.project_id is NOT NULL, so an activity cannot be orphaned;
//     any that reference a purged project go with it.
//   - tasks and notes may have no project, so they are detached and live on
//     unfiled — restoring one later gives an unfiled task rather than
//     nothing.
//   - reminders and inbox items are detached, as they always were: a
//     reminder's text stands on its own.
func purgeWhere(ctx context.Context, tx *sql.Tx, where string, args ...any) (int64, error) {
	//nolint:gosec // where is a literal from this file, never caller input.
	rows, err := tx.QueryContext(ctx, `SELECT id FROM projects WHERE `+where, args...)
	if err != nil {
		return 0, fmt.Errorf("store: purge: find projects: %w", err)
	}
	var projectIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			closeQuietly(rows)
			return 0, fmt.Errorf("store: purge: scan project: %w", err)
		}
		projectIDs = append(projectIDs, id)
	}
	if err := rows.Err(); err != nil {
		closeQuietly(rows)
		return 0, fmt.Errorf("store: purge: find projects: %w", err)
	}
	closeQuietly(rows)

	var total int64
	for _, pid := range projectIDs {
		// Detachable references first.
		for _, d := range []struct{ table, col string }{
			{"reminders", "project_id"},
			{"inbox", "suggested_project_id"},
			{"tasks", "project_id"},
			{"notes", "project_id"},
		} {
			//nolint:gosec // table and column are literals from the slice above.
			if _, err := tx.ExecContext(ctx,
				`UPDATE `+d.table+` SET `+d.col+` = NULL WHERE `+d.col+` = ?`, pid); err != nil {
				return 0, fmt.Errorf("store: purge: detach %s from project %q: %w", d.table, pid, err)
			}
		}
		// Activities cannot be detached — project_id is NOT NULL, and an
		// activity is defined as a fact about a project. Any left over go
		// with it, and they count towards what the purge removed.
		res, err := tx.ExecContext(ctx, `DELETE FROM activities WHERE project_id = ?`, pid)
		if err != nil {
			return 0, fmt.Errorf("store: purge: activities of project %q: %w", pid, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return 0, fmt.Errorf("store: purge: activity rows of project %q: %w", pid, err)
		}
		total += n
	}

	for _, t := range trashedTables {
		//nolint:gosec // t is a literal from trashedTables, where from this file.
		r, err := tx.ExecContext(ctx, `DELETE FROM `+t+` WHERE `+where, args...)
		if err != nil {
			return 0, fmt.Errorf("store: purge %s: %w", t, err)
		}
		n, err := r.RowsAffected()
		if err != nil {
			return 0, fmt.Errorf("store: purge %s rows: %w", t, err)
		}
		total += n
	}
	return total, nil
}
