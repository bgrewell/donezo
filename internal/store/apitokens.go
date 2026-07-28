package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// This file implements per-user API token persistence: minting, the
// owner's listing, revocation, the constant-time-ish lookup that
// authenticates an MCP bearer credential, and a throttled last-used
// stamp. The plaintext token never touches the database — only its
// SHA-256 (computed by the auth package) is stored.

// API token scopes gate what a token may do over MCP.
const (
	// ScopeReadOnly permits only the read tools (inspecting the account).
	ScopeReadOnly = "read_only"
	// ScopeReadWrite permits the read tools plus the mutating ones.
	ScopeReadWrite = "read_write"
)

// apiTokenTouchInterval caps how often a token's last_used_at is
// rewritten: at most once per minute, so a chatty LLM does not turn every
// MCP call into a write. It mirrors the session touch interval.
const apiTokenTouchInterval = time.Minute

// APIToken is a row in core.db's api_tokens table. TokenHash is the hex
// SHA-256 of the bearer credential; the token itself is never stored, so
// a leaked database cannot be replayed as a bearer credential. The hash
// is never serialized and is left empty by the listing.
type APIToken struct {
	// ID identifies the token row (an identifier, not a secret).
	ID string `json:"id"`
	// UserID owns the token.
	UserID int64 `json:"userId"`
	// Name is the owner-supplied label for the token.
	Name string `json:"name"`
	// TokenHash is the hex-encoded SHA-256 of the plaintext token, the
	// lookup key for authentication. Never serialized, never listed.
	TokenHash string `json:"-"`
	// TokenPrefix is the first characters of the rendered token, kept so
	// the listing can identify a token without exposing it.
	TokenPrefix string `json:"tokenPrefix"`
	// Scope is ScopeReadOnly or ScopeReadWrite.
	Scope string `json:"scope"`
	// CreatedAt is when the token was minted (RFC 3339 UTC).
	CreatedAt string `json:"createdAt"`
	// LastUsedAt is the most recent authenticated use, nil until the
	// first one is recorded.
	LastUsedAt *string `json:"lastUsedAt,omitempty"`
	// RevokedAt is when the owner revoked the token, nil if still active.
	RevokedAt *string `json:"revokedAt,omitempty"`
}

// validScope reports whether scope is one of the two accepted values.
func validScope(scope string) bool {
	return scope == ScopeReadOnly || scope == ScopeReadWrite
}

// CreateAPIToken inserts a token from tok's identity fields (ID, UserID,
// Name, TokenHash, TokenPrefix, Scope), stamping CreatedAt from the store
// clock, and returns the stored row. The plaintext token is the caller's
// to surface exactly once; it never reaches this layer.
func (s *CoreStore) CreateAPIToken(ctx context.Context, tok APIToken) (APIToken, error) {
	if tok.ID == "" {
		return APIToken{}, errors.New("store: api token id is required")
	}
	if tok.UserID <= 0 {
		return APIToken{}, errors.New("store: api token user id is required")
	}
	if tok.Name == "" {
		return APIToken{}, errors.New("store: api token name is required")
	}
	if tok.TokenHash == "" {
		return APIToken{}, errors.New("store: api token hash is required")
	}
	if tok.TokenPrefix == "" {
		return APIToken{}, errors.New("store: api token prefix is required")
	}
	if !validScope(tok.Scope) {
		return APIToken{}, fmt.Errorf("store: invalid api token scope %q", tok.Scope)
	}
	tok.CreatedAt = s.opts.now()
	tok.LastUsedAt, tok.RevokedAt = nil, nil
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO api_tokens (id, user_id, name, token_hash, token_prefix, scope, created_at, last_used_at, revoked_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, NULL, NULL)`,
		tok.ID, tok.UserID, tok.Name, tok.TokenHash, tok.TokenPrefix, tok.Scope, tok.CreatedAt); err != nil {
		return APIToken{}, fmt.Errorf("store: create api token %q: %w", tok.ID, classifyConstraint(err))
	}
	return tok, nil
}

// ListAPITokens returns userID's tokens, newest first. The token hash is
// deliberately never selected, so it cannot leak through the listing —
// the returned rows carry an empty TokenHash.
func (s *CoreStore) ListAPITokens(ctx context.Context, userID int64) ([]APIToken, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, name, token_prefix, scope, created_at, last_used_at, revoked_at
		 FROM api_tokens WHERE user_id = ? ORDER BY created_at DESC, id`, userID)
	if err != nil {
		return nil, fmt.Errorf("store: list api tokens: %w", err)
	}
	defer closeQuietly(rows)
	tokens := []APIToken{}
	for rows.Next() {
		var t APIToken
		if err := rows.Scan(&t.ID, &t.UserID, &t.Name, &t.TokenPrefix, &t.Scope,
			&t.CreatedAt, &t.LastUsedAt, &t.RevokedAt); err != nil {
			return nil, fmt.Errorf("store: scan api token: %w", err)
		}
		tokens = append(tokens, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list api tokens: %w", err)
	}
	return tokens, nil
}

