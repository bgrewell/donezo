package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

// credentialedUser creates a user that can log in (non-empty password), the
// only kind a reset targets.
func credentialedUser(t *testing.T, s *CoreStore, username string) User {
	t.Helper()
	u, err := s.CreateUser(context.Background(), username, username)
	if err != nil {
		t.Fatalf("CreateUser %s: %v", username, err)
	}
	if err := s.SetUserPassword(context.Background(), u.ID, "$argon2id$v=19$m=8,t=1,p=1$c2FsdA$aGFzaA"); err != nil {
		t.Fatalf("SetUserPassword %s: %v", username, err)
	}
	return u
}

// verifiedEmailContact adds a verified email destination to a user.
func verifiedEmailContact(t *testing.T, s *CoreStore, userID int64, id, address string) {
	t.Helper()
	ctx := context.Background()
	if _, err := s.CreateUserContact(ctx, UserContact{ID: id, UserID: userID, Channel: "email", Address: address}); err != nil {
		t.Fatalf("CreateUserContact: %v", err)
	}
	if _, err := s.StartContactChallenge(ctx, userID, id, "codehash"); err != nil {
		t.Fatalf("StartContactChallenge: %v", err)
	}
	if _, err := s.VerifyUserContact(ctx, userID, id, "codehash"); err != nil {
		t.Fatalf("VerifyUserContact: %v", err)
	}
}

func TestFindUserIDForReset(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("matches an account email, case-insensitively", func(t *testing.T) {
		t.Parallel()
		s := newTestCoreStore(t)
		u := credentialedUser(t, s, "alice")
		email := "Alice@Example.com"
		if err := s.SetUserEmail(ctx, u.ID, &email); err != nil {
			t.Fatalf("SetUserEmail: %v", err)
		}
		id, ok, err := s.FindUserIDForReset(ctx, "alice@example.com")
		if err != nil || !ok || id != u.ID {
			t.Fatalf("FindUserIDForReset = (%d,%v,%v), want (%d,true,nil)", id, ok, err, u.ID)
		}
	})

	t.Run("matches a verified email contact", func(t *testing.T) {
		t.Parallel()
		s := newTestCoreStore(t)
		u := credentialedUser(t, s, "bob")
		verifiedEmailContact(t, s, u.ID, "ctc-bob", "bob@example.com")
		id, ok, err := s.FindUserIDForReset(ctx, "BOB@example.com")
		if err != nil || !ok || id != u.ID {
			t.Fatalf("FindUserIDForReset = (%d,%v,%v), want (%d,true,nil)", id, ok, err, u.ID)
		}
	})

	t.Run("no match returns ok=false", func(t *testing.T) {
		t.Parallel()
		s := newTestCoreStore(t)
		credentialedUser(t, s, "carol")
		if _, ok, err := s.FindUserIDForReset(ctx, "nobody@example.com"); ok || err != nil {
			t.Fatalf("unknown email = (ok %v, err %v), want (false, nil)", ok, err)
		}
	})

	t.Run("ambiguous match (two users, same address) returns ok=false", func(t *testing.T) {
		t.Parallel()
		s := newTestCoreStore(t)
		u1 := credentialedUser(t, s, "dave")
		shared := "shared@example.com"
		if err := s.SetUserEmail(ctx, u1.ID, &shared); err != nil {
			t.Fatalf("SetUserEmail: %v", err)
		}
		u2 := credentialedUser(t, s, "erin")
		// u2 has the same address as a verified contact — a different account,
		// same email. The reset must refuse to guess which to send to.
		verifiedEmailContact(t, s, u2.ID, "ctc-erin", shared)
		if _, ok, err := s.FindUserIDForReset(ctx, shared); ok || err != nil {
			t.Fatalf("ambiguous email = (ok %v, err %v), want (false, nil)", ok, err)
		}
	})

	t.Run("password-less account is not a target", func(t *testing.T) {
		t.Parallel()
		s := newTestCoreStore(t)
		u, err := s.CreateUser(ctx, "frank", "Frank") // no password set
		if err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
		email := "frank@example.com"
		if err := s.SetUserEmail(ctx, u.ID, &email); err != nil {
			t.Fatalf("SetUserEmail: %v", err)
		}
		if _, ok, _ := s.FindUserIDForReset(ctx, email); ok {
			t.Error("a password-less account was offered a reset")
		}
	})

	t.Run("unverified contact is not a target", func(t *testing.T) {
		t.Parallel()
		s := newTestCoreStore(t)
		u := credentialedUser(t, s, "gina")
		if _, err := s.CreateUserContact(ctx, UserContact{ID: "ctc-gina", UserID: u.ID, Channel: "email", Address: "gina@example.com"}); err != nil {
			t.Fatalf("CreateUserContact: %v", err)
		}
		if _, ok, _ := s.FindUserIDForReset(ctx, "gina@example.com"); ok {
			t.Error("an unverified contact was offered a reset")
		}
	})
}

