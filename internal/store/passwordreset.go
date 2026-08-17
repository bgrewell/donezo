package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// This file is the persistence for password reset: finding the one account a
// reset request resolves to, minting and spending single-use reset tokens, and
// the session invalidation that follows a reset. Only token hashes are stored;
// the plaintext token lives only in the emailed link.

// ErrResetInvalid is returned when a reset token is unknown, already spent, or
// expired — one sentinel, so a caller (and its client) cannot tell those apart
// and a guessed token is not an oracle for which tokens exist.
var ErrResetInvalid = errors.New("reset token invalid")

// FindUserIDForReset resolves a reset request for email to exactly one account,
// or reports that it resolves to none/ambiguous (ok == false). A match is an
// account whose own email is email, or who has a verified email contact at
// email — both compared case-insensitively — restricted to credentialed
// accounts (a password-less, never-set-up row has no password to reset).
//
// It returns a unique match only: if email somehow maps to more than one
// account (e.g. one account's address is another's verified contact), it
// reports ok == false rather than guess which to send to. The caller answers
// the request identically whether ok is true or false, so this is not an
// account-existence oracle.
func (s *CoreStore) FindUserIDForReset(ctx context.Context, email string) (int64, bool, error) {
	ids := make(map[int64]struct{}, 2)

	var accountID int64
	err := s.db.QueryRowContext(ctx,
		`SELECT id FROM users WHERE email = ? COLLATE NOCASE AND password_hash <> ''`, email).Scan(&accountID)
	switch {
	case err == nil:
		ids[accountID] = struct{}{}
	case errors.Is(err, sql.ErrNoRows):
		// no account with that email; contacts may still match
	default:
		return 0, false, fmt.Errorf("store: find reset user: %w", err)
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT c.user_id
		 FROM user_contacts c JOIN users u ON u.id = c.user_id
		 WHERE c.channel = 'email' AND c.verified_at IS NOT NULL
		   AND c.address = ? COLLATE NOCASE AND u.password_hash <> ''`, email)
	if err != nil {
		return 0, false, fmt.Errorf("store: find reset user: %w", err)
	}
	defer closeQuietly(rows)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return 0, false, fmt.Errorf("store: find reset user: %w", err)
		}
		ids[id] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return 0, false, fmt.Errorf("store: find reset user: %w", err)
	}

	if len(ids) != 1 {
		return 0, false, nil
	}
	for id := range ids {
		return id, true, nil
	}
	return 0, false, nil
}

// CreatePasswordReset stores a single-use reset token (by hash) for userID,
// expiring ttl from now. It first clears any earlier resets for the same user,
// so requesting a new link invalidates every older one — only the most recent
// email can be used. Both writes are one transaction.
func (s *CoreStore) CreatePasswordReset(ctx context.Context, userID int64, tokenHash string, ttl time.Duration) error {
	if userID <= 0 {
		return errors.New("store: reset user id is required")
	}
	if tokenHash == "" {
		return errors.New("store: reset token hash is required")
	}
	if ttl <= 0 {
		return fmt.Errorf("store: reset ttl must be positive, got %s", ttl)
	}
	now := s.opts.clock().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: create reset: begin: %w", err)
	}
	defer rollbackQuietly(tx)

	if _, err := tx.ExecContext(ctx, `DELETE FROM password_resets WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("store: create reset: clear old: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO password_resets (token_hash, user_id, created_at, expires_at, used_at)
		 VALUES (?, ?, ?, ?, NULL)`,
		tokenHash, userID, now.Format(time.RFC3339), now.Add(ttl).Format(time.RFC3339)); err != nil {
		return fmt.Errorf("store: create reset: %w", classifyConstraint(err))
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: create reset: commit: %w", err)
	}
	return nil
}

// ConsumePasswordReset spends a reset token and returns the account it belongs
// to. The spend is one guarded UPDATE — used only if the token is unspent and
// unexpired — so two racing redemptions cannot both succeed. An unknown,
// already-used, or expired token yields ErrResetInvalid.
func (s *CoreStore) ConsumePasswordReset(ctx context.Context, tokenHash string) (int64, error) {
	if tokenHash == "" {
		return 0, ErrResetInvalid
	}
	now := s.opts.clock().UTC().Format(time.RFC3339)
	var userID int64
	err := s.db.QueryRowContext(ctx,
		`UPDATE password_resets SET used_at = ?
		 WHERE token_hash = ? AND used_at IS NULL AND expires_at > ?
		 RETURNING user_id`,
		now, tokenHash, now).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrResetInvalid
	}
	if err != nil {
		return 0, fmt.Errorf("store: consume reset: %w", err)
	}
	return userID, nil
}

// DeleteUserSessions removes every session for a user and returns how many.
// Used after a password reset so a thief holding a live session is logged out
// the moment the true owner takes the account back.
func (s *CoreStore) DeleteUserSessions(ctx context.Context, userID int64) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID)
	if err != nil {
		return 0, fmt.Errorf("store: delete user sessions: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: delete user sessions: %w", err)
	}
	return n, nil
}
