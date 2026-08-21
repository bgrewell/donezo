package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func newCoreForInbound(t *testing.T) (*CoreStore, context.Context) {
	t.Helper()
	core, err := NewCoreStore(WithDataDir(t.TempDir()), WithClock(fixedClock))
	if err != nil {
		t.Fatalf("NewCoreStore: %v", err)
	}
	t.Cleanup(func() {
		if err := core.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	return core, context.Background()
}

// verifyContact creates and verifies an SMS contact for a user.
func verifyContact(t *testing.T, core *CoreStore, ctx context.Context, userID int64, addr string) {
	t.Helper()
	id := fmt.Sprintf("ct-%d-%s", userID, addr)
	if _, err := core.CreateUserContact(ctx, UserContact{
		ID: id, UserID: userID, Channel: "sms", Address: addr, CreatedAt: "2026-08-21T00:00:00Z",
	}); err != nil {
		t.Fatalf("create contact: %v", err)
	}
	if _, err := core.StartContactChallenge(ctx, userID, id, "h"); err != nil {
		t.Fatalf("challenge: %v", err)
	}
	if _, err := core.VerifyUserContact(ctx, userID, id, "h"); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestUserForVerifiedContact(t *testing.T) {
	t.Parallel()
	core, ctx := newCoreForInbound(t)
	a, _ := core.CreateUser(ctx, "a", "A")
	b, _ := core.CreateUser(ctx, "b", "B")

	// No contact anywhere -> not found.
	if _, err := core.UserForVerifiedContact(ctx, "sms", "+10000000000"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown number err = %v, want ErrNotFound", err)
	}

	// Verified for A -> resolves to A.
	verifyContact(t, core, ctx, a.ID, "+11111111111")
	if u, err := core.UserForVerifiedContact(ctx, "sms", "+11111111111"); err != nil || u.ID != a.ID {
		t.Errorf("verified lookup = (%d, %v), want (%d, nil)", u.ID, err, a.ID)
	}

	// A number verified by BOTH users is ambiguous — must not be attributed.
	verifyContact(t, core, ctx, a.ID, "+12222222222")
	verifyContact(t, core, ctx, b.ID, "+12222222222")
	if _, err := core.UserForVerifiedContact(ctx, "sms", "+12222222222"); !errors.Is(err, ErrAmbiguousContact) {
		t.Errorf("shared number err = %v, want ErrAmbiguousContact", err)
	}

	// An unverified contact does not count.
	if _, err := core.CreateUserContact(ctx, UserContact{
		ID: "unv", UserID: a.ID, Channel: "sms", Address: "+13333333333", CreatedAt: "2026-08-21T00:00:00Z",
	}); err != nil {
		t.Fatalf("create unverified: %v", err)
	}
	if _, err := core.UserForVerifiedContact(ctx, "sms", "+13333333333"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unverified number err = %v, want ErrNotFound", err)
	}
}

func TestFirstLiveSpace(t *testing.T) {
	t.Parallel()
	core, ctx := newCoreForInbound(t)
	u, _ := core.CreateUser(ctx, "u", "U")

	// No space yet -> not found.
	if _, err := core.FirstLiveSpace(ctx, u.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("no-space err = %v, want ErrNotFound", err)
	}

	if _, err := core.CreateSpace(ctx, Space{ID: "s2", UserID: u.ID, Name: "Second", Color: "blue", Position: 2}); err != nil {
		t.Fatalf("create s2: %v", err)
	}
	if _, err := core.CreateSpace(ctx, Space{ID: "s1", UserID: u.ID, Name: "First", Color: "green", Position: 1}); err != nil {
		t.Fatalf("create s1: %v", err)
	}
	// Lowest position wins.
	if sp, err := core.FirstLiveSpace(ctx, u.ID); err != nil || sp.ID != "s1" {
		t.Errorf("first live space = (%q, %v), want s1", sp.ID, err)
	}
}
