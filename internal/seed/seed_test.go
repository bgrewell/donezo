package seed

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bgrewell/donezo/internal/store"
)

// fixedClock keeps seed tests deterministic.
func fixedClock() time.Time {
	return time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
}

// newTestStores builds core and space stores over one temp data dir.
func newTestStores(t *testing.T) (*store.CoreStore, *store.SpaceStore) {
	t.Helper()
	dir := t.TempDir()
	core, err := store.NewCoreStore(store.WithDataDir(dir), store.WithClock(fixedClock))
	if err != nil {
		t.Fatalf("NewCoreStore: %v", err)
	}
	spaces, err := store.NewSpaceStore(store.WithDataDir(dir), store.WithClock(fixedClock))
	if err != nil {
		t.Fatalf("NewSpaceStore: %v", err)
	}
	t.Cleanup(func() {
		if err := core.Close(); err != nil {
			t.Errorf("close core: %v", err)
		}
		if err := spaces.Close(); err != nil {
			t.Errorf("close spaces: %v", err)
		}
	})
	return core, spaces
}

// strPtr returns a pointer to s.
func strPtr(s string) *string {
	return &s
}

// fixtureDataset is a small but complete dataset covering every entity.
func fixtureDataset() Dataset {
	return Dataset{
		Projects: []store.Project{
			{ID: "p1", Name: "One", Color: "blue", Purpose: "p", Outcome: "o",
				CurrentFocus: "cf", NextAction: "na", Status: "active", ResumeContext: "rc",
				Tags: []string{"a"}},
			{ID: "p2", Name: "Two", Color: "green", Purpose: "p", Outcome: "o",
				CurrentFocus: "cf", NextAction: "na", Status: "paused", ResumeContext: "rc"},
		},
		Activities: []store.ActivityEntry{
			{ID: "a1", ProjectID: "p1", Date: "2026-07-01", Type: "work", Title: "t",
				Details: "d", Source: "manual"},
			{ID: "a2", ProjectID: "p2", Date: "2026-07-02", Type: "meeting", Title: "t",
				Details: "d", Source: "capture"},
			{ID: "a3", ProjectID: "p1", Date: "2026-07-03", Type: "decision", Title: "t",
				Details: "d", Source: "manual"},
		},
		Tasks: []store.TaskItem{
			{ID: "t1", ProjectID: strPtr("p1"), Title: "task", Status: "open", CreatedAt: "2026-07-01"},
		},
		Notes: []store.NoteItem{
			{ID: "n1", Title: "note", Body: "b", CreatedAt: "2026-07-01"},
			{ID: "n2", ProjectID: strPtr("p2"), Title: "note2", Body: "b", CreatedAt: "2026-07-02"},
		},
		Reminders: []store.Reminder{
			{ID: "r1", Text: "remind", RemindAt: "2026-07-27T09:00:00"},
		},
		Inbox: []store.InboxItem{
			{ID: "i1", Raw: "raw", CapturedAt: "2026-07-25T08:00:00", SuggestedKind: "task", Status: "pending"},
			{ID: "i2", Raw: "raw2", CapturedAt: "2026-07-25T09:00:00", SuggestedKind: "note", Status: "pending"},
		},
	}
}

