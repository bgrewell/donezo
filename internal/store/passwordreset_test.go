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

	t.Run("an unverified account-email shadow does not block a verified owner", func(t *testing.T) {
		t.Parallel()
		s := newTestCoreStore(t)
		// The attack: an attacker sets their (unverified) account email to a
		// victim's verified-contact address, hoping to make the reset lookup
		// ambiguous and lock the victim out. The verified owner must still win.
		attacker := credentialedUser(t, s, "attacker")
		shared := "victim-contact@example.com"
		if err := s.SetUserEmail(ctx, attacker.ID, &shared); err != nil {
			t.Fatalf("SetUserEmail: %v", err)
		}
		victim := credentialedUser(t, s, "victim")
		verifiedEmailContact(t, s, victim.ID, "ctc-victim", shared)

		id, ok, err := s.FindUserIDForReset(ctx, shared)
		if err != nil || !ok || id != victim.ID {
			t.Fatalf("shadowed lookup = (%d, %v, %v), want the verified victim (%d, true, nil)", id, ok, err, victim.ID)
		}
	})

	t.Run("two accounts that both verified the address is genuinely ambiguous", func(t *testing.T) {
		t.Parallel()
		s := newTestCoreStore(t)
		shared := "shared@example.com"
		a := credentialedUser(t, s, "aaron")
		b := credentialedUser(t, s, "bianca")
		verifiedEmailContact(t, s, a.ID, "ctc-a", shared)
		verifiedEmailContact(t, s, b.ID, "ctc-b", shared)
		if _, ok, err := s.FindUserIDForReset(ctx, shared); ok || err != nil {
			t.Fatalf("two verified owners = (ok %v, err %v), want (false, nil)", ok, err)
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
		if issued, err := s.CreatePasswordReset(ctx, u.ID, "hash-1", time.Hour); err != nil || !issued {
			t.Fatalf("CreatePasswordReset = (%v, %v), want (true, nil)", issued, err)
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
		if _, err := s.CreatePasswordReset(ctx, u.ID, "hash-exp", time.Minute); err != nil {
			t.Fatalf("CreatePasswordReset: %v", err)
		}
		clock.Advance(2 * time.Minute) // past the 1-minute TTL
		if _, err := s.ConsumePasswordReset(ctx, "hash-exp"); !errors.Is(err, ErrResetInvalid) {
			t.Errorf("expired token err = %v, want ErrResetInvalid", err)
		}
	})

	t.Run("a new reset after the cooldown invalidates the previous one", func(t *testing.T) {
		t.Parallel()
		clock := newTickingClock()
		s, err := NewCoreStore(WithDataDir(t.TempDir()), WithClock(clock.Now))
		if err != nil {
			t.Fatalf("NewCoreStore: %v", err)
		}
		t.Cleanup(func() { _ = s.Close() })
		u := credentialedUser(t, s, "carol")
		if issued, err := s.CreatePasswordReset(ctx, u.ID, "hash-old", time.Hour); err != nil || !issued {
			t.Fatalf("CreatePasswordReset old = (%v, %v)", issued, err)
		}
		clock.Advance(ResetResendInterval + time.Second) // past the resend cooldown
		if issued, err := s.CreatePasswordReset(ctx, u.ID, "hash-new", time.Hour); err != nil || !issued {
			t.Fatalf("CreatePasswordReset new = (%v, %v)", issued, err)
		}
		if _, err := s.ConsumePasswordReset(ctx, "hash-old"); !errors.Is(err, ErrResetInvalid) {
			t.Errorf("old token after re-request err = %v, want ErrResetInvalid", err)
		}
		if got, err := s.ConsumePasswordReset(ctx, "hash-new"); err != nil || got != u.ID {
			t.Errorf("new token consume = (%d, %v), want (%d, nil)", got, err, u.ID)
		}
	})

	t.Run("a second request within the cooldown does not reissue", func(t *testing.T) {
		t.Parallel()
		s := newTestCoreStore(t)
		u := credentialedUser(t, s, "dave")
		if issued, err := s.CreatePasswordReset(ctx, u.ID, "hash-first", time.Hour); err != nil || !issued {
			t.Fatalf("first CreatePasswordReset = (%v, %v), want (true, nil)", issued, err)
		}
		// Immediately again: throttled, no new token, the first still stands.
		if issued, err := s.CreatePasswordReset(ctx, u.ID, "hash-second", time.Hour); err != nil || issued {
			t.Fatalf("second CreatePasswordReset = (%v, %v), want (false, nil) — throttled", issued, err)
		}
		if _, err := s.ConsumePasswordReset(ctx, "hash-second"); !errors.Is(err, ErrResetInvalid) {
			t.Errorf("throttled token should not exist, consume err = %v, want ErrResetInvalid", err)
		}
		if got, err := s.ConsumePasswordReset(ctx, "hash-first"); err != nil || got != u.ID {
			t.Errorf("first token should still be valid, consume = (%d, %v)", got, err)
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
