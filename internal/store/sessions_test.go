package store

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

// tickingClock is a manually advanced clock for session expiry tests.
type tickingClock struct {
	mu sync.Mutex
	t  time.Time
}

func newTickingClock() *tickingClock {
	return &tickingClock{t: fixedClock()}
}

func (c *tickingClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *tickingClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// newSessionTestStore builds a CoreStore on an advancing clock with one
// user ("ben") created, returning both.
func newSessionTestStore(t *testing.T) (*CoreStore, *tickingClock, User) {
	t.Helper()
	clock := newTickingClock()
	s, err := NewCoreStore(WithDataDir(t.TempDir()), WithClock(clock.Now))
	if err != nil {
		t.Fatalf("NewCoreStore: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("close core store: %v", err)
		}
	})
	u, err := s.CreateUser(context.Background(), "ben", "Ben")
	if err != nil {
		t.Fatalf("setup user: %v", err)
	}
	return s, clock, u
}

func TestCreateSession(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		tokenHash string
		userID    func(u User) int64
		ttl       time.Duration
		wantErr   bool
	}{
		{name: "happy path", tokenHash: "hash-1", userID: func(u User) int64 { return u.ID }, ttl: time.Hour},
		{name: "empty token hash", tokenHash: "", userID: func(u User) int64 { return u.ID }, ttl: time.Hour, wantErr: true},
		{name: "zero ttl", tokenHash: "hash-2", userID: func(u User) int64 { return u.ID }, ttl: 0, wantErr: true},
		{name: "negative ttl", tokenHash: "hash-3", userID: func(u User) int64 { return u.ID }, ttl: -time.Hour, wantErr: true},
		{name: "unknown user violates foreign key", tokenHash: "hash-4", userID: func(User) int64 { return 999 }, ttl: time.Hour, wantErr: true},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s, _, u := newSessionTestStore(t)
			sess, err := s.CreateSession(context.Background(), tt.userID(u), tt.tokenHash, tt.ttl)
			if tt.wantErr {
				if err == nil {
					t.Fatal("want error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("CreateSession: %v", err)
			}
			if sess.CreatedAt != fixedNow {
				t.Errorf("CreatedAt = %q, want %q", sess.CreatedAt, fixedNow)
			}
			wantExpiry := fixedClock().Add(tt.ttl).Format(time.RFC3339)
			if sess.ExpiresAt != wantExpiry {
				t.Errorf("ExpiresAt = %q, want %q", sess.ExpiresAt, wantExpiry)
			}
			if sess.LastSeenAt != nil {
				t.Errorf("LastSeenAt = %v, want nil on creation", *sess.LastSeenAt)
			}
			got, user, err := s.GetSessionUser(context.Background(), tt.tokenHash)
			if err != nil {
				t.Fatalf("GetSessionUser: %v", err)
			}
			if got != sess {
				t.Errorf("round-trip mismatch: got %+v, want %+v", got, sess)
			}
			if user.ID != u.ID || user.Username != "ben" {
				t.Errorf("session user = %+v, want ben (%d)", user, u.ID)
			}
		})
	}
}

func TestGetSessionUserNotFound(t *testing.T) {
	t.Parallel()
	s, _, _ := newSessionTestStore(t)
	if _, _, err := s.GetSessionUser(context.Background(), "ghost"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestTouchSession(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		create       bool
		wantNotFound bool
	}{
		{name: "existing session", create: true},
		{name: "missing session", wantNotFound: true},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s, clock, u := newSessionTestStore(t)
			ctx := context.Background()
			if tt.create {
				if _, err := s.CreateSession(ctx, u.ID, "hash-1", time.Hour); err != nil {
					t.Fatalf("setup session: %v", err)
				}
			}
			clock.Advance(5 * time.Minute)
			err := s.TouchSession(ctx, "hash-1")
			if tt.wantNotFound {
				if !errors.Is(err, ErrNotFound) {
					t.Fatalf("err = %v, want ErrNotFound", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("TouchSession: %v", err)
			}
			sess, _, err := s.GetSessionUser(ctx, "hash-1")
			if err != nil {
				t.Fatalf("GetSessionUser: %v", err)
			}
			want := clock.Now().UTC().Format(time.RFC3339)
			if sess.LastSeenAt == nil || *sess.LastSeenAt != want {
				t.Errorf("LastSeenAt = %v, want %q", sess.LastSeenAt, want)
			}
		})
	}
}

func TestDeleteSession(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		create       bool
		wantNotFound bool
	}{
		{name: "existing session", create: true},
		{name: "missing session", wantNotFound: true},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s, _, u := newSessionTestStore(t)
			ctx := context.Background()
			if tt.create {
				if _, err := s.CreateSession(ctx, u.ID, "hash-1", time.Hour); err != nil {
					t.Fatalf("setup session: %v", err)
				}
			}
			err := s.DeleteSession(ctx, "hash-1")
			if tt.wantNotFound {
				if !errors.Is(err, ErrNotFound) {
					t.Fatalf("err = %v, want ErrNotFound", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("DeleteSession: %v", err)
			}
			if _, _, err := s.GetSessionUser(ctx, "hash-1"); !errors.Is(err, ErrNotFound) {
				t.Errorf("session still present after delete: err = %v", err)
			}
		})
	}
}

