package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// SpaceStore manages per-space SQLite files under <data-dir>/spaces/.
// Each space id maps to one database file (<id>.db) that is opened — and
// created + migrated — on first use, then cached. All list methods return
// rows in insertion order so a seeded space round-trips byte-for-byte.
type SpaceStore struct {
	opts options

	mu sync.Mutex
	// conns caches one open handle per space for the life of the process;
	// the only removal path is Close. This is a deliberate tradeoff: each
	// cached handle pins ~3 file descriptors (db + WAL + shm), which is
	// nothing for the personal deployments donezod targets (a handful of
	// spaces), and eviction would reintroduce open/migrate/PRAGMA churn on
	// every revisit. If usage patterns ever change (bulk imports, hundreds
	// of spaces), add idle-timeout or LRU eviction here.
	conns map[string]*sql.DB
}

// NewSpaceStore creates a SpaceStore rooted at the configured data
// directory. Space database files are created lazily.
func NewSpaceStore(opts ...Option) (*SpaceStore, error) {
	o, err := newOptions(opts)
	if err != nil {
		return nil, err
	}
	// 0o700: space databases are personal data and must not be readable
	// by other users on shared hosts.
	if err := os.MkdirAll(filepath.Join(o.dataDir, "spaces"), 0o700); err != nil {
		return nil, fmt.Errorf("store: create spaces dir: %w", err)
	}
	return &SpaceStore{opts: o, conns: map[string]*sql.DB{}}, nil
}

// Close closes every cached space database handle, returning the first
// error encountered.
func (s *SpaceStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var firstErr error
	for id, db := range s.conns {
		if err := db.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("store: close space %q: %w", id, err)
		}
		delete(s.conns, id)
	}
	return firstErr
}

// db returns the cached handle for spaceID, opening (and migrating) the
// space database file on first use.
func (s *SpaceStore) db(ctx context.Context, spaceID string) (*sql.DB, error) {
	if err := ValidateSpaceID(spaceID); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if db, ok := s.conns[spaceID]; ok {
		return db, nil
	}
	db, err := openDB(filepath.Join(s.opts.dataDir, "spaces", spaceID+".db"))
	if err != nil {
		return nil, err
	}
	if _, err := migrate(ctx, db, spaceMigrationFS, "migrations/space", s.opts.now); err != nil {
		closeQuietly(db)
		return nil, err
	}
	s.conns[spaceID] = db
	return db, nil
}

// execer is the subset of *sql.DB and *sql.Tx used by row-writing helpers,
// so entity inserts can run standalone or inside a transaction.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// rowQuerier is the subset of *sql.DB and *sql.Tx used by single-row read
// helpers.
type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// marshalList serializes a JSON-array column value, normalizing nil to []
// so columns never hold JSON null.
func marshalList[T any](v []T) (string, error) {
	if v == nil {
		v = []T{}
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("store: marshal list: %w", err)
	}
	return string(b), nil
}

// unmarshalList parses a JSON-array column value; empty/NULL text yields
// an empty, non-nil slice.
func unmarshalList[T any](raw string) ([]T, error) {
	if raw == "" {
		return []T{}, nil
	}
	var v []T
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return nil, fmt.Errorf("store: unmarshal list: %w", err)
	}
	if v == nil {
		v = []T{}
	}
	return v, nil
}

// requireID rejects blank primary keys before they hit SQL.
func requireID(kind, id string) error {
	if id == "" {
		return fmt.Errorf("store: %s id is required", kind)
	}
	return nil
}

// notFoundIfZero converts a zero-rows-affected result into ErrNotFound.
func notFoundIfZero(res sql.Result, kind, id string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: %s %q: %w", kind, id, err)
	}
	if n == 0 {
		return fmt.Errorf("store: %s %q: %w", kind, id, ErrNotFound)
	}
	return nil
}

// ─── Projects ───────────────────────────────────────────────────────────

// CreateProject inserts a project. CreatedAt/UpdatedAt are set from the
// store clock.
func (s *SpaceStore) CreateProject(ctx context.Context, spaceID string, p Project) (Project, error) {
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return Project{}, err
	}
	return s.insertProject(ctx, db, p)
}

