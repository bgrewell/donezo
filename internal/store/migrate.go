package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/core/*.sql
var coreMigrationFS embed.FS

//go:embed migrations/space/*.sql
var spaceMigrationFS embed.FS

// migration is one embedded .sql file, named NNNN_description.sql. The
// numeric prefix is the version; files apply in ascending version order.
type migration struct {
	version int
	name    string
	sql     string
}

// loadMigrations reads and orders the migration files under dir in fsys.
func loadMigrations(fsys fs.FS, dir string) ([]migration, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("store: read migrations %s: %w", dir, err)
	}
	migs := make([]migration, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}
		prefix, _, ok := strings.Cut(name, "_")
		if !ok {
			return nil, fmt.Errorf("store: migration %s: want NNNN_name.sql", name)
		}
		version, err := strconv.Atoi(prefix)
		if err != nil {
			return nil, fmt.Errorf("store: migration %s: bad version prefix: %w", name, err)
		}
		body, err := fs.ReadFile(fsys, dir+"/"+name)
		if err != nil {
			return nil, fmt.Errorf("store: read migration %s: %w", name, err)
		}
		migs = append(migs, migration{version: version, name: name, sql: string(body)})
	}
	sort.Slice(migs, func(i, j int) bool { return migs[i].version < migs[j].version })
	for i := 1; i < len(migs); i++ {
		if migs[i].version == migs[i-1].version {
			return nil, fmt.Errorf("store: duplicate migration version %d (%s, %s)",
				migs[i].version, migs[i-1].name, migs[i].name)
		}
	}
	return migs, nil
}

// migrate applies all pending migrations from fsys/dir to db, tracking
// progress in a schema_migrations table inside that same database file.
// It is idempotent: already-applied versions are skipped. It returns the
// number of migrations applied by this call.
func migrate(ctx context.Context, db *sql.DB, fsys fs.FS, dir string, now func() string) (int, error) {
	migs, err := loadMigrations(fsys, dir)
	if err != nil {
		return 0, err
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		name       TEXT NOT NULL,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return 0, fmt.Errorf("store: create schema_migrations: %w", err)
	}
	var current int
	if err := db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&current); err != nil {
		return 0, fmt.Errorf("store: read schema version: %w", err)
	}
	applied := 0
	for _, m := range migs {
		if m.version <= current {
			continue
		}
		if err := applyMigration(ctx, db, m, now); err != nil {
			return applied, err
		}
		applied++
	}
	return applied, nil
}

// applyMigration runs one migration and records it, atomically.
func applyMigration(ctx context.Context, db *sql.DB, m migration, now func() string) (err error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin migration %s: %w", m.name, err)
	}
	defer func() {
		if err != nil {
			// Rollback error is unrecoverable and secondary to err.
			_ = tx.Rollback()
		}
	}()
	if _, err = tx.ExecContext(ctx, m.sql); err != nil {
		return fmt.Errorf("store: apply migration %s: %w", m.name, err)
	}
	if _, err = tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)`,
		m.version, m.name, now()); err != nil {
		return fmt.Errorf("store: record migration %s: %w", m.name, err)
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("store: commit migration %s: %w", m.name, err)
	}
	return nil
}
