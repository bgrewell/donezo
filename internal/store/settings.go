package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// This file implements per-user preference persistence. Settings are stored
// as a single JSON document per user (see migrations/core/0004), so adding a
// preference is a change to UserSettings alone — no migration, and no
// per-preference column.
//
// A user who has never saved a preference has no row. That is a normal
// resting state, not an error: GetUserSettings returns the zero value, and
// PatchUserSettings creates the row on first write.

// UserSettings is one user's stored preferences.
//
// Every field is optional and omitted from the stored JSON when empty, so an
// unset preference stays unset rather than being pinned to whatever the
// current default happens to be — a default can change later and previously
// untouched preferences will follow it.
//
// Values are validated at the API boundary against the same enums the web UI
// uses (see internal/api/validate.go); the store keeps them opaque.
type UserSettings struct {
	// Theme is the selected color theme id (see web/src/lib/themes.ts).
	Theme string `json:"theme,omitempty"`
	// Font is the selected font-set id.
	Font string `json:"font,omitempty"`
	// FontSize is the selected text-size id.
	FontSize string `json:"fontSize,omitempty"`
}

// GetUserSettings returns a user's stored preferences. A user with no stored
// preferences returns the zero value and a nil error — absence is normal.
func (s *CoreStore) GetUserSettings(ctx context.Context, userID int64) (UserSettings, error) {
	var raw string
	err := s.db.QueryRowContext(ctx,
		`SELECT settings FROM user_settings WHERE user_id = ?`, userID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return UserSettings{}, nil
	}
	if err != nil {
		return UserSettings{}, fmt.Errorf("store: get user settings %d: %w", userID, err)
	}
	return decodeUserSettings(raw, userID)
}

// decodeUserSettings unmarshals a stored settings document.
func decodeUserSettings(raw string, userID int64) (UserSettings, error) {
	var out UserSettings
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return UserSettings{}, fmt.Errorf("store: decode user settings %d: %w", userID, err)
	}
	return out, nil
}

// PatchUserSettings atomically applies apply to a user's settings and writes
// them back, creating the row on first write. The load, mutation, and write
// run in one transaction, so concurrent patches of different preferences
// serialize instead of clobbering each other — the same guarantee the space
// store's Patch* methods give.
//
// A non-nil error from apply aborts the patch and is returned unchanged.
// Patching a user that does not exist returns ErrNotFound rather than
// creating an orphan row.
func (s *CoreStore) PatchUserSettings(ctx context.Context, userID int64, apply func(*UserSettings) error) (UserSettings, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return UserSettings{}, fmt.Errorf("store: patch user settings %d: begin: %w", userID, err)
	}
	defer rollbackQuietly(tx)

	// The foreign key would catch this at write time, but only as an opaque
	// constraint error; check first so an unknown user reports as ErrNotFound.
	var exists int
	switch err := tx.QueryRowContext(ctx, `SELECT 1 FROM users WHERE id = ?`, userID).Scan(&exists); {
	case errors.Is(err, sql.ErrNoRows):
		return UserSettings{}, fmt.Errorf("store: user %d: %w", userID, ErrNotFound)
	case err != nil:
		return UserSettings{}, fmt.Errorf("store: patch user settings %d: %w", userID, err)
	}

	var raw string
	err = tx.QueryRowContext(ctx,
		`SELECT settings FROM user_settings WHERE user_id = ?`, userID).Scan(&raw)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		raw = "{}"
	case err != nil:
		return UserSettings{}, fmt.Errorf("store: patch user settings %d: %w", userID, err)
	}
	settings, err := decodeUserSettings(raw, userID)
	if err != nil {
		return UserSettings{}, err
	}
	if err := apply(&settings); err != nil {
		return UserSettings{}, err
	}
	encoded, err := json.Marshal(settings)
	if err != nil {
		return UserSettings{}, fmt.Errorf("store: encode user settings %d: %w", userID, err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO user_settings (user_id, settings, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT (user_id) DO UPDATE SET settings = excluded.settings, updated_at = excluded.updated_at`,
		userID, string(encoded), s.opts.now()); err != nil {
		return UserSettings{}, fmt.Errorf("store: patch user settings %d: %w", userID, err)
	}
	if err := tx.Commit(); err != nil {
		return UserSettings{}, fmt.Errorf("store: patch user settings %d: commit: %w", userID, err)
	}
	return settings, nil
}