// insertProject writes one project row via ex, stamping CreatedAt and
// UpdatedAt from the store clock.
func (s *SpaceStore) insertProject(ctx context.Context, ex execer, p Project) (Project, error) {
	if err := requireID("project", p.ID); err != nil {
		return Project{}, err
	}
	alt, err := marshalList(p.AltNextActions)
	if err != nil {
		return Project{}, err
	}
	tags, err := marshalList(p.Tags)
	if err != nil {
		return Project{}, err
	}
	now := s.opts.now()
	p.CreatedAt, p.UpdatedAt = now, now
	if _, err := ex.ExecContext(ctx,
		`INSERT INTO projects (id, name, color, purpose, outcome, current_focus, next_action,
		   alt_next_actions, status, resume_context, waiting_on, tags, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.Color, p.Purpose, p.Outcome, p.CurrentFocus, p.NextAction,
		alt, p.Status, p.ResumeContext, p.WaitingOn, tags, p.CreatedAt, p.UpdatedAt); err != nil {
		return Project{}, fmt.Errorf("store: create project %q: %w", p.ID, classifyConstraint(err))
	}
	return p, nil
}

// scanProject reads one projects row.
func scanProject(row interface{ Scan(...any) error }) (Project, error) {
	var p Project
	var alt, tags string
	err := row.Scan(&p.ID, &p.Name, &p.Color, &p.Purpose, &p.Outcome, &p.CurrentFocus,
		&p.NextAction, &alt, &p.Status, &p.ResumeContext, &p.WaitingOn, &tags,
		&p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return Project{}, err
	}
	if p.AltNextActions, err = unmarshalList[string](alt); err != nil {
		return Project{}, err
	}
	if p.Tags, err = unmarshalList[string](tags); err != nil {
		return Project{}, err
	}
	return p, nil
}

const projectColumns = `id, name, color, purpose, outcome, current_focus, next_action,
	alt_next_actions, status, resume_context, waiting_on, tags, created_at, updated_at`

// GetProject returns one project by id, or ErrNotFound.
func (s *SpaceStore) GetProject(ctx context.Context, spaceID, id string) (Project, error) {
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return Project{}, err
	}
	return getProjectRow(ctx, db, id)
}

// getProjectRow reads one project by id via q, or ErrNotFound.
func getProjectRow(ctx context.Context, q rowQuerier, id string) (Project, error) {
	p, err := scanProject(q.QueryRowContext(ctx,
		`SELECT `+projectColumns+` FROM projects WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, fmt.Errorf("store: project %q: %w", id, ErrNotFound)
	}
	if err != nil {
		return Project{}, fmt.Errorf("store: get project %q: %w", id, err)
	}
	return p, nil
}

// UpdateProject rewrites all mutable fields of an existing project and
// refreshes UpdatedAt, returning the stored row. The update and the
// re-read run in one transaction so a concurrent delete cannot make a
// committed update report ErrNotFound. Returns ErrNotFound if the id does
// not exist.
func (s *SpaceStore) UpdateProject(ctx context.Context, spaceID string, p Project) (Project, error) {
	if err := requireID("project", p.ID); err != nil {
		return Project{}, err
	}
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return Project{}, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Project{}, fmt.Errorf("store: update project %q: begin: %w", p.ID, err)
	}
	defer rollbackQuietly(tx)
	p.UpdatedAt = s.opts.now()
	res, err := execUpdateProject(ctx, tx, p)
	if err != nil {
		return Project{}, fmt.Errorf("store: update project %q: %w", p.ID, classifyConstraint(err))
	}
	if err := notFoundIfZero(res, "project", p.ID); err != nil {
		return Project{}, err
	}
	stored, err := getProjectRow(ctx, tx, p.ID)
	if err != nil {
		return Project{}, err
	}
	if err := tx.Commit(); err != nil {
		return Project{}, fmt.Errorf("store: update project %q: commit: %w", p.ID, err)
	}
	return stored, nil
}

// execUpdateProject rewrites all mutable columns of a project row via ex.
// The caller stamps UpdatedAt first.
func execUpdateProject(ctx context.Context, ex execer, p Project) (sql.Result, error) {
	alt, err := marshalList(p.AltNextActions)
	if err != nil {
		return nil, err
	}
	tags, err := marshalList(p.Tags)
	if err != nil {
		return nil, err
	}
	return ex.ExecContext(ctx,
		`UPDATE projects SET name = ?, color = ?, purpose = ?, outcome = ?, current_focus = ?,
		   next_action = ?, alt_next_actions = ?, status = ?, resume_context = ?, waiting_on = ?,
		   tags = ?, updated_at = ? WHERE id = ?`,
		p.Name, p.Color, p.Purpose, p.Outcome, p.CurrentFocus, p.NextAction, alt, p.Status,
		p.ResumeContext, p.WaitingOn, tags, p.UpdatedAt, p.ID)
}

