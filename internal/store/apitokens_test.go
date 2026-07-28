package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

// newTokenUser creates a user to hang API tokens off, on the given store.
func newTokenUser(t *testing.T, s *CoreStore, username string) User {
	t.Helper()
	u, err := s.CreateUser(context.Background(), username, username)
	if err != nil {
		t.Fatalf("create user %s: %v", username, err)
	}
	return u
}

func TestCreateAPITokenValidation(t *testing.T) {
	t.Parallel()
	s := newTestCoreStore(t)
	u := newTokenUser(t, s, "ben")
	base := APIToken{ID: "tok-1", UserID: u.ID, Name: "laptop", TokenHash: "h", TokenPrefix: "dzmcp-ABCDEF", Scope: ScopeReadOnly}
	tests := []struct {
		name    string
		mutate  func(APIToken) APIToken
		wantErr bool
	}{
		{name: "happy", mutate: func(tok APIToken) APIToken { return tok }},
		{name: "missing id", mutate: func(tok APIToken) APIToken { tok.ID = ""; return tok }, wantErr: true},
		{name: "missing user", mutate: func(tok APIToken) APIToken { tok.UserID = 0; return tok }, wantErr: true},
		{name: "missing name", mutate: func(tok APIToken) APIToken { tok.Name = ""; return tok }, wantErr: true},
		{name: "missing hash", mutate: func(tok APIToken) APIToken { tok.TokenHash = ""; return tok }, wantErr: true},
		{name: "missing prefix", mutate: func(tok APIToken) APIToken { tok.TokenPrefix = ""; return tok }, wantErr: true},
		{name: "bad scope", mutate: func(tok APIToken) APIToken { tok.Scope = "admin"; return tok }, wantErr: true},
	}
	for _, tt := range tests {
		tt := tt // capture (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			// Only the happy case reaches INSERT; the rest fail validation
			// first, so a shared id/hash across cases is fine (no collision).
			got, err := s.CreateAPIToken(context.Background(), tt.mutate(base))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("want error, got token %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("CreateAPIToken: %v", err)
			}
			if got.CreatedAt != fixedNow {
				t.Errorf("CreatedAt = %q, want %q", got.CreatedAt, fixedNow)
			}
		})
	}
}

func TestAPITokenLifecycle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestCoreStore(t)
	ben := newTokenUser(t, s, "ben")
	other := newTokenUser(t, s, "other")

	created, err := s.CreateAPIToken(ctx, APIToken{
		ID: "tok-ben", UserID: ben.ID, Name: "laptop",
		TokenHash: "hash-ben", TokenPrefix: "dzmcp-AAAAAA", Scope: ScopeReadWrite,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Listing never carries the hash, and is scoped to the owner.
	listed, err := s.ListAPITokens(ctx, ben.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("list len = %d, want 1", len(listed))
	}
	if listed[0].TokenHash != "" {
		t.Errorf("listing leaked hash %q", listed[0].TokenHash)
	}
	if listed[0].RevokedAt != nil || listed[0].LastUsedAt != nil {
		t.Errorf("fresh token should be active and unused, got %+v", listed[0])
	}
	if empty, err := s.ListAPITokens(ctx, other.ID); err != nil || len(empty) != 0 {
		t.Errorf("other's listing = %v (err %v), want empty", empty, err)
	}

	// Active lookup resolves user + id + scope.
	gotUser, tokenID, scope, err := s.GetUserByAPIToken(ctx, created.TokenHash)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if gotUser.ID != ben.ID || tokenID != "tok-ben" || scope != ScopeReadWrite {
		t.Errorf("lookup = (%d,%q,%q)", gotUser.ID, tokenID, scope)
	}

	// A user cannot revoke another user's token (reads as not found).
	if err := s.RevokeAPIToken(ctx, other.ID, "tok-ben"); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-user revoke err = %v, want ErrNotFound", err)
	}
	if _, _, _, err := s.GetUserByAPIToken(ctx, created.TokenHash); err != nil {
		t.Errorf("token should still be active after failed cross-user revoke: %v", err)
	}

	// Owner revoke is idempotent, and revoked tokens no longer authenticate.
	if err := s.RevokeAPIToken(ctx, ben.ID, "tok-ben"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if err := s.RevokeAPIToken(ctx, ben.ID, "tok-ben"); err != nil {
		t.Fatalf("second revoke should be idempotent: %v", err)
	}
	if _, _, _, err := s.GetUserByAPIToken(ctx, created.TokenHash); !errors.Is(err, ErrNotFound) {
		t.Errorf("revoked token lookup err = %v, want ErrNotFound", err)
	}
	after, err := s.ListAPITokens(ctx, ben.ID)
	if err != nil {
		t.Fatalf("list after revoke: %v", err)
	}
	if after[0].RevokedAt == nil {
		t.Error("revoked token should carry a revokedAt stamp")
	}
}

func TestRevokeAPITokenUnknown(t *testing.T) {
	t.Parallel()
	s := newTestCoreStore(t)
	ben := newTokenUser(t, s, "ben")
	if err := s.RevokeAPIToken(context.Background(), ben.ID, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("revoke unknown err = %v, want ErrNotFound", err)
	}
}

func TestGetUserByAPITokenUnknown(t *testing.T) {
	t.Parallel()
	s := newTestCoreStore(t)
	for _, hash := range []string{"", "nope"} {
		if _, _, _, err := s.GetUserByAPIToken(context.Background(), hash); !errors.Is(err, ErrNotFound) {
			t.Errorf("lookup(%q) err = %v, want ErrNotFound", hash, err)
		}
	}
}

func TestTouchAPITokenLastUsedThrottle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	clock := &advClock{t: fixedClock()}
	s, err := NewCoreStore(WithDataDir(t.TempDir()), WithClock(clock.Now))
	if err != nil {
		t.Fatalf("NewCoreStore: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	ben := newTokenUser(t, s, "ben")
	if _, err := s.CreateAPIToken(ctx, APIToken{
		ID: "tok", UserID: ben.ID, Name: "n", TokenHash: "h", TokenPrefix: "dzmcp-AAAAAA", Scope: ScopeReadOnly,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	lastUsed := func() *string {
		t.Helper()
		toks, err := s.ListAPITokens(ctx, ben.ID)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		return toks[0].LastUsedAt
	}

	if lastUsed() != nil {
		t.Fatal("fresh token should have no last_used_at")
	}
	if err := s.TouchAPITokenLastUsed(ctx, "tok"); err != nil {
		t.Fatalf("first touch: %v", err)
	}
	first := lastUsed()
	if first == nil {
		t.Fatal("first touch should set last_used_at")
	}

	// A second touch within the minute is throttled: no write.
	clock.Advance(30 * time.Second)
	if err := s.TouchAPITokenLastUsed(ctx, "tok"); err != nil {
		t.Fatalf("throttled touch: %v", err)
	}
	if got := lastUsed(); got == nil || *got != *first {
		t.Errorf("last_used_at changed within throttle window: %v -> %v", first, got)
	}

	// Past the minute, it advances.
	clock.Advance(31 * time.Second)
	if err := s.TouchAPITokenLastUsed(ctx, "tok"); err != nil {
		t.Fatalf("post-window touch: %v", err)
	}
	if got := lastUsed(); got == nil || *got == *first {
		t.Errorf("last_used_at should advance past the throttle window, still %v", first)
	}

	// Touching an unknown id is a best-effort no-op, not an error.
	if err := s.TouchAPITokenLastUsed(ctx, "missing"); err != nil {
		t.Errorf("touch unknown id err = %v, want nil", err)
	}
}