func TestImport(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name              string
		dataset           func() Dataset
		preImport         bool // run a successful import first
		wantErr           bool
		wantAlreadySeeded bool // error must wrap ErrAlreadySeeded
		wantCounts        Counts
	}{
		{
			name:       "happy path counts",
			dataset:    fixtureDataset,
			wantCounts: Counts{Projects: 2, Activities: 3, Tasks: 1, Notes: 2, Reminders: 1, Inbox: 2},
		},
		{
			name:       "empty dataset",
			dataset:    func() Dataset { return Dataset{} },
			wantCounts: Counts{},
		},
		{
			name: "activity referencing missing project fails",
			dataset: func() Dataset {
				ds := fixtureDataset()
				ds.Activities = append(ds.Activities, store.ActivityEntry{
					ID: "a-bad", ProjectID: "ghost", Date: "2026-07-04", Type: "work",
					Title: "t", Details: "d", Source: "manual",
				})
				return ds
			},
			wantErr: true,
		},
		{
			name:              "reseed refused with ErrAlreadySeeded",
			dataset:           fixtureDataset,
			preImport:         true,
			wantErr:           true,
			wantAlreadySeeded: true,
		},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			core, spaces := newTestStores(t)
			ctx := context.Background()
			if tt.preImport {
				if _, err := Import(ctx, core, spaces, fixtureDataset()); err != nil {
					t.Fatalf("pre-import: %v", err)
				}
			}
			res, err := Import(ctx, core, spaces, tt.dataset())
			if tt.wantErr {
				if err == nil {
					t.Fatal("want error, got nil")
				}
				if tt.wantAlreadySeeded && !errors.Is(err, ErrAlreadySeeded) {
					t.Errorf("err = %v, want ErrAlreadySeeded", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Import: %v", err)
			}
			if res.Counts != tt.wantCounts {
				t.Errorf("counts = %+v, want %+v", res.Counts, tt.wantCounts)
			}
			if res.Username != Username || res.SpaceID != SpaceID {
				t.Errorf("result identities = %q/%q, want %q/%q", res.Username, res.SpaceID, Username, SpaceID)
			}

			// Registry side effects.
			user, err := core.GetUserByUsername(ctx, Username)
			if err != nil {
				t.Fatalf("seeded user missing: %v", err)
			}
			if user.PasswordHash != "" {
				t.Errorf("seeded PasswordHash = %q, want empty", user.PasswordHash)
			}
			sp, err := core.GetSpace(ctx, SpaceID)
			if err != nil {
				t.Fatalf("seeded space missing: %v", err)
			}
			if sp.UserID != user.ID || sp.Name != SpaceName {
				t.Errorf("space = %+v, want owner %d name %q", sp, user.ID, SpaceName)
			}

			// Space content matches counts.
			state, err := spaces.State(ctx, SpaceID)
			if err != nil {
				t.Fatalf("State: %v", err)
			}
			gotCounts := Counts{
				Projects:   len(state.Projects),
				Activities: len(state.Activities),
				Tasks:      len(state.Tasks),
				Notes:      len(state.Notes),
				Reminders:  len(state.Reminders),
				Inbox:      len(state.Inbox),
			}
			if gotCounts != tt.wantCounts {
				t.Errorf("state counts = %+v, want %+v", gotCounts, tt.wantCounts)
			}
		})
	}
}

// TestImportFailureLeavesDataDirRetryable is the regression test for the
// partial-seed lockout: a failed import must leave neither core rows nor
// space content behind, so a corrected seed file succeeds against the
// same data dir without wiping it.
func TestImportFailureLeavesDataDirRetryable(t *testing.T) {
	t.Parallel()
	core, spaces := newTestStores(t)
	ctx := context.Background()

	poisoned := fixtureDataset()
	poisoned.Activities = append(poisoned.Activities, store.ActivityEntry{
		ID: "a-poison", ProjectID: "ghost", Date: "2026-07-04", Type: "work",
		Title: "t", Details: "d", Source: "manual",
	})
	if _, err := Import(ctx, core, spaces, poisoned); err == nil {
		t.Fatal("poisoned import: want error, got nil")
	}

	// Nothing may remain: core registry rows...
	if _, err := core.GetUserByUsername(ctx, Username); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("user survived failed import: err = %v", err)
	}
	if _, err := core.GetSpace(ctx, SpaceID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("space registry row survived failed import: err = %v", err)
	}
	// ...and space content (the transaction must have rolled back).
	state, err := spaces.State(ctx, SpaceID)
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if n := len(state.Projects) + len(state.Activities) + len(state.Tasks) +
		len(state.Notes) + len(state.Reminders) + len(state.Inbox); n != 0 {
		t.Errorf("space content survived failed import: %d rows", n)
	}

	// The documented recovery path — retry with a corrected file against
	// the SAME data dir — must now succeed.
	res, err := Import(ctx, core, spaces, fixtureDataset())
	if err != nil {
		t.Fatalf("retry after failed import: %v", err)
	}
	want := Counts{Projects: 2, Activities: 3, Tasks: 1, Notes: 2, Reminders: 1, Inbox: 2}
	if res.Counts != want {
		t.Errorf("retry counts = %+v, want %+v", res.Counts, want)
	}
}

func TestIsSeeded(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		seeded bool // run a successful import first
	}{
		{name: "fresh data dir", seeded: false},
		{name: "after successful import", seeded: true},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			core, spaces := newTestStores(t)
			ctx := context.Background()
			if tt.seeded {
				if _, err := Import(ctx, core, spaces, fixtureDataset()); err != nil {
					t.Fatalf("pre-import: %v", err)
				}
			}
			got, err := IsSeeded(ctx, core)
			if err != nil {
				t.Fatalf("IsSeeded: %v", err)
			}
			if got != tt.seeded {
				t.Errorf("IsSeeded = %v, want %v", got, tt.seeded)
			}
		})
	}
}

