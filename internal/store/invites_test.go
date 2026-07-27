package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// advClock is a manually advanced clock for invite-expiry tests.
type advClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *advClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *advClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// newInviteStore builds a CoreStore on an advanceable clock, with a
// credentialed admin to hang invites off. Returns the store, the clock,
// and the admin.
func newInviteStore(t *testing.T) (*CoreStore, *advClock, User) {
	t.Helper()
	clock := &advClock{t: fixedClock()}
	s, err := NewCoreStore(WithDataDir(t.TempDir()), WithClock(clock.Now))
	if err != nil {
		t.Fatalf("NewCoreStore: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("close core store: %v", err)
		}
	})
	admin, err := s.SetupOwner(context.Background(), "admin", "Admin", "$argon2id$v=19$m=8,t=1,p=1$c2FsdA$aGFzaA")
	if err != nil {
		t.Fatalf("setup owner: %v", err)
	}
	return s, clock, admin
}

// mintInvite creates an invite with a distinct hash/prefix per code tag.
func mintInvite(t *testing.T, s *CoreStore, adminID int64, tag string, ttl time.Duration) Invite {
	t.Helper()
	inv, err := s.CreateInvite(context.Background(), Invite{
		ID:         "inv-" + tag,
		CodeHash:   "hash-" + tag,
		CodePrefix: "dz-" + tag,
		CreatedBy:  adminID,
	}, ttl)
	if err != nil {
		t.Fatalf("create invite %s: %v", tag, err)
	}
	return inv
}

// registerArgs builds a valid RegisterInvitedUser user/space pair.
func registerArgs(username string) (User, Space) {
	return User{Username: username, DisplayName: username, PasswordHash: "fake$pw"},
		Space{ID: username + "-main", Name: "main", Color: "blue"}
}

func TestCreateInvite(t *testing.T) {
	t.Parallel()
	base := Invite{ID: "inv-1", CodeHash: "hash-1", CodePrefix: "dz-ABCDE", CreatedBy: 1}
	tests := []struct {
		name    string
		mutate  func(*Invite)
		ttl     time.Duration
		prep    func(t *testing.T, s *CoreStore, adminID int64)
		wantErr bool
	}{
		{name: "happy path", ttl: 7 * 24 * time.Hour},
		{name: "missing id", mutate: func(i *Invite) { i.ID = "" }, ttl: time.Hour, wantErr: true},
		{name: "missing code hash", mutate: func(i *Invite) { i.CodeHash = "" }, ttl: time.Hour, wantErr: true},
		{name: "missing code prefix", mutate: func(i *Invite) { i.CodePrefix = "" }, ttl: time.Hour, wantErr: true},
		{name: "missing creator", mutate: func(i *Invite) { i.CreatedBy = 0 }, ttl: time.Hour, wantErr: true},
		{name: "unknown creator", mutate: func(i *Invite) { i.CreatedBy = 999 }, ttl: time.Hour, wantErr: true},
		{name: "zero ttl", ttl: 0, wantErr: true},
		{name: "negative ttl", ttl: -time.Hour, wantErr: true},
		{
			name: "duplicate id",
			ttl:  time.Hour,
			prep: func(t *testing.T, s *CoreStore, adminID int64) {
				t.Helper()
				mintInvite(t, s, adminID, "dup", time.Hour)
			},
			mutate:  func(i *Invite) { i.ID = "inv-dup" },
			wantErr: true,
		},
		{
			name: "duplicate code hash",
			ttl:  time.Hour,
			prep: func(t *testing.T, s *CoreStore, adminID int64) {
				t.Helper()
				mintInvite(t, s, adminID, "dup", time.Hour)
			},
			mutate:  func(i *Invite) { i.CodeHash = "hash-dup" },
			wantErr: true,
		},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s, _, admin := newInviteStore(t)
			if tt.prep != nil {
				tt.prep(t, s, admin.ID)
			}
			inv := base
			if inv.CreatedBy == 1 {
				inv.CreatedBy = admin.ID
			}
			if tt.mutate != nil {
				tt.mutate(&inv)
			}
			got, err := s.CreateInvite(context.Background(), inv, tt.ttl)
			if tt.wantErr {
				if err == nil {
					t.Fatal("want error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("CreateInvite: %v", err)
			}
			if got.CreatedAt != fixedNow {
				t.Errorf("CreatedAt = %q, want %q", got.CreatedAt, fixedNow)
			}
			wantExpiry := fixedClock().Add(tt.ttl).UTC().Format(time.RFC3339)
			if got.ExpiresAt != wantExpiry {
				t.Errorf("ExpiresAt = %q, want %q", got.ExpiresAt, wantExpiry)
			}
			if got.UsedBy != nil || got.UsedAt != nil || got.RevokedAt != nil {
				t.Errorf("fresh invite carries claim/revocation state: %+v", got)
			}
		})
	}
}