// DeleteProject removes a project by id. Returns ErrNotFound if absent.
func (s *SpaceStore) DeleteProject(ctx context.Context, spaceID, id string) error {
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return err
	}
	res, err := db.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete project %q: %w", id, err)
	}
	return notFoundIfZero(res, "project", id)
}

// ListProjects returns all projects in insertion order.
func (s *SpaceStore) ListProjects(ctx context.Context, spaceID string) ([]Project, error) {
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT `+projectColumns+` FROM projects ORDER BY rowid`)
	if err != nil {
		return nil, fmt.Errorf("store: list projects: %w", err)
	}
	defer closeQuietly(rows)
	out := []Project{}
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan project: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list projects: %w", err)
	}
	return out, nil
}

// ─── Activities ─────────────────────────────────────────────────────────

const activityColumns = `id, project_id, date, type, title, details, effort_hours, source,
	tags, links, next_action, planned, created_at, updated_at`

// CreateActivity inserts an activity entry. CreatedAt/UpdatedAt are set
// from the store clock. The referenced project must exist.
func (s *SpaceStore) CreateActivity(ctx context.Context, spaceID string, a ActivityEntry) (ActivityEntry, error) {
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return ActivityEntry{}, err
	}
	return s.insertActivity(ctx, db, a)
}

// insertActivity writes one activity row via ex, stamping CreatedAt and
// UpdatedAt from the store clock.
func (s *SpaceStore) insertActivity(ctx context.Context, ex execer, a ActivityEntry) (ActivityEntry, error) {
	if err := requireID("activity", a.ID); err != nil {
		return ActivityEntry{}, err
	}
	tags, err := marshalList(a.Tags)
	if err != nil {
		return ActivityEntry{}, err
	}
	links, err := marshalList(a.Links)
	if err != nil {
		return ActivityEntry{}, err
	}
	now := s.opts.now()
	a.CreatedAt, a.UpdatedAt = now, now
	if _, err := ex.ExecContext(ctx,
		`INSERT INTO activities (id, project_id, date, type, title, details, effort_hours,
		   source, tags, links, next_action, planned, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.ProjectID, a.Date, a.Type, a.Title, a.Details, a.EffortHours,
		a.Source, tags, links, a.NextAction, boolPtrToInt(a.Planned), a.CreatedAt, a.UpdatedAt); err != nil {
		return ActivityEntry{}, fmt.Errorf("store: create activity %q: %w", a.ID, classifyConstraint(err))
	}
	return a, nil
}

// scanActivity reads one activities row.
func scanActivity(row interface{ Scan(...any) error }) (ActivityEntry, error) {
	var a ActivityEntry
	var tags, links string
	var planned *int64
	err := row.Scan(&a.ID, &a.ProjectID, &a.Date, &a.Type, &a.Title, &a.Details,
		&a.EffortHours, &a.Source, &tags, &links, &a.NextAction, &planned,
		&a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return ActivityEntry{}, err
	}
	a.Planned = intPtrToBool(planned)
	if a.Tags, err = unmarshalList[string](tags); err != nil {
		return ActivityEntry{}, err
	}
	if a.Links, err = unmarshalList[ActivityLink](links); err != nil {
		return ActivityEntry{}, err
	}
	return a, nil
}

// GetActivity returns one activity by id, or ErrNotFound.
func (s *SpaceStore) GetActivity(ctx context.Context, spaceID, id string) (ActivityEntry, error) {
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return ActivityEntry{}, err
	}
	return getActivityRow(ctx, db, id)
}

