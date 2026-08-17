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

// CreateUser inserts a new member user with an empty password hash
// (phase 2 sets real credentials) and returns the stored row.
func (s *CoreStore) CreateUser(ctx context.Context, username, displayName string) (User, error) {
	if username == "" {
		return User{}, errors.New("store: username is required")
	}
	u := User{
		Username:    username,
		DisplayName: displayName,
		Role:        RoleMember,
		CreatedAt:   s.opts.now(),
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO users (username, display_name, role, password_hash, created_at) VALUES (?, ?, ?, '', ?)`,
		u.Username, u.DisplayName, u.Role, u.CreatedAt)
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
// ErrNotFound. The match is case-insensitive (COLLATE NOCASE), so any casing
// of a username resolves to the one account that owns it — the same folding
// the users.username_nocase unique index enforces on creation.
func (s *CoreStore) GetUserByUsername(ctx context.Context, username string) (User, error) {
	var u User
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, display_name, role, password_hash, created_at FROM users WHERE username = ? COLLATE NOCASE`,
		username).Scan(&u.ID, &u.Username, &u.DisplayName, &u.Role, &u.PasswordHash, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, fmt.Errorf("store: user %q: %w", username, ErrNotFound)
	}
	if err != nil {
		return User{}, fmt.Errorf("store: get user %q: %w", username, err)
	}
	return u, nil
}

// GetUserByID returns the user with the given id, or ErrNotFound.
func (s *CoreStore) GetUserByID(ctx context.Context, id int64) (User, error) {
	var u User
	var email sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, display_name, role, password_hash, created_at, email FROM users WHERE id = ?`,
		id).Scan(&u.ID, &u.Username, &u.DisplayName, &u.Role, &u.PasswordHash, &u.CreatedAt, &email)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, fmt.Errorf("store: user %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return User{}, fmt.Errorf("store: get user %d: %w", id, err)
	}
	if email.Valid {
		e := email.String
		u.Email = &e
	}
	return u, nil
}

// SetUserPassword replaces the stored password hash for the user with
// the given id. The hash must be non-empty (an encoded PHC string, not
// a raw password). Returns ErrNotFound if the id does not exist.
func (s *CoreStore) SetUserPassword(ctx context.Context, id int64, passwordHash string) error {
	if passwordHash == "" {
		return errors.New("store: password hash is required")
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET password_hash = ? WHERE id = ?`, passwordHash, id)
	if err != nil {
		return fmt.Errorf("store: set password for user %d: %w", id, err)
	}
	return notFoundIfZero(res, "user", strconv.FormatInt(id, 10))
}

// SetUserEmail sets (nil clears) a user's recovery email. Returns
// ErrEmailTaken if the address already belongs to another account and
// ErrNotFound if the id does not exist.
func (s *CoreStore) SetUserEmail(ctx context.Context, id int64, email *string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE users SET email = ? WHERE id = ?`, email, id)
	if err != nil {
		if conflict := userUniqueConflict(err); errors.Is(conflict, ErrEmailTaken) {
			return ErrEmailTaken
		}
		return fmt.Errorf("store: set email for user %d: %w", id, err)
	}
	return notFoundIfZero(res, "user", strconv.FormatInt(id, 10))
}

// SetUserDisplayName replaces the display name for the user with the
// given id. Returns ErrNotFound if the id does not exist.
func (s *CoreStore) SetUserDisplayName(ctx context.Context, id int64, displayName string) error {
	if displayName == "" {
		return errors.New("store: display name is required")
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET display_name = ? WHERE id = ?`, displayName, id)
	if err != nil {
		return fmt.Errorf("store: set display name for user %d: %w", id, err)
	}
	return notFoundIfZero(res, "user", strconv.FormatInt(id, 10))
}