// RevokeAPIToken stamps the token's revoked_at from the store clock,
// scoped to userID so a caller can only revoke their own tokens. It is
// idempotent: revoking an already-revoked token keeps the original stamp
// and still succeeds. Returns ErrNotFound when userID owns no token with
// that id — so another user's token is indistinguishable from an unknown
// one.
func (s *CoreStore) RevokeAPIToken(ctx context.Context, userID int64, id string) error {
	if id == "" {
		return errors.New("store: api token id is required")
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE api_tokens SET revoked_at = COALESCE(revoked_at, ?) WHERE id = ? AND user_id = ?`,
		s.opts.now(), id, userID)
	if err != nil {
		return fmt.Errorf("store: revoke api token %q: %w", id, err)
	}
	return notFoundIfZero(res, "api token", id)
}

// GetUserByAPIToken resolves an active token's hash to its owning user,
// returning the user, the token id, and the token's scope. Revoked tokens
// are rejected as if they did not exist (the WHERE clause filters them),
// so a leaked-then-revoked token cannot authenticate. Returns ErrNotFound
// when no active token is stored under tokenHash.
//
// The lookup is by hash: a SHA-256 preimage cannot be steered by lookup
// timing, so the database-side match is effectively constant-time in the
// secret — the same property session-token and invite-code lookups rely
// on.
func (s *CoreStore) GetUserByAPIToken(ctx context.Context, tokenHash string) (User, string, string, error) {
	if tokenHash == "" {
		return User{}, "", "", fmt.Errorf("store: api token: %w", ErrNotFound)
	}
	var (
		u       User
		tokenID string
		scope   string
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT t.id, t.scope,
		        u.id, u.username, u.display_name, u.role, u.password_hash, u.created_at
		 FROM api_tokens t JOIN users u ON u.id = t.user_id
		 WHERE t.token_hash = ? AND t.revoked_at IS NULL`, tokenHash).
		Scan(&tokenID, &scope, &u.ID, &u.Username, &u.DisplayName, &u.Role, &u.PasswordHash, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, "", "", fmt.Errorf("store: api token: %w", ErrNotFound)
	}
	if err != nil {
		return User{}, "", "", fmt.Errorf("store: get user by api token: %w", err)
	}
	return u, tokenID, scope, nil
}

// TouchAPITokenLastUsed records that the token was just used, at most once
// per minute: the UPDATE only fires when last_used_at is unset or older
// than a minute, so a chatty client does not turn every MCP call into a
// write. It is best-effort bookkeeping — callers ignore its error — and a
// no-op within the throttle window. RFC 3339 UTC timestamps order
// lexicographically, so the string comparison is a time comparison.
func (s *CoreStore) TouchAPITokenLastUsed(ctx context.Context, id string) error {
	now := s.opts.clock().UTC()
	cutoff := now.Add(-apiTokenTouchInterval).Format(time.RFC3339)
	if _, err := s.db.ExecContext(ctx,
		`UPDATE api_tokens SET last_used_at = ?
		 WHERE id = ? AND (last_used_at IS NULL OR last_used_at <= ?)`,
		now.Format(time.RFC3339), id, cutoff); err != nil {
		return fmt.Errorf("store: touch api token %q: %w", id, err)
	}
	return nil
}