func TestDeleteExpiredSessions(t *testing.T) {
	t.Parallel()
	s, clock, u := newSessionTestStore(t)
	ctx := context.Background()
	if _, err := s.CreateSession(ctx, u.ID, "short", time.Hour); err != nil {
		t.Fatalf("setup short session: %v", err)
	}
	if _, err := s.CreateSession(ctx, u.ID, "long", 48*time.Hour); err != nil {
		t.Fatalf("setup long session: %v", err)
	}

	// Nothing has expired yet.
	n, err := s.DeleteExpiredSessions(ctx)
	if err != nil {
		t.Fatalf("DeleteExpiredSessions: %v", err)
	}
	if n != 0 {
		t.Fatalf("deleted %d sessions, want 0 before expiry", n)
	}

	// Boundary: exactly at expiry a session counts as expired
	// (expires_at <= now), matching the authenticator's rejection.
	clock.Advance(time.Hour)
	n, err = s.DeleteExpiredSessions(ctx)
	if err != nil {
		t.Fatalf("DeleteExpiredSessions: %v", err)
	}
	if n != 1 {
		t.Fatalf("deleted %d sessions, want 1 at expiry boundary", n)
	}
	if _, _, err := s.GetSessionUser(ctx, "short"); !errors.Is(err, ErrNotFound) {
		t.Errorf("expired session still present: err = %v", err)
	}
	if _, _, err := s.GetSessionUser(ctx, "long"); err != nil {
		t.Errorf("unexpired session was deleted: %v", err)
	}
}

func TestSetUserPassword(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		id           func(u User) int64
		hash         string
		wantErr      bool
		wantNotFound bool
	}{
		{name: "happy path", id: func(u User) int64 { return u.ID }, hash: "$argon2id$v=19$m=8,t=1,p=1$c2FsdA$aGFzaA"},
		{name: "empty hash rejected", id: func(u User) int64 { return u.ID }, hash: "", wantErr: true},
		{name: "missing user", id: func(User) int64 { return 999 }, hash: "$argon2id$x", wantNotFound: true},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s, _, u := newSessionTestStore(t)
			err := s.SetUserPassword(context.Background(), tt.id(u), tt.hash)
			switch {
			case tt.wantNotFound:
				if !errors.Is(err, ErrNotFound) {
					t.Fatalf("err = %v, want ErrNotFound", err)
				}
			case tt.wantErr:
				if err == nil {
					t.Fatal("want error, got nil")
				}
			default:
				if err != nil {
					t.Fatalf("SetUserPassword: %v", err)
				}
				got, err := s.GetUserByUsername(context.Background(), "ben")
				if err != nil {
					t.Fatalf("GetUserByUsername: %v", err)
				}
				if got.PasswordHash != tt.hash {
					t.Errorf("PasswordHash = %q, want %q", got.PasswordHash, tt.hash)
				}
			}
		})
	}
}