// HasCredentialedUser reports whether any user has a password set. It
// is the first-run signal: seeding creates a user with an empty hash,
// which deliberately does not count, so /api/auth/setup stays open
// until a real password exists.
func (s *CoreStore) HasCredentialedUser(ctx context.Context) (bool, error) {
	var n int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM users WHERE password_hash <> ''`).Scan(&n); err != nil {
		return false, fmt.Errorf("store: count credentialed users: %w", err)
	}
	return n > 0, nil
}

// ErrSetupComplete is returned by SetupOwner once any user already has
// a password: first-run setup may only ever succeed once.
var ErrSetupComplete = errors.New("setup already complete")

// noCredentialedUserGuard is the SQL predicate enforcing the first-run
// invariant inside each SetupOwner write. Embedding it in the statement
// makes the check and the write one atomic operation — SQLite executes
// a statement in isolation — so concurrent SetupOwner calls cannot all
// pass a separate Go-level check first: exactly one wins.
const noCredentialedUserGuard = `NOT EXISTS (SELECT 1 FROM users WHERE password_hash <> '')`

// SetupOwner atomically performs first-run setup: while no user has a
// password yet, it gives username one — claiming the seeded
// password-less row in place if the username exists, creating the user
// otherwise — and returns the resulting user. The owner is the instance
// admin, so both paths assign RoleAdmin. Once any user is credentialed
// (including losing a race against a concurrent call) it returns
// ErrSetupComplete and writes nothing.
func (s *CoreStore) SetupOwner(ctx context.Context, username, displayName, passwordHash string, email *string) (User, error) {
	if username == "" {
		return User{}, errors.New("store: username is required")
	}
	if displayName == "" {
		return User{}, errors.New("store: display name is required")
	}
	if passwordHash == "" {
		return User{}, errors.New("store: password hash is required")
	}
	// Claim path: the seeded password-less row, updated in place. NOCASE so a
	// seeded "ben" is claimed by setup as "Ben" rather than falling through to
	// an insert that the case-folding unique index would then reject.
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET password_hash = ?, display_name = ?, role = ?, email = ?
		 WHERE username = ? COLLATE NOCASE AND password_hash = '' AND `+noCredentialedUserGuard,
		passwordHash, displayName, RoleAdmin, email, username)
	if err != nil {
		return User{}, fmt.Errorf("store: setup owner %q: %w", username, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return User{}, fmt.Errorf("store: setup owner %q: %w", username, err)
	}
	if n > 0 {
		u, err := s.GetUserByUsername(ctx, username)
		if err == nil {
			u.Email = email
		}
		return u, err
	}
	// Create path: no claimable row (and possibly no open setup — the
	// guard decides atomically; when it fails the SELECT yields no row
	// and nothing is inserted).
	u := User{
		Username:     username,
		DisplayName:  displayName,
		Role:         RoleAdmin,
		PasswordHash: passwordHash,
		CreatedAt:    s.opts.now(),
		Email:        email,
	}
	res, err = s.db.ExecContext(ctx,
		`INSERT INTO users (username, display_name, role, password_hash, created_at, email)
		 SELECT ?, ?, ?, ?, ?, ? WHERE `+noCredentialedUserGuard,
		u.Username, u.DisplayName, u.Role, u.PasswordHash, u.CreatedAt, u.Email)
	if err != nil {
		return User{}, fmt.Errorf("store: setup owner %q: %w", username, err)
	}
	n, err = res.RowsAffected()
	if err != nil {
		return User{}, fmt.Errorf("store: setup owner %q: %w", username, err)
	}
	if n == 0 {
		return User{}, fmt.Errorf("store: setup owner %q: %w", username, ErrSetupComplete)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return User{}, fmt.Errorf("store: setup owner %q: %w", username, err)
	}
	u.ID = id
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
		return Space{}, fmt.Errorf("store: create space %q: %w", sp.ID, classifyConstraint(err))
	}
	return sp, nil
}

