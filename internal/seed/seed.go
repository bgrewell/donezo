// Package seed imports a JSON dataset (generated from the frontend mock
// data by web/scripts/export-seed.mjs) into a donezod data directory.
package seed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/bgrewell/donezo/internal/store"
)

// Dataset is the shape of seed/seed.json: the six space entity
// collections, field-for-field identical to the frontend mock data.
type Dataset struct {
	Projects   []store.Project       `json:"projects"`
	Activities []store.ActivityEntry `json:"activities"`
	Tasks      []store.TaskItem      `json:"tasks"`
	Notes      []store.NoteItem      `json:"notes"`
	Reminders  []store.Reminder      `json:"reminders"`
	Inbox      []store.InboxItem     `json:"inbox"`
}

// Result reports what an Import created.
type Result struct {
	// Username of the created dev user.
	Username string
	// SpaceID of the created space.
	SpaceID string
	// Counts of imported rows per entity.
	Counts Counts
}

// Counts holds imported row counts per entity type.
type Counts struct {
	Projects   int
	Activities int
	Tasks      int
	Notes      int
	Reminders  int
	Inbox      int
}

// Phase 1 fixed identities: seeding creates this user and space. The
// password hash stays empty until phase 2 introduces real credentials.
const (
	// Username of the seeded dev user.
	Username = "ben"
	// DisplayName of the seeded dev user.
	DisplayName = "Ben"
	// SpaceID of the seeded space (also its database file name).
	SpaceID = "sandbox"
	// SpaceName of the seeded space.
	SpaceName = "Sandbox"
	// SpaceColor of the seeded space.
	SpaceColor = "blue"
)

// Load reads and parses a seed dataset from path.
func Load(path string) (Dataset, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Dataset{}, fmt.Errorf("seed: read %s: %w", path, err)
	}
	var ds Dataset
	if err := json.Unmarshal(raw, &ds); err != nil {
		return Dataset{}, fmt.Errorf("seed: parse %s: %w", path, err)
	}
	return ds, nil
}

// ErrAlreadySeeded reports that the data directory already contains the
// seed user, meaning a previous Import completed. Callers seeding on
// startup should treat it as a no-op signal rather than a fatal error.
var ErrAlreadySeeded = errors.New("seed: already seeded")

// IsSeeded reports whether the registry behind core already contains the
// seed user, i.e. whether a previous Import completed.
func IsSeeded(ctx context.Context, core *store.CoreStore) (bool, error) {
	_, err := core.GetUserByUsername(ctx, Username)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, store.ErrNotFound):
		return false, nil
	default:
		return false, err
	}
}

// EnsureDevUser returns the seeded dev user's registry row, creating the
// user (with no password — it cannot log in) when the registry has no
// such row yet. --dev-auto-login uses it so the static dev identity
// always matches a real users row: without one, any write referencing
// user_id (e.g. POST /api/spaces) would fail its foreign key check when
// the server starts on an unseeded data dir.
func EnsureDevUser(ctx context.Context, core *store.CoreStore) (store.User, error) {
	user, err := core.GetUserByUsername(ctx, Username)
	if err == nil {
		return user, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return store.User{}, err
	}
	return core.CreateUser(ctx, Username, DisplayName)
}

// Import creates the dev user and the Sandbox space in core, then loads
// the dataset into the space's database. The space content lands in one
// transaction, and on any failure the core rows created here are removed
// again, so a failed import leaves the data directory retryable with a
// corrected seed file. If the seed user already exists, Import returns an
// error wrapping ErrAlreadySeeded.
func Import(ctx context.Context, core *store.CoreStore, spaces *store.SpaceStore, ds Dataset) (Result, error) {
	seeded, err := IsSeeded(ctx, core)
	if err != nil {
		return Result{}, err
	}
	if seeded {
		return Result{}, fmt.Errorf("%w: user %q exists (use a fresh data dir to reseed)", ErrAlreadySeeded, Username)
	}
	user, err := core.CreateUser(ctx, Username, DisplayName)
	if err != nil {
		return Result{}, err
	}
	if _, err := core.CreateSpace(ctx, store.Space{
		ID:     SpaceID,
		UserID: user.ID,
		Name:   SpaceName,
		Color:  SpaceColor,
	}); err != nil {
		return Result{}, errors.Join(err, cleanupCore(ctx, core, user.ID, false))
	}
	if err := spaces.ImportState(ctx, SpaceID, store.SpaceState{
		Projects:   ds.Projects,
		Activities: ds.Activities,
		Tasks:      ds.Tasks,
		Notes:      ds.Notes,
		Reminders:  ds.Reminders,
		Inbox:      ds.Inbox,
	}); err != nil {
		return Result{}, errors.Join(err, cleanupCore(ctx, core, user.ID, true))
	}
	return Result{
		Username: user.Username,
		SpaceID:  SpaceID,
		Counts: Counts{
			Projects:   len(ds.Projects),
			Activities: len(ds.Activities),
			Tasks:      len(ds.Tasks),
			Notes:      len(ds.Notes),
			Reminders:  len(ds.Reminders),
			Inbox:      len(ds.Inbox),
		},
	}, nil
}

// cleanupCore removes the core rows created by a failed Import (the space
// registry row first: it references the user) so the data directory stays
// retryable. It returns nil when everything was removed.
func cleanupCore(ctx context.Context, core *store.CoreStore, userID int64, spaceCreated bool) error {
	var errs []error
	if spaceCreated {
		if err := core.DeleteSpace(ctx, SpaceID); err != nil {
			errs = append(errs, fmt.Errorf("seed: cleanup space %q: %w", SpaceID, err))
		}
	}
	if err := core.DeleteUser(ctx, userID); err != nil {
		errs = append(errs, fmt.Errorf("seed: cleanup user %q: %w", Username, err))
	}
	return errors.Join(errs...)
}

// SummaryRow is one line of the import summary table.
type SummaryRow struct {
	// Name of the entity collection.
	Name string
	// Count of imported rows.
	Count int
}

// SummaryRows returns the count table rows in display order, for the CLI
// summary.
func (c Counts) SummaryRows() []SummaryRow {
	return []SummaryRow{
		{Name: "projects", Count: c.Projects},
		{Name: "activities", Count: c.Activities},
		{Name: "tasks", Count: c.Tasks},
		{Name: "notes", Count: c.Notes},
		{Name: "reminders", Count: c.Reminders},
		{Name: "inbox", Count: c.Inbox},
	}
}