// getActivityRow reads one activity by id via q, or ErrNotFound.
func getActivityRow(ctx context.Context, q rowQuerier, id string) (ActivityEntry, error) {
	a, err := scanActivity(q.QueryRowContext(ctx,
		`SELECT `+activityColumns+` FROM activities WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return ActivityEntry{}, fmt.Errorf("store: activity %q: %w", id, ErrNotFound)
	}
	if err != nil {
		return ActivityEntry{}, fmt.Errorf("store: get activity %q: %w", id, err)
	}
	return a, nil
}

// UpdateActivity rewrites all mutable fields of an existing activity and
// refreshes UpdatedAt, returning the stored row. The update and the
// re-read run in one transaction so a concurrent delete cannot make a
// committed update report ErrNotFound. Returns ErrNotFound if the id does
// not exist.
func (s *SpaceStore) UpdateActivity(ctx context.Context, spaceID string, a ActivityEntry) (ActivityEntry, error) {
	if err := requireID("activity", a.ID); err != nil {
		return ActivityEntry{}, err
	}
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return ActivityEntry{}, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return ActivityEntry{}, fmt.Errorf("store: update activity %q: begin: %w", a.ID, err)
	}
	defer rollbackQuietly(tx)
	a.UpdatedAt = s.opts.now()
	res, err := execUpdateActivity(ctx, tx, a)
	if err != nil {
		return ActivityEntry{}, fmt.Errorf("store: update activity %q: %w", a.ID, classifyConstraint(err))
	}
	if err := notFoundIfZero(res, "activity", a.ID); err != nil {
		return ActivityEntry{}, err
	}
	stored, err := getActivityRow(ctx, tx, a.ID)
	if err != nil {
		return ActivityEntry{}, err
	}
	if err := tx.Commit(); err != nil {
		return ActivityEntry{}, fmt.Errorf("store: update activity %q: commit: %w", a.ID, err)
	}
	return stored, nil
}

// execUpdateActivity rewrites all mutable columns of an activity row via
// ex. The caller stamps UpdatedAt first.
func execUpdateActivity(ctx context.Context, ex execer, a ActivityEntry) (sql.Result, error) {
	tags, err := marshalList(a.Tags)
	if err != nil {
		return nil, err
	}
	links, err := marshalList(a.Links)
	if err != nil {
		return nil, err
	}
	return ex.ExecContext(ctx,
		`UPDATE activities SET project_id = ?, date = ?, type = ?, title = ?, details = ?,
		   effort_hours = ?, source = ?, tags = ?, links = ?, next_action = ?, planned = ?,
		   updated_at = ? WHERE id = ?`,
		a.ProjectID, a.Date, a.Type, a.Title, a.Details, a.EffortHours, a.Source,
		tags, links, a.NextAction, boolPtrToInt(a.Planned), a.UpdatedAt, a.ID)
}

// DeleteActivity removes an activity by id. Returns ErrNotFound if absent.
func (s *SpaceStore) DeleteActivity(ctx context.Context, spaceID, id string) error {
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return err
	}
	res, err := db.ExecContext(ctx, `DELETE FROM activities WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete activity %q: %w", id, err)
	}
	return notFoundIfZero(res, "activity", id)
}

// ListActivities returns all activities in insertion order.
func (s *SpaceStore) ListActivities(ctx context.Context, spaceID string) ([]ActivityEntry, error) {
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT `+activityColumns+` FROM activities ORDER BY rowid`)
	if err != nil {
		return nil, fmt.Errorf("store: list activities: %w", err)
	}
	defer closeQuietly(rows)
	out := []ActivityEntry{}
	for rows.Next() {
		a, err := scanActivity(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan activity: %w", err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list activities: %w", err)
	}
	return out, nil
}

// ─── Tasks ──────────────────────────────────────────────────────────────

// CreateTask inserts a task. CreatedAt is caller data (a frontend field),
// not a server timestamp, and must be provided.
func (s *SpaceStore) CreateTask(ctx context.Context, spaceID string, t TaskItem) (TaskItem, error) {
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return TaskItem{}, err
	}
	return insertTask(ctx, db, t)
}

// insertTask writes one task row via ex.
func insertTask(ctx context.Context, ex execer, t TaskItem) (TaskItem, error) {
	if err := requireID("task", t.ID); err != nil {
		return TaskItem{}, err
	}
	if _, err := ex.ExecContext(ctx,
		`INSERT INTO tasks (id, project_id, title, status, due, waiting_on, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.ProjectID, t.Title, t.Status, t.Due, t.WaitingOn, t.CreatedAt); err != nil {
		return TaskItem{}, fmt.Errorf("store: create task %q: %w", t.ID, classifyConstraint(err))
	}
	return t, nil
}

// GetTask returns one task by id, or ErrNotFound.
func (s *SpaceStore) GetTask(ctx context.Context, spaceID, id string) (TaskItem, error) {
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return TaskItem{}, err
	}
	return getTaskRow(ctx, db, id)
}

// getTaskRow reads one task by id via q, or ErrNotFound.
func getTaskRow(ctx context.Context, q rowQuerier, id string) (TaskItem, error) {
	var t TaskItem
	err := q.QueryRowContext(ctx,
		`SELECT id, project_id, title, status, due, waiting_on, created_at FROM tasks WHERE id = ?`,
		id).Scan(&t.ID, &t.ProjectID, &t.Title, &t.Status, &t.Due, &t.WaitingOn, &t.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return TaskItem{}, fmt.Errorf("store: task %q: %w", id, ErrNotFound)
	}
	if err != nil {
		return TaskItem{}, fmt.Errorf("store: get task %q: %w", id, err)
	}
	return t, nil
}

