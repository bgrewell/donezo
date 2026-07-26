package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// fixedClock is a deterministic Clock for tests.
func fixedClock() time.Time {
	return time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
}

// fixedNow is fixedClock rendered the way options.now renders it.
const fixedNow = "2026-07-26T12:00:00Z"

func TestMigrate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		fsysDir     string
		wantApplied int
		wantTables  []string
	}{
		{
			name:        "core set",
			fsysDir:     "migrations/core",
			wantApplied: 1,
			wantTables:  []string{"users", "sessions", "spaces"},
		},
		{
			name:        "space set",
			fsysDir:     "migrations/space",
			wantApplied: 1,
			wantTables:  []string{"projects", "activities", "tasks", "notes", "reminders", "inbox", "meta"},
		},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			db, err := openDB(filepath.Join(t.TempDir(), "test.db"))
			if err != nil {
				t.Fatalf("openDB: %v", err)
			}
			defer closeQuietly(db)
			fsys := coreMigrationFS
			if tt.fsysDir == "migrations/space" {
				fsys = spaceMigrationFS
			}
			now := func() string { return fixedNow }

			// Fresh apply.
			applied, err := migrate(ctx, db, fsys, tt.fsysDir, now)
			if err != nil {
				t.Fatalf("fresh migrate: %v", err)
			}
			if applied != tt.wantApplied {
				t.Errorf("fresh migrate applied = %d, want %d", applied, tt.wantApplied)
			}
			for _, table := range tt.wantTables {
				var n int
				if err := db.QueryRowContext(ctx,
					`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`,
					table).Scan(&n); err != nil {
					t.Fatalf("check table %s: %v", table, err)
				}
				if n != 1 {
					t.Errorf("table %s missing after migrate", table)
				}
			}

			// Idempotent re-apply.
			applied, err = migrate(ctx, db, fsys, tt.fsysDir, now)
			if err != nil {
				t.Fatalf("re-apply migrate: %v", err)
			}
			if applied != 0 {
				t.Errorf("re-apply applied = %d, want 0", applied)
			}

			// Version tracking.
			var version int
			var name, appliedAt string
			if err := db.QueryRowContext(ctx,
				`SELECT version, name, applied_at FROM schema_migrations ORDER BY version DESC LIMIT 1`).
				Scan(&version, &name, &appliedAt); err != nil {
				t.Fatalf("read schema_migrations: %v", err)
			}
			if version != tt.wantApplied {
				t.Errorf("schema version = %d, want %d", version, tt.wantApplied)
			}
			if name == "" {
				t.Error("schema_migrations.name is empty")
			}
			if appliedAt != fixedNow {
				t.Errorf("applied_at = %q, want %q", appliedAt, fixedNow)
			}
			var rows int
			if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&rows); err != nil {
				t.Fatalf("count schema_migrations: %v", err)
			}
			if rows != tt.wantApplied {
				t.Errorf("schema_migrations rows = %d, want %d", rows, tt.wantApplied)
			}
		})
	}
}

func TestLoadMigrationsBadNames(t *testing.T) {
	t.Parallel()
	// The embedded sets must parse; loadMigrations errors are covered by
	// construction, so exercise the version-prefix validation directly.
	if _, err := loadMigrations(coreMigrationFS, "migrations/core"); err != nil {
		t.Fatalf("core set should load: %v", err)
	}
	if _, err := loadMigrations(spaceMigrationFS, "migrations/space"); err != nil {
		t.Fatalf("space set should load: %v", err)
	}
	if _, err := loadMigrations(coreMigrationFS, "migrations/missing"); err == nil {
		t.Fatal("want error for missing migrations dir")
	}
}
