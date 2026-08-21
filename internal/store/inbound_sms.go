package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// NewID returns a fresh entity id (a readable prefix plus 8 hex chars) for a
// server-minted row — e.g. an inbox item captured from an inbound text, where
// no client supplies one.
func NewID(prefix string) (string, error) { return randID(prefix) }

// UserForVerifiedContact returns the user who has verified a contact at
// (channel, address) — the identity behind an inbound message from that
// address. It returns ErrNotFound when no verified contact matches, and
// ErrAmbiguousContact when more than one user has verified the same address
// (a shared number): the caller must never guess whose message it is.
func (s *CoreStore) UserForVerifiedContact(ctx context.Context, channel, address string) (User, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT user_id FROM user_contacts
		 WHERE channel = ? AND address = ? AND verified_at IS NOT NULL`, channel, address)
	if err != nil {
		return User{}, fmt.Errorf("store: user for contact: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return User{}, fmt.Errorf("store: user for contact: scan: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return User{}, fmt.Errorf("store: user for contact: %w", err)
	}
	switch len(ids) {
	case 0:
		return User{}, ErrNotFound
	case 1:
		return s.GetUserByID(ctx, ids[0])
	default:
		return User{}, ErrAmbiguousContact
	}
}

// FirstLiveSpace returns a user's first non-archived space by position — the
// default target when an inbound message names no project of its own. Returns
// ErrNotFound when the user has no live space.
func (s *CoreStore) FirstLiveSpace(ctx context.Context, userID int64) (Space, error) {
	var sp Space
	var archivedAt sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, name, color, position, archived_at, created_at
		 FROM spaces WHERE user_id = ? AND archived_at IS NULL
		 ORDER BY position, id LIMIT 1`, userID).
		Scan(&sp.ID, &sp.UserID, &sp.Name, &sp.Color, &sp.Position, &archivedAt, &sp.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Space{}, ErrNotFound
	}
	if err != nil {
		return Space{}, fmt.Errorf("store: first live space: %w", err)
	}
	if archivedAt.Valid {
		sp.ArchivedAt = &archivedAt.String
	}
	return sp, nil
}