func TestPasswordResetTokenLifecycle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("create then consume returns the user, once", func(t *testing.T) {
		t.Parallel()
		s := newTestCoreStore(t)
		u := credentialedUser(t, s, "alice")
		if err := s.CreatePasswordReset(ctx, u.ID, "hash-1", time.Hour); err != nil {
			t.Fatalf("CreatePasswordReset: %v", err)
		}
		got, err := s.ConsumePasswordReset(ctx, "hash-1")
		if err != nil || got != u.ID {
			t.Fatalf("first consume = (%d, %v), want (%d, nil)", got, err, u.ID)
		}
		// Single-use: a second spend of the same token is refused.
		if _, err := s.ConsumePasswordReset(ctx, "hash-1"); !errors.Is(err, ErrResetInvalid) {
			t.Errorf("second consume err = %v, want ErrResetInvalid", err)
		}
	})

	t.Run("unknown token is invalid", func(t *testing.T) {
		t.Parallel()
		s := newTestCoreStore(t)
		if _, err := s.ConsumePasswordReset(ctx, "nope"); !errors.Is(err, ErrResetInvalid) {
			t.Errorf("unknown token err = %v, want ErrResetInvalid", err)
		}
	})

	t.Run("expired token is invalid", func(t *testing.T) {
		t.Parallel()
		clock := newTickingClock()
		s, err := NewCoreStore(WithDataDir(t.TempDir()), WithClock(clock.Now))
		if err != nil {
			t.Fatalf("NewCoreStore: %v", err)
		}
		t.Cleanup(func() { _ = s.Close() })
		u := credentialedUser(t, s, "bob")
		if err := s.CreatePasswordReset(ctx, u.ID, "hash-exp", time.Minute); err != nil {
			t.Fatalf("CreatePasswordReset: %v", err)
		}
		clock.Advance(2 * time.Minute) // past the 1-minute TTL
		if _, err := s.ConsumePasswordReset(ctx, "hash-exp"); !errors.Is(err, ErrResetInvalid) {
			t.Errorf("expired token err = %v, want ErrResetInvalid", err)
		}
	})

	t.Run("a new reset invalidates the previous one", func(t *testing.T) {
		t.Parallel()
		s := newTestCoreStore(t)
		u := credentialedUser(t, s, "carol")
		if err := s.CreatePasswordReset(ctx, u.ID, "hash-old", time.Hour); err != nil {
			t.Fatalf("CreatePasswordReset old: %v", err)
		}
		if err := s.CreatePasswordReset(ctx, u.ID, "hash-new", time.Hour); err != nil {
			t.Fatalf("CreatePasswordReset new: %v", err)
		}
		if _, err := s.ConsumePasswordReset(ctx, "hash-old"); !errors.Is(err, ErrResetInvalid) {
			t.Errorf("old token after re-request err = %v, want ErrResetInvalid", err)
		}
		if got, err := s.ConsumePasswordReset(ctx, "hash-new"); err != nil || got != u.ID {
			t.Errorf("new token consume = (%d, %v), want (%d, nil)", got, err, u.ID)
		}
	})
}

func TestDeleteUserSessions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestCoreStore(t)
	u := credentialedUser(t, s, "alice")
	other := credentialedUser(t, s, "bob")
	if _, err := s.CreateSession(ctx, u.ID, "sess-1", time.Hour); err != nil {
		t.Fatalf("CreateSession 1: %v", err)
	}
	if _, err := s.CreateSession(ctx, u.ID, "sess-2", time.Hour); err != nil {
		t.Fatalf("CreateSession 2: %v", err)
	}
	if _, err := s.CreateSession(ctx, other.ID, "sess-other", time.Hour); err != nil {
		t.Fatalf("CreateSession other: %v", err)
	}

	n, err := s.DeleteUserSessions(ctx, u.ID)
	if err != nil {
		t.Fatalf("DeleteUserSessions: %v", err)
	}
	if n != 2 {
		t.Errorf("deleted %d sessions, want 2", n)
	}
	// alice's sessions are gone…
	if _, _, err := s.GetSessionUser(ctx, "sess-1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("sess-1 after delete err = %v, want ErrNotFound", err)
	}
	// …but the other user's session is untouched.
	if _, _, err := s.GetSessionUser(ctx, "sess-other"); err != nil {
		t.Errorf("other user's session was collateral: %v", err)
	}
}