func TestEnsureDevUser(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		// prepare sets up the registry and returns the user id EnsureDevUser
		// must resolve to, or 0 when any fresh id is acceptable.
		prepare func(t *testing.T, ctx context.Context, core *store.CoreStore, spaces *store.SpaceStore) int64
	}{
		{
			name: "fresh data dir creates the dev user",
			prepare: func(_ *testing.T, _ context.Context, _ *store.CoreStore, _ *store.SpaceStore) int64 {
				return 0
			},
		},
		{
			name: "seeded data dir returns the existing row",
			prepare: func(t *testing.T, ctx context.Context, core *store.CoreStore, spaces *store.SpaceStore) int64 {
				t.Helper()
				if _, err := Import(ctx, core, spaces, fixtureDataset()); err != nil {
					t.Fatalf("pre-import: %v", err)
				}
				user, err := core.GetUserByUsername(ctx, Username)
				if err != nil {
					t.Fatalf("get seeded user: %v", err)
				}
				return user.ID
			},
		},
		{
			name: "dev user id need not be 1",
			prepare: func(t *testing.T, ctx context.Context, core *store.CoreStore, _ *store.SpaceStore) int64 {
				t.Helper()
				// Another user claims id 1 first; EnsureDevUser must still
				// resolve to a real row for the dev username.
				if _, err := core.CreateUser(ctx, "someone-else", "Someone Else"); err != nil {
					t.Fatalf("create other user: %v", err)
				}
				return 0
			},
		},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			core, spaces := newTestStores(t)
			ctx := context.Background()
			wantID := tt.prepare(t, ctx, core, spaces)

			user, err := EnsureDevUser(ctx, core)
			if err != nil {
				t.Fatalf("EnsureDevUser: %v", err)
			}
			if user.Username != Username || user.DisplayName != DisplayName {
				t.Errorf("user = %q/%q, want %q/%q", user.Username, user.DisplayName, Username, DisplayName)
			}
			if wantID != 0 && user.ID != wantID {
				t.Errorf("user.ID = %d, want existing row %d", user.ID, wantID)
			}

			// Idempotent: a second call answers the same row, and the
			// identity satisfies user_id foreign keys (the failure mode
			// EnsureDevUser exists to prevent).
			again, err := EnsureDevUser(ctx, core)
			if err != nil {
				t.Fatalf("EnsureDevUser (second call): %v", err)
			}
			if again.ID != user.ID {
				t.Errorf("second call user.ID = %d, want %d", again.ID, user.ID)
			}
			if _, err := core.CreateSpace(ctx, store.Space{
				ID: "devcheck", UserID: user.ID, Name: "Dev Check", Color: "blue",
			}); err != nil {
				t.Errorf("create space as dev user: %v", err)
			}
		})
	}
}

func TestLoad(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		content string
		missing bool
		wantErr bool
	}{
		{
			name:    "valid file",
			content: `{"projects":[{"id":"p1","name":"One"}],"activities":[],"tasks":[],"notes":[],"reminders":[],"inbox":[]}`,
		},
		{name: "malformed JSON", content: `{"projects":`, wantErr: true},
		{name: "missing file", missing: true, wantErr: true},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "seed.json")
			if !tt.missing {
				if err := os.WriteFile(path, []byte(tt.content), 0o644); err != nil {
					t.Fatalf("write fixture: %v", err)
				}
			}
			ds, err := Load(path)
			if tt.wantErr {
				if err == nil {
					t.Fatal("want error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if len(ds.Projects) != 1 || ds.Projects[0].ID != "p1" {
				t.Errorf("parsed dataset = %+v", ds)
			}
		})
	}
}

func TestLoadRealSeedFile(t *testing.T) {
	t.Parallel()
	// The committed seed file must stay parseable and carry the full mock
	// dataset (7 projects, matching the frontend).
	path := filepath.Join("..", "..", "seed", "seed.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("seed/seed.json not present: %v", err)
	}
	var ds Dataset
	if err := json.Unmarshal(raw, &ds); err != nil {
		t.Fatalf("parse seed/seed.json: %v", err)
	}
	if len(ds.Projects) != 7 {
		t.Errorf("seed projects = %d, want 7", len(ds.Projects))
	}
	for _, coll := range []struct {
		name string
		n    int
	}{
		{"activities", len(ds.Activities)},
		{"tasks", len(ds.Tasks)},
		{"notes", len(ds.Notes)},
		{"reminders", len(ds.Reminders)},
		{"inbox", len(ds.Inbox)},
	} {
		if coll.n == 0 {
			t.Errorf("seed %s is empty", coll.name)
		}
	}
}