// UpdateTask rewrites all mutable fields of an existing task. Returns
// ErrNotFound if the id does not exist.
func (s *SpaceStore) UpdateTask(ctx context.Context, spaceID string, t TaskItem) (TaskItem, error) {
	if err := requireID("task", t.ID); err != nil {
		return TaskItem{}, err
	}
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return TaskItem{}, err
	}
	res, err := execUpdateTask(ctx, db, t)
	if err != nil {
		return TaskItem{}, fmt.Errorf("store: update task %q: %w", t.ID, classifyConstraint(err))
	}
	if err := notFoundIfZero(res, "task", t.ID); err != nil {
		return TaskItem{}, err
	}
	return t, nil
}

// execUpdateTask rewrites all mutable columns of a task row via ex.
func execUpdateTask(ctx context.Context, ex execer, t TaskItem) (sql.Result, error) {
	return ex.ExecContext(ctx,
		`UPDATE tasks SET project_id = ?, title = ?, status = ?, due = ?, waiting_on = ?,
		   created_at = ? WHERE id = ?`,
		t.ProjectID, t.Title, t.Status, t.Due, t.WaitingOn, t.CreatedAt, t.ID)
}

// DeleteTask removes a task by id. Returns ErrNotFound if absent.
func (s *SpaceStore) DeleteTask(ctx context.Context, spaceID, id string) error {
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return err
	}
	res, err := db.ExecContext(ctx, `DELETE FROM tasks WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete task %q: %w", id, err)
	}
	return notFoundIfZero(res, "task", id)
}

// ListTasks returns all tasks in insertion order.
func (s *SpaceStore) ListTasks(ctx context.Context, spaceID string) ([]TaskItem, error) {
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx,
		`SELECT id, project_id, title, status, due, waiting_on, created_at FROM tasks ORDER BY rowid`)
	if err != nil {
		return nil, fmt.Errorf("store: list tasks: %w", err)
	}
	defer closeQuietly(rows)
	out := []TaskItem{}
	for rows.Next() {
		var t TaskItem
		if err := rows.Scan(&t.ID, &t.ProjectID, &t.Title, &t.Status, &t.Due,
			&t.WaitingOn, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: scan task: %w", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list tasks: %w", err)
	}
	return out, nil
}

// ─── Notes ──────────────────────────────────────────────────────────────

// CreateNote inserts a note. CreatedAt is caller data and must be provided.
func (s *SpaceStore) CreateNote(ctx context.Context, spaceID string, n NoteItem) (NoteItem, error) {
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return NoteItem{}, err
	}
	return insertNote(ctx, db, n)
}

// insertNote writes one note row via ex.
func insertNote(ctx context.Context, ex execer, n NoteItem) (NoteItem, error) {
	if err := requireID("note", n.ID); err != nil {
		return NoteItem{}, err
	}
	if _, err := ex.ExecContext(ctx,
		`INSERT INTO notes (id, project_id, title, body, created_at) VALUES (?, ?, ?, ?, ?)`,
		n.ID, n.ProjectID, n.Title, n.Body, n.CreatedAt); err != nil {
		return NoteItem{}, fmt.Errorf("store: create note %q: %w", n.ID, classifyConstraint(err))
	}
	return n, nil
}

// GetNote returns one note by id, or ErrNotFound.
func (s *SpaceStore) GetNote(ctx context.Context, spaceID, id string) (NoteItem, error) {
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return NoteItem{}, err
	}
	return getNoteRow(ctx, db, id)
}

// getNoteRow reads one note through any querier (a *sql.DB or a *sql.Tx), so
// PatchNote can read inside its transaction.
func getNoteRow(ctx context.Context, q rowQuerier, id string) (NoteItem, error) {
	var n NoteItem
	err := q.QueryRowContext(ctx,
		`SELECT id, project_id, title, body, created_at FROM notes WHERE id = ?`,
		id).Scan(&n.ID, &n.ProjectID, &n.Title, &n.Body, &n.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return NoteItem{}, fmt.Errorf("store: note %q: %w", id, ErrNotFound)
	}
	if err != nil {
		return NoteItem{}, fmt.Errorf("store: get note %q: %w", id, err)
	}
	return n, nil
}

// UpdateNote rewrites all mutable fields of an existing note. Returns
// ErrNotFound if the id does not exist. Callers applying a partial update
// should prefer PatchNote, which re-reads inside a transaction rather than
// writing back a snapshot that may have gone stale.
func (s *SpaceStore) UpdateNote(ctx context.Context, spaceID string, n NoteItem) (NoteItem, error) {
	if err := requireID("note", n.ID); err != nil {
		return NoteItem{}, err
	}
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return NoteItem{}, err
	}
	res, err := execUpdateNote(ctx, db, n)
	if err != nil {
		return NoteItem{}, fmt.Errorf("store: update note %q: %w", n.ID, classifyConstraint(err))
	}
	if err := notFoundIfZero(res, "note", n.ID); err != nil {
		return NoteItem{}, err
	}
	return n, nil
}

// execUpdateNote rewrites a note through any execer, so PatchNote can write
// inside its transaction.
func execUpdateNote(ctx context.Context, ex execer, n NoteItem) (sql.Result, error) {
	return ex.ExecContext(ctx,
		`UPDATE notes SET project_id = ?, title = ?, body = ?, created_at = ? WHERE id = ?`,
		n.ProjectID, n.Title, n.Body, n.CreatedAt, n.ID)
}

// DeleteNote removes a note by id. Returns ErrNotFound if absent.
func (s *SpaceStore) DeleteNote(ctx context.Context, spaceID, id string) error {
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return err
	}
	res, err := db.ExecContext(ctx, `DELETE FROM notes WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete note %q: %w", id, err)
	}
	return notFoundIfZero(res, "note", id)
}

