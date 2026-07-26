// Package store implements donezod persistence.
//
// Storage is split across SQLite files: core.db holds the cross-space
// registry (users, sessions, spaces), while each space's content lives in
// its own database file under <data-dir>/spaces/<id>.db. The file is the
// isolation boundary — space tables carry no space_id columns.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"regexp"
	"time"

	// Pure-Go SQLite driver (no cgo); named so constraint failures can be
	// classified by their SQLite error codes.
	sqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// ErrNotFound is returned when a requested row does not exist.
var ErrNotFound = errors.New("not found")

// ErrDuplicateID is returned when an insert collides with an existing
// row's primary key or unique constraint.
var ErrDuplicateID = errors.New("duplicate id")

// ErrInvalidReference is returned when a write names a related row that
// does not exist — a foreign key violation, e.g. an unknown project id.
var ErrInvalidReference = errors.New("invalid reference")

// classifyConstraint maps SQLite constraint violations onto the store's
// sentinel errors so callers can distinguish duplicate ids and broken
// references from other database faults. Any other error passes through
// unchanged.
func classifyConstraint(err error) error {
	var se *sqlite.Error
	if !errors.As(err, &se) {
		return err
	}
	switch se.Code() {
	case sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY, sqlite3.SQLITE_CONSTRAINT_UNIQUE:
		return fmt.Errorf("%w: %v", ErrDuplicateID, err)
	case sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY:
		return fmt.Errorf("%w: %v", ErrInvalidReference, err)
	}
	return err
}

// Clock returns the current time; injectable for deterministic tests.
type Clock func() time.Time

// options holds configuration shared by the store constructors.
type options struct {
	dataDir string
	clock   Clock
}

// Option configures a store constructor (functional options pattern).
type Option func(*options)

// WithDataDir sets the directory that holds core.db and the spaces/
// subdirectory. It is required by both store constructors.
func WithDataDir(dir string) Option {
	return func(o *options) { o.dataDir = dir }
}

// WithClock overrides the time source used for server-generated
// timestamps (created_at / updated_at). Defaults to time.Now.
func WithClock(c Clock) Option {
	return func(o *options) { o.clock = c }
}

// newOptions applies opts over defaults and validates them.
func newOptions(opts []Option) (options, error) {
	o := options{clock: time.Now}
	for _, opt := range opts {
		opt(&o)
	}
	if o.dataDir == "" {
		return options{}, errors.New("store: data dir is required (use WithDataDir)")
	}
	if o.clock == nil {
		return options{}, errors.New("store: clock must not be nil")
	}
	return o, nil
}

// now returns the injected clock's current UTC time in RFC 3339 format,
// the canonical format for server-generated timestamps.
func (o options) now() string {
	return o.clock().UTC().Format(time.RFC3339)
}

// openDB opens (creating if absent) a SQLite database with WAL journaling,
// foreign keys enforced, and a busy timeout, via the pure-Go driver.
func openDB(path string) (*sql.DB, error) {
	dsn := "file:" + path +
		"?_pragma=journal_mode(WAL)" +
		"&_pragma=foreign_keys(1)" +
		"&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	// SQLite serializes writers and gains nothing from database/sql's
	// default unbounded pool: many physical connections against one file
	// only produce SQLITE_BUSY contention (masked by busy_timeout retries)
	// and open/PRAGMA/close churn from the tiny default idle cap. One
	// long-lived connection per database file is the recommended pattern.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.Ping(); err != nil {
		closeQuietly(db)
		return nil, fmt.Errorf("store: ping %s: %w", path, err)
	}
	return db, nil
}

// closeQuietly closes c, discarding the error. Used only for cleanup paths
// (read cursors, already-failed handles) where the close error carries no
// actionable information.
func closeQuietly(c io.Closer) {
	_ = c.Close()
}

// rollbackQuietly rolls back tx, discarding the error: deferred after a
// successful Commit it returns sql.ErrTxDone by design, and on failure
// paths the original error is the actionable one.
func rollbackQuietly(tx *sql.Tx) {
	_ = tx.Rollback()
}

// spaceIDPattern constrains space ids because they become file names on
// disk: lowercase alphanumerics, dash and underscore, 64 chars max.
var spaceIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// ValidateSpaceID reports whether id is safe to use as a space database
// file name.
func ValidateSpaceID(id string) error {
	if !spaceIDPattern.MatchString(id) {
		return fmt.Errorf("store: invalid space id %q (want %s)", id, spaceIDPattern)
	}
	return nil
}