func TestSetUserDisplayName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		id           func(u User) int64
		displayName  string
		wantErr      bool
		wantNotFound bool
	}{
		{name: "happy path", id: func(u User) int64 { return u.ID }, displayName: "Benjamin"},
		{name: "empty name rejected", id: func(u User) int64 { return u.ID }, displayName: "", wantErr: true},
		{name: "missing user", id: func(User) int64 { return 999 }, displayName: "Ghost", wantNotFound: true},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s, _, u := newSessionTestStore(t)
			err := s.SetUserDisplayName(context.Background(), tt.id(u), tt.displayName)
			switch {
			case tt.wantNotFound:
				if !errors.Is(err, ErrNotFound) {
					t.Fatalf("err = %v, want ErrNotFound", err)
				}
			case tt.wantErr:
				if err == nil {
					t.Fatal("want error, got nil")
				}
			default:
				if err != nil {
					t.Fatalf("SetUserDisplayName: %v", err)
				}
				got, err := s.GetUserByUsername(context.Background(), "ben")
				if err != nil {
					t.Fatalf("GetUserByUsername: %v", err)
				}
				if got.DisplayName != tt.displayName {
					t.Errorf("DisplayName = %q, want %q", got.DisplayName, tt.displayName)
				}
			}
		})
	}
}

func TestSetupOwner(t *testing.T) {
	t.Parallel()
	const hash = "$argon2id$v=19$m=8,t=1,p=1$c2FsdA$aGFzaA"
	seedBen := func(t *testing.T, s *CoreStore) User {
		t.Helper()
		u, err := s.CreateUser(context.Background(), "ben", "Ben")
		if err != nil {
			t.Fatalf("seed user: %v", err)
		}
		return u
	}
	tests := []struct {
		name         string
		prep         func(t *testing.T, s *CoreStore)
		username     string
		displayName  string
		hash         string
		wantErr      bool
		wantComplete bool
		check        func(t *testing.T, s *CoreStore, got User)
	}{
		{
			name:        "creates the owner on an empty table",
			username:    "alice",
			displayName: "Alice",
			hash:        hash,
			check: func(t *testing.T, s *CoreStore, got User) {
				t.Helper()
				stored, err := s.GetUserByUsername(context.Background(), "alice")
				if err != nil {
					t.Fatalf("stored user: %v", err)
				}
				if stored != got {
					t.Errorf("returned user %+v != stored row %+v", got, stored)
				}
				if stored.PasswordHash != hash || stored.CreatedAt != fixedNow {
					t.Errorf("stored = %+v, want hash %q at %q", stored, hash, fixedNow)
				}
			},
		},
		{
			name:        "claims the seeded password-less row in place",
			prep:        func(t *testing.T, s *CoreStore) { seedBen(t, s) },
			username:    "ben",
			displayName: "Big Ben",
			hash:        hash,
			check: func(t *testing.T, s *CoreStore, got User) {
				t.Helper()
				if got.ID != 1 {
					t.Errorf("claimed user id = %d, want the seeded row (1), not a new user", got.ID)
				}
				if got.PasswordHash != hash || got.DisplayName != "Big Ben" {
					t.Errorf("claimed user = %+v, want hash and display name updated", got)
				}
			},
		},
		{
			name:        "creates alongside a dormant seeded user",
			prep:        func(t *testing.T, s *CoreStore) { seedBen(t, s) },
			username:    "alice",
			displayName: "Alice",
			hash:        hash,
			check: func(t *testing.T, s *CoreStore, got User) {
				t.Helper()
				if got.Username != "alice" || got.ID == 1 {
					t.Errorf("got %+v, want a fresh row distinct from the seed", got)
				}
			},
		},
		{
			name: "refuses once any user is credentialed",
			prep: func(t *testing.T, s *CoreStore) {
				u := seedBen(t, s)
				if err := s.SetUserPassword(context.Background(), u.ID, hash); err != nil {
					t.Fatalf("credential user: %v", err)
				}
			},
			username:     "mallory",
			displayName:  "M",
			hash:         hash,
			wantComplete: true,
			check: func(t *testing.T, s *CoreStore, _ User) {
				t.Helper()
				if _, err := s.GetUserByUsername(context.Background(), "mallory"); !errors.Is(err, ErrNotFound) {
					t.Errorf("refused setup still wrote a row: err = %v", err)
				}
			},
		},
		{
			name: "refuses to re-claim a credentialed username",
			prep: func(t *testing.T, s *CoreStore) {
				u := seedBen(t, s)
				if err := s.SetUserPassword(context.Background(), u.ID, hash); err != nil {
					t.Fatalf("credential user: %v", err)
				}
			},
			username:     "ben",
			displayName:  "Impostor",
			hash:         "$argon2id$other",
			wantComplete: true,
			check: func(t *testing.T, s *CoreStore, _ User) {
				t.Helper()
				stored, err := s.GetUserByUsername(context.Background(), "ben")
				if err != nil {
					t.Fatalf("stored user: %v", err)
				}
				if stored.PasswordHash != hash || stored.DisplayName != "Ben" {
					t.Errorf("credentialed row was modified: %+v", stored)
				}
			},
		},
		{name: "empty username", username: "", displayName: "X", hash: hash, wantErr: true},
		{name: "empty display name", username: "alice", displayName: "", hash: hash, wantErr: true},
		{name: "empty password hash", username: "alice", displayName: "Alice", hash: "", wantErr: true},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newTestCoreStore(t)
			if tt.prep != nil {
				tt.prep(t, s)
			}
			got, err := s.SetupOwner(context.Background(), tt.username, tt.displayName, tt.hash, nil)
			switch {
			case tt.wantComplete:
				if !errors.Is(err, ErrSetupComplete) {
					t.Fatalf("err = %v, want ErrSetupComplete", err)
				}
			case tt.wantErr:
				if err == nil {
					t.Fatal("want error, got nil")
				}
			default:
				if err != nil {
					t.Fatalf("SetupOwner: %v", err)
				}
			}
			if tt.check != nil {
				tt.check(t, s, got)
			}
		})
	}
}

