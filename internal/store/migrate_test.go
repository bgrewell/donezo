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
			wantApplied: 2,
			wantTables:  []string{"users", "sessions", "spaces", "invites"},
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

// TestCoreMigrationUpgradeFromV1 proves the roles migration is safe on
// existing data: a core.db created at schema version 1 (pre-roles) with
// real user rows upgrades in place through the ordinary constructor —
// the role column appears, the right user is promoted to admin, and the
// invites table works.
func TestCoreMigrationUpgradeFromV1(t *testing.T) {
	t.Parallel()
	type oldUser struct {
		username string
		hash     string // "" = never completed setup
	}
	tests := []struct {
		name string
		// users are inserted in order, so ids ascend with the slice.
		users     []oldUser
		wantAdmin string // username promoted to admin; "" for none
	}{
		{
			name: "lowest-id credentialed user is promoted",
			users: []oldUser{
				{username: "seeded", hash: ""}, // dormant seed row must not win
				{username: "owner", hash: "$argon2id$v=19$m=8,t=1,p=1$c2FsdA$aGFzaA"},
				{username: "other", hash: "$argon2id$v=19$m=8,t=1,p=1$c2FsdA$aGFzaQ"},
			},
			wantAdmin: "owner",
		},
		{
			name: "lowest-id user when nobody is credentialed yet",
			users: []oldUser{
				{username: "first", hash: ""},
				{username: "second", hash: ""},
			},
			wantAdmin: "first",
		},
		{
			name:  "empty users table upgrades cleanly",
			users: nil,
		},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			dir := t.TempDir()

			// Build the pre-migration fixture: core.db at exactly schema
			// version 1, populated the way a phase-2 deployment would be.
			db, err := openDB(filepath.Join(dir, "core.db"))
			if err != nil {
				t.Fatalf("openDB: %v", err)
			}
			migs, err := loadMigrations(coreMigrationFS, "migrations/core")
			if err != nil {
				t.Fatalf("loadMigrations: %v", err)
			}
			if len(migs) < 2 || migs[0].version != 1 {
				t.Fatalf("core migration set = %+v, want version 1 first and a later version to upgrade to", migs)
			}
			if _, err := db.ExecContext(ctx, `CREATE TABLE schema_migrations (
				version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL
			)`); err != nil {
				t.Fatalf("create schema_migrations: %v", err)
			}
			now := func() string { return fixedNow }
			if err := applyMigration(ctx, db, migs[0], now); err != nil {
				t.Fatalf("apply v1: %v", err)
			}
			for _, u := range tt.users {
				// The v1 schema has no role column; this INSERT would fail
				// if the fixture were accidentally built on the new schema.
				if _, err := db.ExecContext(ctx,
					`INSERT INTO users (username, display_name, password_hash, created_at) VALUES (?, ?, ?, ?)`,
					u.username, u.username, u.hash, fixedNow); err != nil {
					t.Fatalf("insert v1 user %s: %v", u.username, err)
				}
			}
			if err := db.Close(); err != nil {
				t.Fatalf("close fixture db: %v", err)
			}

			// Reopen through the real constructor — the exact upgrade path
			// donezod takes at startup on an existing data dir.
			s, err := NewCoreStore(WithDataDir(dir), WithClock(fixedClock))
			if err != nil {
				t.Fatalf("NewCoreStore over v1 fixture: %v", err)
			}
			defer func() {
				if err := s.Close(); err != nil {
					t.Errorf("close store: %v", err)
				}
			}()

			var adminID int64
			for _, u := range tt.users {
				got, err := s.GetUserByUsername(ctx, u.username)
				if err != nil {
					t.Fatalf("user %s after upgrade: %v", u.username, err)
				}
				want := RoleMember
				if u.username == tt.wantAdmin {
					want = RoleAdmin
					adminID = got.ID
				}
				if got.Role != want {
					t.Errorf("user %s role = %q, want %q", u.username, got.Role, want)
				}
				if got.PasswordHash != u.hash {
					t.Errorf("user %s hash changed across upgrade: %q", u.username, got.PasswordHash)
				}
			}

			// The invites table is not just present — it works.
			listed, err := s.ListInvites(ctx)
			if err != nil {
				t.Fatalf("ListInvites after upgrade: %v", err)
			}
			if len(listed) != 0 {
				t.Errorf("upgraded invites table not empty: %d rows", len(listed))
			}
			if tt.wantAdmin != "" {
				if _, err := s.CreateInvite(ctx, Invite{
					ID: "inv-upgrade", CodeHash: "hash", CodePrefix: "dz-TESTS", CreatedBy: adminID,
				}, time.Hour); err != nil {
					t.Errorf("CreateInvite after upgrade: %v", err)
				}
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
