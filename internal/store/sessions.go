package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Session is a row in core.db's sessions table. TokenHash is the hex
// SHA-256 of the opaque cookie token; the token itself is never stored,
// so a leaked database cannot be replayed as cookies.
type Session struct {
	// TokenHash is the hex-encoded SHA-256 of the session token.
	TokenHash string
	// UserID owns the session.
	UserID int64
	// CreatedAt is when the session was issued (RFC 3339 UTC).
	CreatedAt string
	// ExpiresAt is the absolute expiry (RFC 3339 UTC).
	ExpiresAt string
	// LastSeenAt is the most recent authenticated use, nil until the
	// first one is recorded.
	LastSeenAt *string
}

// CreateSession inserts a session for userID stored under tokenHash,
// expiring ttl after the store clock's now, and returns the stored row.
func (s *CoreStore) CreateSession(ctx context.Context, userID int64, tokenHash string, ttl time.Duration) (Session, error) {
	if tokenHash == "" {
		return Session{}, errors.New("store: session token hash is required")
	}
	if ttl <= 0 {
		return Session{}, fmt.Errorf("store: session ttl must be positive, got %s", ttl)
	}
	now := s.opts.clock().UTC()
	sess := Session{
		TokenHash: tokenHash,
		UserID:    userID,
		CreatedAt: now.Format(time.RFC3339),
		ExpiresAt: now.Add(ttl).Format(time.RFC3339),
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (token_hash, user_id, created_at, expires_at, last_seen_at)
		 VALUES (?, ?, ?, ?, NULL)`,
		sess.TokenHash, sess.UserID, sess.CreatedAt, sess.ExpiresAt); err != nil {
		return Session{}, fmt.Errorf("store: create session for user %d: %w", userID, err)
	}
	return sess, nil
}

// GetSessionUser returns the session stored under tokenHash together
// with its owning user, or ErrNotFound.
func (s *CoreStore) GetSessionUser(ctx context.Context, tokenHash string) (Session, User, error) {
	var sess Session
	var u User
	err := s.db.QueryRowContext(ctx,
		`SELECT s.token_hash, s.user_id, s.created_at, s.expires_at, s.last_seen_at,
		        u.id, u.username, u.display_name, u.role, u.password_hash, u.created_at
		 FROM sessions s JOIN users u ON u.id = s.user_id
		 WHERE s.token_hash = ?`, tokenHash).
		Scan(&sess.TokenHash, &sess.UserID, &sess.CreatedAt, &sess.ExpiresAt, &sess.LastSeenAt,
			&u.ID, &u.Username, &u.DisplayName, &u.Role, &u.PasswordHash, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, User{}, fmt.Errorf("store: session: %w", ErrNotFound)
	}
	if err != nil {
		return Session{}, User{}, fmt.Errorf("store: get session: %w", err)
	}
	return sess, u, nil
}

// TouchSession sets the session's last_seen_at to the store clock's
// now. Returns ErrNotFound if no session is stored under tokenHash.
func (s *CoreStore) TouchSession(ctx context.Context, tokenHash string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET last_seen_at = ? WHERE token_hash = ?`, s.opts.now(), tokenHash)
	if err != nil {
		return fmt.Errorf("store: touch session: %w", err)
	}
	return notFoundIfZero(res, "session", tokenHash)
}

// DeleteSession removes the session stored under tokenHash. Returns
// ErrNotFound if it does not exist.
func (s *CoreStore) DeleteSession(ctx context.Context, tokenHash string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
	if err != nil {
		return fmt.Errorf("store: delete session: %w", err)
	}
	return notFoundIfZero(res, "session", tokenHash)
}

// DeleteExpiredSessions removes sessions whose expiry is at or before
// the store clock's now, returning the count. RFC 3339 UTC timestamps
// order lexicographically, so string comparison is time comparison.
func (s *CoreStore) DeleteExpiredSessions(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, s.opts.now())
	if err != nil {
		return 0, fmt.Errorf("store: delete expired sessions: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: delete expired sessions: %w", err)
	}
	return n, nil
}