// CreateSpaceAtEnd inserts a registry row for a space like CreateSpace,
// but assigns Position automatically: one past the owner's current
// highest position (0 for the owner's first space). The position is
// computed inside the INSERT itself, so concurrent creates cannot read
// the same maximum. Returns the stored row.
func (s *CoreStore) CreateSpaceAtEnd(ctx context.Context, sp Space) (Space, error) {
	if err := ValidateSpaceID(sp.ID); err != nil {
		return Space{}, err
	}
	if sp.Name == "" {
		return Space{}, errors.New("store: space name is required")
	}
	sp.CreatedAt = s.opts.now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Space{}, fmt.Errorf("store: create space %q: begin: %w", sp.ID, err)
	}
	defer rollbackQuietly(tx)
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO spaces (id, user_id, name, color, position, archived_at, created_at)
		 VALUES (?, ?, ?, ?,
		   (SELECT COALESCE(MAX(position) + 1, 0) FROM spaces WHERE user_id = ?),
		   NULL, ?)`,
		sp.ID, sp.UserID, sp.Name, sp.Color, sp.UserID, sp.CreatedAt); err != nil {
		return Space{}, fmt.Errorf("store: create space %q: %w", sp.ID, classifyConstraint(err))
	}
	stored, err := getSpaceRow(ctx, tx, sp.ID)
	if err != nil {
		return Space{}, err
	}
	if err := tx.Commit(); err != nil {
		return Space{}, fmt.Errorf("store: create space %q: commit: %w", sp.ID, err)
	}
	return stored, nil
}

// PatchSpace atomically applies apply to an existing space registry row
// and rewrites its mutable columns (name, color, position, archived_at).
// ID, UserID, and CreatedAt are identity fields and stay as stored even
// if apply mutates them. The load, mutation, and write run in one
// transaction. Returns ErrNotFound if the id does not exist; a non-nil
// error from apply aborts the patch and is returned unchanged.
func (s *CoreStore) PatchSpace(ctx context.Context, id string, apply func(*Space) error) (Space, error) {
	if id == "" {
		return Space{}, errors.New("store: space id is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Space{}, fmt.Errorf("store: patch space %q: begin: %w", id, err)
	}
	defer rollbackQuietly(tx)
	sp, err := getSpaceRow(ctx, tx, id)
	if err != nil {
		return Space{}, err
	}
	userID, createdAt := sp.UserID, sp.CreatedAt
	if err := apply(&sp); err != nil {
		return Space{}, err
	}
	sp.ID, sp.UserID, sp.CreatedAt = id, userID, createdAt
	if sp.Name == "" {
		return Space{}, errors.New("store: space name is required")
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE spaces SET name = ?, color = ?, position = ?, archived_at = ? WHERE id = ?`,
		sp.Name, sp.Color, sp.Position, sp.ArchivedAt, sp.ID); err != nil {
		return Space{}, fmt.Errorf("store: patch space %q: %w", id, classifyConstraint(err))
	}
	if err := tx.Commit(); err != nil {
		return Space{}, fmt.Errorf("store: patch space %q: commit: %w", id, err)
	}
	return sp, nil
}

// SetSpaceArchived archives (stamping ArchivedAt from the store clock) or
// unarchives (clearing it) a space, returning the stored row. Archiving
// an already-archived space refreshes the stamp. Returns ErrNotFound if
// the id does not exist.
func (s *CoreStore) SetSpaceArchived(ctx context.Context, id string, archived bool) (Space, error) {
	return s.PatchSpace(ctx, id, func(sp *Space) error {
		if archived {
			now := s.opts.now()
			sp.ArchivedAt = &now
		} else {
			sp.ArchivedAt = nil
		}
		return nil
	})
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
	return getSpaceRow(ctx, s.db, id)
}

// getSpaceRow reads one space registry row by id via q, or ErrNotFound.
func getSpaceRow(ctx context.Context, q rowQuerier, id string) (Space, error) {
	var sp Space
	err := q.QueryRowContext(ctx,
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

// ListUsers returns every account, oldest first.
//
// Identity only — never the password hash, which has no caller outside
// authentication and would be one careless handler away from a response
// body. It backs the admin views (#9, #45), where the question is who has
// an account rather than what is in it.
func (s *CoreStore) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, username, display_name, role, created_at FROM users ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("store: list users: %w", err)
	}
	defer closeQuietly(rows)
	users := []User{}
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Role, &u.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: list users: %w", err)
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list users: %w", err)
	}
	return users, nil
}
