package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// CoreStore persists the cross-space registry in <data-dir>/core.db:
// users, sessions, and the spaces registry. Space content is out of scope
// here — see SpaceStore.
type CoreStore struct {
	opts options
	db   *sql.DB
}

// NewCoreStore opens (creating and migrating if needed) core.db under the
// configured data directory.
func NewCoreStore(opts ...Option) (*CoreStore, error) {
	o, err := newOptions(opts)
	if err != nil {
		return nil, err
	}
	// 0o700: the data directory holds personal task/project databases and
	// must not be readable by other users on shared hosts.
	if err := os.MkdirAll(o.dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("store: create data dir: %w", err)
	}
	db, err := openDB(filepath.Join(o.dataDir, "core.db"))
	if err != nil {
		return nil, err
	}
	if _, err := migrate(context.Background(), db, coreMigrationFS, "migrations/core", o.now); err != nil {
		closeQuietly(db)
		return nil, err
	}
	return &CoreStore{opts: o, db: db}, nil
}

// Close releases the underlying database handle.
func (s *CoreStore) Close() error {
	return s.db.Close()
}

// CreateUser inserts a new user with an empty password hash (phase 2 sets
// real credentials) and returns the stored row.
func (s *CoreStore) CreateUser(ctx context.Context, username, displayName string) (User, error) {
	if username == "" {
		return User{}, errors.New("store: username is required")
	}
	u := User{
		Username:    username,
		DisplayName: displayName,
		CreatedAt:   s.opts.now(),
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO users (username, display_name, password_hash, created_at) VALUES (?, ?, '', ?)`,
		u.Username, u.DisplayName, u.CreatedAt)
	if err != nil {
		return User{}, fmt.Errorf("store: create user %q: %w", username, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return User{}, fmt.Errorf("store: create user %q: %w", username, err)
	}
	u.ID = id
	return u, nil
}

// DeleteUser removes a user row by id. Rows referencing the user
// (sessions, spaces) must be removed first: foreign keys are enforced.
// Returns ErrNotFound if the id does not exist.
func (s *CoreStore) DeleteUser(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete user %d: %w", id, err)
	}
	return notFoundIfZero(res, "user", strconv.FormatInt(id, 10))
}

// GetUserByUsername returns the user with the given username, or
// ErrNotFound.
func (s *CoreStore) GetUserByUsername(ctx context.Context, username string) (User, error) {
	var u User
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, display_name, password_hash, created_at FROM users WHERE username = ?`,
		username).Scan(&u.ID, &u.Username, &u.DisplayName, &u.PasswordHash, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, fmt.Errorf("store: user %q: %w", username, ErrNotFound)
	}
	if err != nil {
		return User{}, fmt.Errorf("store: get user %q: %w", username, err)
	}
	return u, nil
}

// CreateSpace inserts a registry row for a space. The caller provides ID
// (validated as a file-safe slug), UserID, Name, Color, and Position;
// CreatedAt is set from the store clock. The space's content database is
// created lazily by SpaceStore on first use.
func (s *CoreStore) CreateSpace(ctx context.Context, sp Space) (Space, error) {
	if err := ValidateSpaceID(sp.ID); err != nil {
		return Space{}, err
	}
	if sp.Name == "" {
		return Space{}, errors.New("store: space name is required")
	}
	sp.CreatedAt = s.opts.now()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO spaces (id, user_id, name, color, position, archived_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		sp.ID, sp.UserID, sp.Name, sp.Color, sp.Position, sp.ArchivedAt, sp.CreatedAt)
	if err != nil {
		return Space{}, fmt.Errorf("store: create space %q: %w", sp.ID, err)
	}
	return sp, nil
}

// DeleteSpace removes a space registry row by id. The space's content
// database file, if any, is left on disk. Returns ErrNotFound if the id
// does not exist.
func (s *CoreStore) DeleteSpace(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM spaces WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete space %q: %w", id, err)
	}
	return notFoundIfZero(res, "space", id)
}

// GetSpace returns the registry row for the given space id, or ErrNotFound.
func (s *CoreStore) GetSpace(ctx context.Context, id string) (Space, error) {
	var sp Space
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, name, color, position, archived_at, created_at FROM spaces WHERE id = ?`,
		id).Scan(&sp.ID, &sp.UserID, &sp.Name, &sp.Color, &sp.Position, &sp.ArchivedAt, &sp.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Space{}, fmt.Errorf("store: space %q: %w", id, ErrNotFound)
	}
	if err != nil {
		return Space{}, fmt.Errorf("store: get space %q: %w", id, err)
	}
	return sp, nil
}

// ListSpaces returns all registry rows ordered by position, then id.
func (s *CoreStore) ListSpaces(ctx context.Context) ([]Space, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, name, color, position, archived_at, created_at
		 FROM spaces ORDER BY position, id`)
	if err != nil {
		return nil, fmt.Errorf("store: list spaces: %w", err)
	}
	defer closeQuietly(rows)
	spaces := []Space{}
	for rows.Next() {
		var sp Space
		if err := rows.Scan(&sp.ID, &sp.UserID, &sp.Name, &sp.Color, &sp.Position,
			&sp.ArchivedAt, &sp.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: scan space: %w", err)
		}
		spaces = append(spaces, sp)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list spaces: %w", err)
	}
	return spaces, nil
}
