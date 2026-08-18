package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
)

// The known catch-all project — the home for activities logged with no project
// in mind. donezo offers it rather than making every user invent a "Chores"
// project, and keeps "activities are project-bound" intact: an unparented
// activity points at the catch-all instead of at nothing. See migration 0006.
const (
	catchAllName    = "Miscellaneous"
	catchAllColor   = "steel"
	catchAllPurpose = "Small chores and one-off work that doesn't belong to any single thread — where activities logged with no project in mind land."
)

// catchAllDB is the subset of *sql.DB / *sql.Tx the catch-all helpers use.
type catchAllDB interface {
	rowQuerier
	execer
}

// GetOrCreateCatchAll returns the space's known catch-all ("Miscellaneous")
// project, creating it lazily on first use. At most one live catch-all exists
// per space (a partial unique index enforces it), so concurrent callers
// converge on the same row rather than racing two into existence.
func (s *SpaceStore) GetOrCreateCatchAll(ctx context.Context, spaceID string) (Project, error) {
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return Project{}, err
	}
	return s.getOrCreateCatchAll(ctx, db)
}

// getOrCreateCatchAll resolves the catch-all against an open handle so it can
// share a caller's db — e.g. CreateActivity routing an unparented activity
// creates the project and the activity against the same connection.
func (s *SpaceStore) getOrCreateCatchAll(ctx context.Context, db catchAllDB) (Project, error) {
	switch p, err := findCatchAll(ctx, db); {
	case err == nil:
		return p, nil
	case !errors.Is(err, ErrNotFound):
		return Project{}, err
	}
	id, err := randID("proj")
	if err != nil {
		return Project{}, err
	}
	created, err := s.insertProject(ctx, db, Project{
		ID:             id,
		Name:           catchAllName,
		Color:          catchAllColor,
		Purpose:        catchAllPurpose,
		AltNextActions: []string{},
		Status:         "active",
		Tags:           []string{},
		Catchall:       true,
	})
	if err == nil {
		return created, nil
	}
	// Lost the race against a concurrent create: the partial unique index
	// rejected this second insert. Re-read the winner instead of erroring.
	if errors.Is(err, ErrDuplicateID) {
		if p, reErr := findCatchAll(ctx, db); reErr == nil {
			return p, nil
		}
	}
	return Project{}, err
}

// findCatchAll returns the space's live catch-all project, or ErrNotFound when
// the space has never needed one.
func findCatchAll(ctx context.Context, q rowQuerier) (Project, error) {
	p, err := scanProject(q.QueryRowContext(ctx,
		`SELECT `+projectColumns+` FROM projects
		 WHERE catchall = 1 AND deleted_at IS NULL LIMIT 1`))
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	if err != nil {
		return Project{}, fmt.Errorf("store: find catch-all: %w", err)
	}
	return p, nil
}

// randID returns a fresh entity id: a readable prefix plus 8 hex chars. It
// mirrors the MCP id shape so server-minted ids read the same everywhere.
func randID(prefix string) (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("store: generate id: %w", err)
	}
	return fmt.Sprintf("%s-%x", prefix, b), nil
}