// TestSetupOwnerConcurrent proves the first-run invariant holds under
// concurrency: racing SetupOwner calls — including one claiming the
// seeded row — produce exactly one owner; everyone else gets
// ErrSetupComplete and writes nothing.
func TestSetupOwnerConcurrent(t *testing.T) {
	t.Parallel()
	s := newTestCoreStore(t)
	ctx := context.Background()
	if _, err := s.CreateUser(ctx, "ben", "Ben"); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	const racers = 16
	errs := make([]error, racers)
	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		i := i // capture (golangci-lint predates Go 1.22 loopvar)
		wg.Add(1)
		go func() {
			defer wg.Done()
			username := fmt.Sprintf("racer%d", i)
			if i == 0 {
				username = "ben" // the claim path races the create path
			}
			_, errs[i] = s.SetupOwner(ctx, username, "Racer", "$argon2id$v=19$m=8,t=1,p=1$c2FsdA$aGFzaA", nil)
		}()
	}
	wg.Wait()
	wins := 0
	for i, err := range errs {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, ErrSetupComplete):
		default:
			t.Errorf("racer %d: unexpected error %v", i, err)
		}
	}
	if wins != 1 {
		t.Fatalf("winners = %d, want exactly 1", wins)
	}
	// The database agrees: exactly one credentialed user.
	var credentialed int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM users WHERE password_hash <> ''`).Scan(&credentialed); err != nil {
		t.Fatalf("count credentialed: %v", err)
	}
	if credentialed != 1 {
		t.Errorf("credentialed users = %d, want exactly 1", credentialed)
	}
}

func TestHasCredentialedUser(t *testing.T) {
	t.Parallel()
	s := newTestCoreStore(t)
	ctx := context.Background()

	// Empty users table: setup needed.
	got, err := s.HasCredentialedUser(ctx)
	if err != nil {
		t.Fatalf("HasCredentialedUser (empty): %v", err)
	}
	if got {
		t.Error("HasCredentialedUser = true with no users")
	}

	// A seeded (password-less) user must not count.
	u, err := s.CreateUser(ctx, "ben", "Ben")
	if err != nil {
		t.Fatalf("setup user: %v", err)
	}
	got, err = s.HasCredentialedUser(ctx)
	if err != nil {
		t.Fatalf("HasCredentialedUser (empty hash): %v", err)
	}
	if got {
		t.Error("HasCredentialedUser = true with only an empty-hash user")
	}

	// A real password flips it.
	if err := s.SetUserPassword(ctx, u.ID, "$argon2id$v=19$m=8,t=1,p=1$c2FsdA$aGFzaA"); err != nil {
		t.Fatalf("SetUserPassword: %v", err)
	}
	got, err = s.HasCredentialedUser(ctx)
	if err != nil {
		t.Fatalf("HasCredentialedUser (credentialed): %v", err)
	}
	if !got {
		t.Error("HasCredentialedUser = false after a password was set")
	}
}