func TestListInvites(t *testing.T) {
	t.Parallel()
	s, _, admin := newInviteStore(t)
	ctx := context.Background()

	empty, err := s.ListInvites(ctx)
	if err != nil {
		t.Fatalf("ListInvites (empty): %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("empty list length = %d, want 0", len(empty))
	}

	mintInvite(t, s, admin.ID, "open", 24*time.Hour)
	used := mintInvite(t, s, admin.ID, "used", 24*time.Hour)
	u, sp := registerArgs("nina")
	if _, _, err := s.RegisterInvitedUser(ctx, used.CodeHash, u, sp); err != nil {
		t.Fatalf("register: %v", err)
	}

	got, err := s.ListInvites(ctx)
	if err != nil {
		t.Fatalf("ListInvites: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("list length = %d, want 2", len(got))
	}
	byID := map[string]InviteListing{}
	for _, l := range got {
		byID[l.ID] = l
		if l.CreatorUsername != "admin" {
			t.Errorf("invite %s creator = %q, want admin", l.ID, l.CreatorUsername)
		}
	}
	open := byID["inv-open"]
	if open.UsedBy != nil || open.UsedByUsername != nil || open.UsedAt != nil || open.RevokedAt != nil {
		t.Errorf("unclaimed invite carries claim state: %+v", open)
	}
	claimed := byID["inv-used"]
	if claimed.UsedByUsername == nil || *claimed.UsedByUsername != "nina" {
		t.Errorf("claimed invite UsedByUsername = %v, want nina", claimed.UsedByUsername)
	}
	if claimed.UsedAt == nil || *claimed.UsedAt != fixedNow {
		t.Errorf("claimed invite UsedAt = %v, want %q", claimed.UsedAt, fixedNow)
	}
	if claimed.CodePrefix != "dz-used" {
		t.Errorf("CodePrefix = %q, want dz-used", claimed.CodePrefix)
	}
}

func TestRevokeInvite(t *testing.T) {
	t.Parallel()
	s, clock, admin := newInviteStore(t)
	ctx := context.Background()
	mintInvite(t, s, admin.ID, "r", 24*time.Hour)

	if err := s.RevokeInvite(ctx, "inv-r"); err != nil {
		t.Fatalf("RevokeInvite: %v", err)
	}
	stamp := func() string {
		t.Helper()
		listed, err := s.ListInvites(ctx)
		if err != nil {
			t.Fatalf("ListInvites: %v", err)
		}
		if len(listed) != 1 || listed[0].RevokedAt == nil {
			t.Fatalf("revoked invite missing stamp: %+v", listed)
		}
		return *listed[0].RevokedAt
	}
	first := stamp()
	if first != fixedNow {
		t.Errorf("RevokedAt = %q, want %q", first, fixedNow)
	}

	// Idempotent: a later second revoke succeeds and keeps the stamp.
	clock.Advance(time.Hour)
	if err := s.RevokeInvite(ctx, "inv-r"); err != nil {
		t.Fatalf("second RevokeInvite: %v", err)
	}
	if again := stamp(); again != first {
		t.Errorf("second revoke moved the stamp: %q -> %q", first, again)
	}

	if err := s.RevokeInvite(ctx, "inv-ghost"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown id err = %v, want ErrNotFound", err)
	}
	if err := s.RevokeInvite(ctx, ""); err == nil {
		t.Error("empty id: want error, got nil")
	}
}

func TestRegisterInvitedUser(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		// prep returns the code hash to register with.
		prep     func(t *testing.T, s *CoreStore, clock *advClock, adminID int64) string
		username string
		mutate   func(u *User, sp *Space)
		wantErr  error // sentinel via errors.Is; nil means success
		anyErr   bool  // plain validation error
	}{
		{
			name: "happy path",
			prep: func(t *testing.T, s *CoreStore, _ *advClock, adminID int64) string {
				t.Helper()
				return mintInvite(t, s, adminID, "ok", 24*time.Hour).CodeHash
			},
			username: "nina",
		},
		{
			name:     "unknown code",
			prep:     func(*testing.T, *CoreStore, *advClock, int64) string { return "hash-ghost" },
			username: "nina",
			wantErr:  ErrInviteInvalid,
		},
		{
			name: "used code",
			prep: func(t *testing.T, s *CoreStore, _ *advClock, adminID int64) string {
				t.Helper()
				inv := mintInvite(t, s, adminID, "used", 24*time.Hour)
				u, sp := registerArgs("first")
				if _, _, err := s.RegisterInvitedUser(context.Background(), inv.CodeHash, u, sp); err != nil {
					t.Fatalf("first register: %v", err)
				}
				return inv.CodeHash
			},
			username: "second",
			wantErr:  ErrInviteInvalid,
		},
		{
			name: "revoked code",
			prep: func(t *testing.T, s *CoreStore, _ *advClock, adminID int64) string {
				t.Helper()
				inv := mintInvite(t, s, adminID, "rev", 24*time.Hour)
				if err := s.RevokeInvite(context.Background(), inv.ID); err != nil {
					t.Fatalf("revoke: %v", err)
				}
				return inv.CodeHash
			},
			username: "nina",
			wantErr:  ErrInviteInvalid,
		},
		{
			name: "expired code",
			prep: func(t *testing.T, s *CoreStore, clock *advClock, adminID int64) string {
				t.Helper()
				inv := mintInvite(t, s, adminID, "exp", time.Hour)
				clock.Advance(2 * time.Hour)
				return inv.CodeHash
			},
			username: "nina",
			wantErr:  ErrInviteInvalid,
		},
		{
			name: "username taken",
			prep: func(t *testing.T, s *CoreStore, _ *advClock, adminID int64) string {
				t.Helper()
				return mintInvite(t, s, adminID, "taken", 24*time.Hour).CodeHash
			},
			username: "admin", // the owner already holds it
			wantErr:  ErrUsernameTaken,
		},
		{
			name: "empty username",
			prep: func(t *testing.T, s *CoreStore, _ *advClock, adminID int64) string {
				t.Helper()
				return mintInvite(t, s, adminID, "v1", 24*time.Hour).CodeHash
			},
			username: "nina",
			mutate:   func(u *User, _ *Space) { u.Username = "" },
			anyErr:   true,
		},
		{
			name: "empty password hash",
			prep: func(t *testing.T, s *CoreStore, _ *advClock, adminID int64) string {
				t.Helper()
				return mintInvite(t, s, adminID, "v2", 24*time.Hour).CodeHash
			},
			username: "nina",
			mutate:   func(u *User, _ *Space) { u.PasswordHash = "" },
			anyErr:   true,
		},
		{
			name: "invalid space id",
			prep: func(t *testing.T, s *CoreStore, _ *advClock, adminID int64) string {
				t.Helper()
				return mintInvite(t, s, adminID, "v3", 24*time.Hour).CodeHash
			},
			username: "nina",
			mutate:   func(_ *User, sp *Space) { sp.ID = "../evil" },
			anyErr:   true,
		},
		{
			name: "duplicate space id",
			prep: func(t *testing.T, s *CoreStore, _ *advClock, adminID int64) string {
				t.Helper()
				if _, err := s.CreateSpace(context.Background(), Space{
					ID: "nina-main", UserID: adminID, Name: "Clash", Color: "blue",
				}); err != nil {
					t.Fatalf("setup clashing space: %v", err)
				}
				return mintInvite(t, s, adminID, "v4", 24*time.Hour).CodeHash
			},
			username: "nina",
			wantErr:  ErrDuplicateID,
		},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s, clock, admin := newInviteStore(t)
			ctx := context.Background()
			codeHash := tt.prep(t, s, clock, admin.ID)
			u, sp := registerArgs(tt.username)
			if tt.mutate != nil {
				tt.mutate(&u, &sp)
			}
			gotUser, gotSpace, err := s.RegisterInvitedUser(ctx, codeHash, u, sp)
			switch {
			case tt.wantErr != nil:
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				return
			case tt.anyErr:
				if err == nil {
					t.Fatal("want error, got nil")
				}
				return
			case err != nil:
				t.Fatalf("RegisterInvitedUser: %v", err)
			}
			if gotUser.Role != RoleMember {
				t.Errorf("role = %q, want member", gotUser.Role)
			}
			stored, err := s.GetUserByUsername(ctx, tt.username)
			if err != nil {
				t.Fatalf("stored user: %v", err)
			}
			if stored != gotUser {
				t.Errorf("returned user %+v != stored %+v", gotUser, stored)
			}
			storedSpace, err := s.GetSpace(ctx, sp.ID)
			if err != nil {
				t.Fatalf("stored space: %v", err)
			}
			if storedSpace != gotSpace {
				t.Errorf("returned space %+v != stored %+v", gotSpace, storedSpace)
			}
			if storedSpace.UserID != gotUser.ID || storedSpace.Position != 0 {
				t.Errorf("space = %+v, want owned by %d at position 0", storedSpace, gotUser.ID)
			}
		})
	}
}