// ListNotes returns all notes in insertion order.
func (s *SpaceStore) ListNotes(ctx context.Context, spaceID string) ([]NoteItem, error) {
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx,
		`SELECT id, project_id, title, body, created_at FROM notes ORDER BY rowid`)
	if err != nil {
		return nil, fmt.Errorf("store: list notes: %w", err)
	}
	defer closeQuietly(rows)
	out := []NoteItem{}
	for rows.Next() {
		var n NoteItem
		if err := rows.Scan(&n.ID, &n.ProjectID, &n.Title, &n.Body, &n.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: scan note: %w", err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list notes: %w", err)
	}
	return out, nil
}

// ─── Reminders ──────────────────────────────────────────────────────────

// CreateReminder inserts a reminder.
func (s *SpaceStore) CreateReminder(ctx context.Context, spaceID string, r Reminder) (Reminder, error) {
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return Reminder{}, err
	}
	return insertReminder(ctx, db, r)
}

// insertReminder writes one reminder row via ex.
func insertReminder(ctx context.Context, ex execer, r Reminder) (Reminder, error) {
	if err := requireID("reminder", r.ID); err != nil {
		return Reminder{}, err
	}
	if _, err := ex.ExecContext(ctx,
		`INSERT INTO reminders (id, text, remind_at, project_id, done) VALUES (?, ?, ?, ?, ?)`,
		r.ID, r.Text, r.RemindAt, r.ProjectID, boolPtrToInt(r.Done)); err != nil {
		return Reminder{}, fmt.Errorf("store: create reminder %q: %w", r.ID, classifyConstraint(err))
	}
	return r, nil
}

// GetReminder returns one reminder by id, or ErrNotFound.
func (s *SpaceStore) GetReminder(ctx context.Context, spaceID, id string) (Reminder, error) {
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return Reminder{}, err
	}
	return getReminderRow(ctx, db, id)
}

// getReminderRow reads one reminder by id via q, or ErrNotFound.
func getReminderRow(ctx context.Context, q rowQuerier, id string) (Reminder, error) {
	var r Reminder
	var done *int64
	err := q.QueryRowContext(ctx,
		`SELECT id, text, remind_at, project_id, done FROM reminders WHERE id = ?`,
		id).Scan(&r.ID, &r.Text, &r.RemindAt, &r.ProjectID, &done)
	if errors.Is(err, sql.ErrNoRows) {
		return Reminder{}, fmt.Errorf("store: reminder %q: %w", id, ErrNotFound)
	}
	if err != nil {
		return Reminder{}, fmt.Errorf("store: get reminder %q: %w", id, err)
	}
	r.Done = intPtrToBool(done)
	return r, nil
}

// UpdateReminder rewrites all mutable fields of an existing reminder.
// Returns ErrNotFound if the id does not exist.
func (s *SpaceStore) UpdateReminder(ctx context.Context, spaceID string, r Reminder) (Reminder, error) {
	if err := requireID("reminder", r.ID); err != nil {
		return Reminder{}, err
	}
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return Reminder{}, err
	}
	res, err := execUpdateReminder(ctx, db, r)
	if err != nil {
		return Reminder{}, fmt.Errorf("store: update reminder %q: %w", r.ID, classifyConstraint(err))
	}
	if err := notFoundIfZero(res, "reminder", r.ID); err != nil {
		return Reminder{}, err
	}
	return r, nil
}

