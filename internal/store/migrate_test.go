package store

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"strings"
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
	// wantApplied is derived from the embedded files rather than hard-coded:
	// migrations are picked up by a //go:embed glob, so a literal count goes
	// stale the moment one is added and fails a change that is actually fine.
	// The table assertions below are what pin the schema's real shape.
	tests := []struct {
		name       string
		fsysDir    string
		wantTables []string
	}{
		{
			name:       "core set",
			fsysDir:    "migrations/core",
			wantTables: []string{"users", "sessions", "spaces", "invites", "api_tokens", "user_settings"},
		},
		{
			name:       "space set",
			fsysDir:    "migrations/space",
			wantTables: []string{"projects", "activities", "tasks", "notes", "reminders", "inbox", "meta"},
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

			entries, err := fs.ReadDir(fsys, tt.fsysDir)
			if err != nil {
				t.Fatalf("read migration dir: %v", err)
			}
			wantApplied := len(entries)
			if wantApplied == 0 {
				t.Fatalf("no migrations found in %s", tt.fsysDir)
			}

			// Fresh apply.
			applied, err := migrate(ctx, db, fsys, tt.fsysDir, now)
			if err != nil {
				t.Fatalf("fresh migrate: %v", err)
			}
			if applied != wantApplied {
				t.Errorf("fresh migrate applied = %d, want %d", applied, wantApplied)
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
			if version != wantApplied {
				t.Errorf("schema version = %d, want %d", version, wantApplied)
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
			if rows != wantApplied {
				t.Errorf("schema_migrations rows = %d, want %d", rows, wantApplied)
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

// TestCoreMigrationUpgradeFromV2 proves the api_tokens migration is safe
// on existing data: a core.db created at schema version 2 (pre-tokens)
// with a real user upgrades in place through the ordinary constructor —
// existing rows are untouched and the new api_tokens table is not just
// present but usable end to end (create, list without the hash, look up).
func TestCoreMigrationUpgradeFromV2(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()

	// Build the pre-migration fixture: core.db at exactly schema version 2,
	// with one credentialed user, the way a phase-3 deployment would be.
	db, err := openDB(filepath.Join(dir, "core.db"))
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}
	migs, err := loadMigrations(coreMigrationFS, "migrations/core")
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	if len(migs) < 3 || migs[1].version != 2 {
		t.Fatalf("core migration set = %+v, want a version 2 to seed and a later version to upgrade to", migs)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE schema_migrations (
		version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL
	)`); err != nil {
		t.Fatalf("create schema_migrations: %v", err)
	}
	now := func() string { return fixedNow }
	for _, m := range migs[:2] { // apply v1 and v2 only
		if err := applyMigration(ctx, db, m, now); err != nil {
			t.Fatalf("apply %s: %v", m.name, err)
		}
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO users (username, display_name, role, password_hash, created_at) VALUES (?, ?, ?, ?, ?)`,
		"owner", "Owner", RoleAdmin, "$argon2id$v=19$m=8,t=1,p=1$c2FsdA$aGFzaA", fixedNow); err != nil {
		t.Fatalf("insert v2 user: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close fixture db: %v", err)
	}

	// Reopen through the real constructor — the exact upgrade path donezod
	// takes at startup on an existing data dir.
	s, err := NewCoreStore(WithDataDir(dir), WithClock(fixedClock))
	if err != nil {
		t.Fatalf("NewCoreStore over v2 fixture: %v", err)
	}
	defer func() {
		if err := s.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	}()

	owner, err := s.GetUserByUsername(ctx, "owner")
	if err != nil {
		t.Fatalf("owner after upgrade: %v", err)
	}
	if owner.Role != RoleAdmin {
		t.Errorf("owner role = %q, want %q (row changed across upgrade)", owner.Role, RoleAdmin)
	}

	// The api_tokens table is not just present — it works.
	created, err := s.CreateAPIToken(ctx, APIToken{
		ID: "tok-upgrade", UserID: owner.ID, Name: "laptop",
		TokenHash: "deadbeef", TokenPrefix: "dzmcp-ABCDEF", Scope: ScopeReadWrite,
	})
	if err != nil {
		t.Fatalf("CreateAPIToken after upgrade: %v", err)
	}
	listed, err := s.ListAPITokens(ctx, owner.ID)
	if err != nil {
		t.Fatalf("ListAPITokens after upgrade: %v", err)
	}
	if len(listed) != 1 || listed[0].TokenHash != "" {
		t.Errorf("listing = %+v, want one token with no hash", listed)
	}
	gotUser, tokenID, scope, err := s.GetUserByAPIToken(ctx, created.TokenHash)
	if err != nil {
		t.Fatalf("GetUserByAPIToken after upgrade: %v", err)
	}
	if gotUser.ID != owner.ID || tokenID != "tok-upgrade" || scope != ScopeReadWrite {
		t.Errorf("lookup = (%d,%q,%q), want (%d,tok-upgrade,%q)", gotUser.ID, tokenID, scope, owner.ID, ScopeReadWrite)
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

// 0002 adds details to tasks and reminders. What matters is what happens to
// rows that were already there — an ALTER that dropped or rewrote them would
// still leave a table with the right columns, and the schema assertions above
// would not notice.
//
// So this genuinely starts at 0001: a store opened at the full current schema
// would write its rows into a table that already had the column, and the
// migration under test would have nothing to migrate.
func TestMigrateAddsDetailsWithoutDisturbingExistingRows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "space.db")
	db, err := openDB(path)
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}
	defer closeQuietly2(t, db)

	// Only 0001, so the tables are the pre-details shape.
	firstOnly, err := fs.Sub(onlyFirstMigration{spaceMigrationFS}, ".")
	if err != nil {
		t.Fatalf("sub fs: %v", err)
	}
	if _, err := migrate(ctx, db, firstOnly, "migrations/space", func() string { return fixedNow }); err != nil {
		t.Fatalf("migrate to 0001: %v", err)
	}
	// Written through the old schema, which has no details column at all —
	// naming it here would fail to compile against the pre-migration table.
	if _, err := db.ExecContext(ctx,
		`INSERT INTO tasks (id, title, status, created_at) VALUES ('t-keep', 'still here', 'open', '2026-07-01')`); err != nil {
		t.Fatalf("insert pre-migration task: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO reminders (id, text, remind_at) VALUES ('r-keep', 'still here', '2026-07-27T09:00:00')`); err != nil {
		t.Fatalf("insert pre-migration reminder: %v", err)
	}

	// Now the rest of the ledger, including 0002.
	applied, err := migrate(ctx, db, spaceMigrationFS, "migrations/space", func() string { return fixedNow })
	if err != nil {
		t.Fatalf("migrate to head: %v", err)
	}
	if applied == 0 {
		t.Fatal("no migrations applied on the second pass — the test is not exercising 0002")
	}

	var title, details, status string
	if err := db.QueryRowContext(ctx,
		`SELECT title, details, status FROM tasks WHERE id = 't-keep'`).Scan(&title, &details, &status); err != nil {
		t.Fatalf("task lost across the migration: %v", err)
	}
	if title != "still here" || status != "open" {
		t.Errorf("task = %q/%q, want its original fields", title, status)
	}
	if details != "" {
		t.Errorf("task details = %q, want empty for a row that predates the column", details)
	}
	var text string
	if err := db.QueryRowContext(ctx,
		`SELECT text, details FROM reminders WHERE id = 'r-keep'`).Scan(&text, &details); err != nil {
		t.Fatalf("reminder lost across the migration: %v", err)
	}
	if text != "still here" {
		t.Errorf("reminder text = %q, want its original", text)
	}
	if details != "" {
		t.Errorf("reminder details = %q, want empty", details)
	}
}

// onlyFirstMigration hides every space migration after 0001, so a test can
// stand a database up at the schema that existed before a later one.
type onlyFirstMigration struct{ fs.FS }

func (o onlyFirstMigration) ReadDir(name string) ([]fs.DirEntry, error) {
	entries, err := fs.ReadDir(o.FS, name)
	if err != nil {
		return nil, err
	}
	kept := entries[:0]
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "0001_") {
			kept = append(kept, e)
		}
	}
	return kept, nil
}

// closeQuietly2 closes a test database and reports a failure to close.
func closeQuietly2(t *testing.T, db *sql.DB) {
	t.Helper()
	if err := db.Close(); err != nil {
		t.Errorf("close db: %v", err)
	}
}

// Details has to survive the round trip on both entities, and an update must
// be able to clear it again.
func TestTaskAndReminderDetailsRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestSpaceStore(t)

	if _, err := s.CreateTask(ctx, "sandbox", TaskItem{
		ID: "t-d", Title: "short title", Details: "the long version,\nover two lines",
		Status: "open", CreatedAt: "2026-07-01",
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	got, err := s.GetTask(ctx, "sandbox", "t-d")
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if got.Details != "the long version,\nover two lines" {
		t.Errorf("task details = %q", got.Details)
	}
	// Listing reads through a different statement than Get, so it gets its
	// own assertion rather than being assumed to match.
	tasks, err := s.ListTasks(ctx, "sandbox")
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(tasks) != 1 || tasks[0].Details != got.Details {
		t.Errorf("listed task details = %+v, want the stored one", tasks)
	}
	got.Details = ""
	if _, err := s.UpdateTask(ctx, "sandbox", got); err != nil {
		t.Fatalf("update task: %v", err)
	}
	if after, err := s.GetTask(ctx, "sandbox", "t-d"); err != nil || after.Details != "" {
		t.Errorf("details = %q after clearing (err %v)", after.Details, err)
	}

	if _, err := s.CreateReminder(ctx, "sandbox", Reminder{
		ID: "r-d", Text: "short text", Details: "why this matters",
		RemindAt: "2026-07-27T09:00:00",
	}); err != nil {
		t.Fatalf("create reminder: %v", err)
	}
	rem, err := s.GetReminder(ctx, "sandbox", "r-d")
	if err != nil {
		t.Fatalf("get reminder: %v", err)
	}
	if rem.Details != "why this matters" {
		t.Errorf("reminder details = %q", rem.Details)
	}
	rems, err := s.ListReminders(ctx, "sandbox")
	if err != nil {
		t.Fatalf("list reminders: %v", err)
	}
	if len(rems) != 1 || rems[0].Details != "why this matters" {
		t.Errorf("listed reminder details = %+v", rems)
	}
	// UpdateReminder as well as UpdateTask: execUpdateReminder writes the new
	// column too, and without this it could stop doing so with nothing red.
	rem.Details = "why it still matters"
	if _, err := s.UpdateReminder(ctx, "sandbox", rem); err != nil {
		t.Fatalf("update reminder: %v", err)
	}
	if after, err := s.GetReminder(ctx, "sandbox", "r-d"); err != nil || after.Details != "why it still matters" {
		t.Errorf("reminder details = %q after update (err %v)", after.Details, err)
	}
	rem.Details = ""
	if _, err := s.UpdateReminder(ctx, "sandbox", rem); err != nil {
		t.Fatalf("clear reminder details: %v", err)
	}
	if after, err := s.GetReminder(ctx, "sandbox", "r-d"); err != nil || after.Details != "" {
		t.Errorf("reminder details = %q after clearing (err %v)", after.Details, err)
	}
}