// TestRegisterInvitedUserRollback proves the claim rolls back with the
// rest of the transaction: a register that dies on a taken username
// must leave the invite claimable, and the retry must succeed.
func TestRegisterInvitedUserRollback(t *testing.T) {
	t.Parallel()
	s, _, admin := newInviteStore(t)
	ctx := context.Background()
	inv := mintInvite(t, s, admin.ID, "rb", 24*time.Hour)

	u, sp := registerArgs("admin") // collides with the owner
	if _, _, err := s.RegisterInvitedUser(ctx, inv.CodeHash, u, sp); !errors.Is(err, ErrUsernameTaken) {
		t.Fatalf("err = %v, want ErrUsernameTaken", err)
	}
	listed, err := s.ListInvites(ctx)
	if err != nil {
		t.Fatalf("ListInvites: %v", err)
	}
	if len(listed) != 1 || listed[0].UsedAt != nil || listed[0].UsedBy != nil {
		t.Fatalf("failed register left the invite claimed: %+v", listed)
	}

	u, sp = registerArgs("nina")
	if _, _, err := s.RegisterInvitedUser(ctx, inv.CodeHash, u, sp); err != nil {
		t.Fatalf("retry after rollback: %v", err)
	}
}

// TestUnregisterInvitedUser covers the compensation path: unwinding a
// committed registration must atomically release the claim AND remove
// the account — never one without the other, or a single invite could
// mint two accounts.
func TestUnregisterInvitedUser(t *testing.T) {
	t.Parallel()

	// registered claims a fresh invite as username and returns the
	// invite plus the created user and space.
	registered := func(t *testing.T, s *CoreStore, adminID int64, tag, username string) (Invite, User, Space) {
		t.Helper()
		inv := mintInvite(t, s, adminID, tag, 24*time.Hour)
		u, sp := registerArgs(username)
		gotUser, gotSpace, err := s.RegisterInvitedUser(context.Background(), inv.CodeHash, u, sp)
		if err != nil {
			t.Fatalf("register %s: %v", username, err)
		}
		return inv, gotUser, gotSpace
	}
	// claimed reports whether the store's single invite is claimed
	// (carries used_at or used_by).
	claimed := func(t *testing.T, s *CoreStore) bool {
		t.Helper()
		listed, err := s.ListInvites(context.Background())
		if err != nil {
			t.Fatalf("ListInvites: %v", err)
		}
		if len(listed) != 1 {
			t.Fatalf("invite count = %d, want 1", len(listed))
		}
		return listed[0].UsedAt != nil || listed[0].UsedBy != nil
	}

	t.Run("happy path unwinds everything and frees the code", func(t *testing.T) {
		t.Parallel()
		s, _, admin := newInviteStore(t)
		ctx := context.Background()
		inv, user, sp := registered(t, s, admin.ID, "rel", "nina")

		if err := s.UnregisterInvitedUser(ctx, inv.CodeHash, user.ID, sp.ID); err != nil {
			t.Fatalf("UnregisterInvitedUser: %v", err)
		}
		if claimed(t, s) {
			t.Error("unregistered invite still claimed")
		}
		if _, err := s.GetUserByUsername(ctx, "nina"); !errors.Is(err, ErrNotFound) {
			t.Errorf("user after unregister: err = %v, want ErrNotFound", err)
		}
		if _, err := s.GetSpace(ctx, sp.ID); !errors.Is(err, ErrNotFound) {
			t.Errorf("space after unregister: err = %v, want ErrNotFound", err)
		}
		// The code — and even the username — are claimable again.
		u2, sp2 := registerArgs("nina")
		if _, _, err := s.RegisterInvitedUser(ctx, inv.CodeHash, u2, sp2); err != nil {
			t.Fatalf("register after unregister: %v", err)
		}
	})

	t.Run("mismatched code hash rolls everything back", func(t *testing.T) {
		t.Parallel()
		s, _, admin := newInviteStore(t)
		ctx := context.Background()
		inv, user, sp := registered(t, s, admin.ID, "mm", "nina")

		// The claim-release UPDATE matches nothing, so the user DELETE
		// trips the invites.used_by foreign key — and the space DELETE
		// that succeeded mid-transaction must roll back with it.
		if err := s.UnregisterInvitedUser(ctx, "hash-ghost", user.ID, sp.ID); err == nil {
			t.Fatal("mismatched hash: want error, got nil")
		}
		if !claimed(t, s) {
			t.Error("failed unregister released the claim (double-mint window)")
		}
		if _, err := s.GetUserByUsername(ctx, "nina"); err != nil {
			t.Errorf("failed unregister removed the user: %v", err)
		}
		if _, err := s.GetSpace(ctx, sp.ID); err != nil {
			t.Errorf("failed unregister removed the space: %v", err)
		}
		// And the code stays burned: a second registration must fail.
		u2, sp2 := registerArgs("otto")
		if _, _, err := s.RegisterInvitedUser(ctx, inv.CodeHash, u2, sp2); !errors.Is(err, ErrInviteInvalid) {
			t.Errorf("burned code reuse err = %v, want ErrInviteInvalid", err)
		}
	})

	t.Run("canceled context changes nothing", func(t *testing.T) {
		t.Parallel()
		s, _, admin := newInviteStore(t)
		inv, user, sp := registered(t, s, admin.ID, "cc", "nina")

		canceled, cancel := context.WithCancel(context.Background())
		cancel()
		if err := s.UnregisterInvitedUser(canceled, inv.CodeHash, user.ID, sp.ID); err == nil {
			t.Fatal("canceled context: want error, got nil")
		}
		if !claimed(t, s) {
			t.Error("failed unregister released the claim")
		}
		if _, err := s.GetUserByUsername(context.Background(), "nina"); err != nil {
			t.Errorf("failed unregister removed the user: %v", err)
		}
	})

	t.Run("input validation", func(t *testing.T) {
		t.Parallel()
		s, _, admin := newInviteStore(t)
		inv, user, sp := registered(t, s, admin.ID, "val", "nina")
		tests := []struct {
			name     string
			codeHash string
			userID   int64
			spaceID  string
		}{
			{name: "empty code hash", codeHash: "", userID: user.ID, spaceID: sp.ID},
			{name: "zero user id", codeHash: inv.CodeHash, userID: 0, spaceID: sp.ID},
			{name: "negative user id", codeHash: inv.CodeHash, userID: -1, spaceID: sp.ID},
			{name: "empty space id", codeHash: inv.CodeHash, userID: user.ID, spaceID: ""},
		}
		for _, tt := range tests {
			tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				err := s.UnregisterInvitedUser(context.Background(), tt.codeHash, tt.userID, tt.spaceID)
				if err == nil {
					t.Fatal("want error, got nil")
				}
			})
		}
	})
}