// execUpdateReminder rewrites all mutable columns of a reminder row via ex.
func execUpdateReminder(ctx context.Context, ex execer, r Reminder) (sql.Result, error) {
	return ex.ExecContext(ctx,
		`UPDATE reminders SET text = ?, remind_at = ?, project_id = ?, done = ? WHERE id = ?`,
		r.Text, r.RemindAt, r.ProjectID, boolPtrToInt(r.Done), r.ID)
}

// DeleteReminder removes a reminder by id. Returns ErrNotFound if absent.
func (s *SpaceStore) DeleteReminder(ctx context.Context, spaceID, id string) error {
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return err
	}
	res, err := db.ExecContext(ctx, `DELETE FROM reminders WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete reminder %q: %w", id, err)
	}
	return notFoundIfZero(res, "reminder", id)
}

// ListReminders returns all reminders in insertion order.
func (s *SpaceStore) ListReminders(ctx context.Context, spaceID string) ([]Reminder, error) {
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx,
		`SELECT id, text, remind_at, project_id, done FROM reminders ORDER BY rowid`)
	if err != nil {
		return nil, fmt.Errorf("store: list reminders: %w", err)
	}
	defer closeQuietly(rows)
	out := []Reminder{}
	for rows.Next() {
		var r Reminder
		var done *int64
		if err := rows.Scan(&r.ID, &r.Text, &r.RemindAt, &r.ProjectID, &done); err != nil {
			return nil, fmt.Errorf("store: scan reminder: %w", err)
		}
		r.Done = intPtrToBool(done)
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list reminders: %w", err)
	}
	return out, nil
}

// ─── Inbox ──────────────────────────────────────────────────────────────

// CreateInboxItem inserts a raw capture.
func (s *SpaceStore) CreateInboxItem(ctx context.Context, spaceID string, it InboxItem) (InboxItem, error) {
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return InboxItem{}, err
	}
	return insertInboxItem(ctx, db, it)
}

// insertInboxItem writes one inbox row via ex.
func insertInboxItem(ctx context.Context, ex execer, it InboxItem) (InboxItem, error) {
	if err := requireID("inbox item", it.ID); err != nil {
		return InboxItem{}, err
	}
	if _, err := ex.ExecContext(ctx,
		`INSERT INTO inbox (id, raw, captured_at, suggested_kind, suggested_project_id, status)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		it.ID, it.Raw, it.CapturedAt, it.SuggestedKind, it.SuggestedProjectID, it.Status); err != nil {
		return InboxItem{}, fmt.Errorf("store: create inbox item %q: %w", it.ID, classifyConstraint(err))
	}
	return it, nil
}

// GetInboxItem returns one inbox item by id, or ErrNotFound.
func (s *SpaceStore) GetInboxItem(ctx context.Context, spaceID, id string) (InboxItem, error) {
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return InboxItem{}, err
	}
	return getInboxRow(ctx, db, id)
}

// getInboxRow reads one inbox item by id via q, or ErrNotFound.
func getInboxRow(ctx context.Context, q rowQuerier, id string) (InboxItem, error) {
	var it InboxItem
	err := q.QueryRowContext(ctx,
		`SELECT id, raw, captured_at, suggested_kind, suggested_project_id, status
		 FROM inbox WHERE id = ?`,
		id).Scan(&it.ID, &it.Raw, &it.CapturedAt, &it.SuggestedKind, &it.SuggestedProjectID, &it.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return InboxItem{}, fmt.Errorf("store: inbox item %q: %w", id, ErrNotFound)
	}
	if err != nil {
		return InboxItem{}, fmt.Errorf("store: get inbox item %q: %w", id, err)
	}
	return it, nil
}

// UpdateInboxItem rewrites all mutable fields of an existing inbox item.
// Returns ErrNotFound if the id does not exist.
func (s *SpaceStore) UpdateInboxItem(ctx context.Context, spaceID string, it InboxItem) (InboxItem, error) {
	if err := requireID("inbox item", it.ID); err != nil {
		return InboxItem{}, err
	}
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return InboxItem{}, err
	}
	res, err := execUpdateInbox(ctx, db, it)
	if err != nil {
		return InboxItem{}, fmt.Errorf("store: update inbox item %q: %w", it.ID, classifyConstraint(err))
	}
	if err := notFoundIfZero(res, "inbox item", it.ID); err != nil {
		return InboxItem{}, err
	}
	return it, nil
}

// execUpdateInbox rewrites all mutable columns of an inbox row via ex.
func execUpdateInbox(ctx context.Context, ex execer, it InboxItem) (sql.Result, error) {
	return ex.ExecContext(ctx,
		`UPDATE inbox SET raw = ?, captured_at = ?, suggested_kind = ?, suggested_project_id = ?,
		   status = ? WHERE id = ?`,
		it.Raw, it.CapturedAt, it.SuggestedKind, it.SuggestedProjectID, it.Status, it.ID)
}

// DeleteInboxItem removes an inbox item by id. Returns ErrNotFound if
// absent.
func (s *SpaceStore) DeleteInboxItem(ctx context.Context, spaceID, id string) error {
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return err
	}
	res, err := db.ExecContext(ctx, `DELETE FROM inbox WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete inbox item %q: %w", id, err)
	}
	return notFoundIfZero(res, "inbox item", id)
}

// ListInboxItems returns all inbox items in insertion order.
func (s *SpaceStore) ListInboxItems(ctx context.Context, spaceID string) ([]InboxItem, error) {
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx,
		`SELECT id, raw, captured_at, suggested_kind, suggested_project_id, status
		 FROM inbox ORDER BY rowid`)
	if err != nil {
		return nil, fmt.Errorf("store: list inbox: %w", err)
	}
	defer closeQuietly(rows)
	out := []InboxItem{}
	for rows.Next() {
		var it InboxItem
		if err := rows.Scan(&it.ID, &it.Raw, &it.CapturedAt, &it.SuggestedKind,
			&it.SuggestedProjectID, &it.Status); err != nil {
			return nil, fmt.Errorf("store: scan inbox item: %w", err)
		}
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list inbox: %w", err)
	}
	return out, nil
}

// ─── State ──────────────────────────────────────────────────────────────

// State assembles the complete content of a space: every entity list, in
// insertion order, ready to serialize for GET /api/spaces/{id}/state.
func (s *SpaceStore) State(ctx context.Context, spaceID string) (SpaceState, error) {
	var st SpaceState
	var err error
	if st.Projects, err = s.ListProjects(ctx, spaceID); err != nil {
		return SpaceState{}, err
	}
	if st.Activities, err = s.ListActivities(ctx, spaceID); err != nil {
		return SpaceState{}, err
	}
	if st.Tasks, err = s.ListTasks(ctx, spaceID); err != nil {
		return SpaceState{}, err
	}
	if st.Notes, err = s.ListNotes(ctx, spaceID); err != nil {
		return SpaceState{}, err
	}
	if st.Reminders, err = s.ListReminders(ctx, spaceID); err != nil {
		return SpaceState{}, err
	}
	if st.Inbox, err = s.ListInboxItems(ctx, spaceID); err != nil {
		return SpaceState{}, err
	}
	return st, nil
}

// ImportState loads a complete dataset into a space atomically: every
// entity in st is inserted inside a single transaction, so a failure —
// e.g. a broken foreign-key reference midway through the dataset — leaves
// the space database untouched. Projects are inserted first because the
// other entities reference them. Intended for seeding fresh spaces.
func (s *SpaceStore) ImportState(ctx context.Context, spaceID string, st SpaceState) error {
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: import state: begin: %w", err)
	}
	defer rollbackQuietly(tx)
	for _, p := range st.Projects {
		if _, err := s.insertProject(ctx, tx, p); err != nil {
			return err
		}
	}
	for _, a := range st.Activities {
		if _, err := s.insertActivity(ctx, tx, a); err != nil {
			return err
		}
	}
	for _, t := range st.Tasks {
		if _, err := insertTask(ctx, tx, t); err != nil {
			return err
		}
	}
	for _, n := range st.Notes {
		if _, err := insertNote(ctx, tx, n); err != nil {
			return err
		}
	}
	for _, r := range st.Reminders {
		if _, err := insertReminder(ctx, tx, r); err != nil {
			return err
		}
	}
	for _, it := range st.Inbox {
		if _, err := insertInboxItem(ctx, tx, it); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: import state: commit: %w", err)
	}
	return nil
}

// boolPtrToInt maps an optional bool to a nullable INTEGER column value.
func boolPtrToInt(b *bool) *int64 {
	if b == nil {
		return nil
	}
	v := int64(0)
	if *b {
		v = 1
	}
	return &v
}

// intPtrToBool maps a nullable INTEGER column value to an optional bool.
func intPtrToBool(v *int64) *bool {
	if v == nil {
		return nil
	}
	b := *v != 0
	return &b
}
