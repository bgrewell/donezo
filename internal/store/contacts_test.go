package store

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

// contactFixture is a core store with one user and a movable clock.
type contactFixture struct {
	core   *CoreStore
	userID int64
	now    time.Time
}

func newContactFixture(t *testing.T) *contactFixture {
	t.Helper()
	f := &contactFixture{now: time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)}
	core, err := NewCoreStore(WithDataDir(t.TempDir()), WithClock(func() time.Time { return f.now }))
	if err != nil {
		t.Fatalf("NewCoreStore: %v", err)
	}
	t.Cleanup(func() {
		if err := core.Close(); err != nil {
			t.Errorf("close core: %v", err)
		}
	})
	f.core = core
	user, err := core.CreateUser(context.Background(), "ben", "Ben")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	f.userID = user.ID
	return f
}

// add creates a destination.
func (f *contactFixture) add(t *testing.T, id, channel, address string) UserContact {
	t.Helper()
	c, err := f.core.CreateUserContact(context.Background(), UserContact{
		ID: id, UserID: f.userID, Channel: channel, Address: address,
	})
	if err != nil {
		t.Fatalf("create contact: %v", err)
	}
	return c
}

func TestCreateUserContactStartsUnverified(t *testing.T) {
	f := newContactFixture(t)
	c := f.add(t, "ctc-1", "email", "ben@example.com")
	if c.Verified() {
		// The whole safety property: a fresh destination is not deliverable.
		t.Fatal("a new contact is verified; it must start unverified")
	}
	if c.CreatedAt == "" {
		t.Fatal("CreatedAt not stamped")
	}
}

func TestCreateUserContactRejectsDuplicates(t *testing.T) {
	f := newContactFixture(t)
	f.add(t, "ctc-1", "email", "ben@example.com")
	_, err := f.core.CreateUserContact(context.Background(), UserContact{
		ID: "ctc-2", UserID: f.userID, Channel: "email", Address: "ben@example.com",
	})
	if !errors.Is(err, ErrDuplicateID) {
		t.Fatalf("second identical destination = %v, want a duplicate error", err)
	}
}

func TestCreateUserContactValidatesRequiredFields(t *testing.T) {
	tests := []struct {
		name    string
		contact UserContact
	}{
		{name: "no id", contact: UserContact{UserID: 1, Channel: "email", Address: "a@b.com"}},
		{name: "no user", contact: UserContact{ID: "ctc-1", Channel: "email", Address: "a@b.com"}},
		{name: "no channel", contact: UserContact{ID: "ctc-1", UserID: 1, Address: "a@b.com"}},
		{name: "no address", contact: UserContact{ID: "ctc-1", UserID: 1, Channel: "email"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newContactFixture(t)
			tt.contact.UserID = f.userID
			if tt.name == "no user" {
				tt.contact.UserID = 0
			}
			if _, err := f.core.CreateUserContact(context.Background(), tt.contact); err == nil {
				t.Fatal("create accepted an incomplete contact")
			}
		})
	}
}

func TestVerifyUserContactHappyPath(t *testing.T) {
	f := newContactFixture(t)
	c := f.add(t, "ctc-1", "email", "ben@example.com")
	ctx := context.Background()

	if _, err := f.core.StartContactChallenge(ctx, f.userID, c.ID, "the-hash"); err != nil {
		t.Fatalf("start challenge: %v", err)
	}
	verified, err := f.core.VerifyUserContact(ctx, f.userID, c.ID, "the-hash")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !verified.Verified() {
		t.Fatal("contact not marked verified")
	}
	if verified.PendingCode {
		t.Fatal("challenge still pending after a successful verification")
	}
}

func TestVerifyUserContactWrongCode(t *testing.T) {
	f := newContactFixture(t)
	c := f.add(t, "ctc-1", "email", "ben@example.com")
	ctx := context.Background()
	if _, err := f.core.StartContactChallenge(ctx, f.userID, c.ID, "right"); err != nil {
		t.Fatalf("start challenge: %v", err)
	}

	if _, err := f.core.VerifyUserContact(ctx, f.userID, c.ID, "wrong"); !errors.Is(err, ErrCodeIncorrect) {
		t.Fatalf("verify with a wrong code = %v, want ErrCodeIncorrect", err)
	}
	// Still usable: one typo must not burn the challenge.
	if _, err := f.core.VerifyUserContact(ctx, f.userID, c.ID, "right"); err != nil {
		t.Fatalf("verify after one wrong guess: %v", err)
	}
}

func TestVerifyUserContactAttemptLimit(t *testing.T) {
	f := newContactFixture(t)
	c := f.add(t, "ctc-1", "email", "ben@example.com")
	ctx := context.Background()
	if _, err := f.core.StartContactChallenge(ctx, f.userID, c.ID, "right"); err != nil {
		t.Fatalf("start challenge: %v", err)
	}

	for i := 0; i < ContactMaxAttempts; i++ {
		if _, err := f.core.VerifyUserContact(ctx, f.userID, c.ID, "wrong"); !errors.Is(err, ErrCodeIncorrect) {
			t.Fatalf("attempt %d = %v, want ErrCodeIncorrect", i+1, err)
		}
	}
	// The limit is what makes a six-digit code safe, so the RIGHT code must
	// also be refused once the budget is spent.
	if _, err := f.core.VerifyUserContact(ctx, f.userID, c.ID, "right"); !errors.Is(err, ErrTooManyAttempts) {
		t.Fatalf("verify after the limit = %v, want ErrTooManyAttempts", err)
	}
	got, err := f.core.GetUserContact(ctx, f.userID, c.ID)
	if err != nil {
		t.Fatalf("get contact: %v", err)
	}
	if got.Verified() {
		t.Fatal("contact was verified despite exhausting the attempt limit")
	}
}

func TestVerifyUserContactExpiry(t *testing.T) {
	f := newContactFixture(t)
	c := f.add(t, "ctc-1", "email", "ben@example.com")
	ctx := context.Background()
	if _, err := f.core.StartContactChallenge(ctx, f.userID, c.ID, "right"); err != nil {
		t.Fatalf("start challenge: %v", err)
	}

	f.now = f.now.Add(ContactCodeTTL + time.Second)
	if _, err := f.core.VerifyUserContact(ctx, f.userID, c.ID, "right"); !errors.Is(err, ErrCodeExpired) {
		t.Fatalf("verify after expiry = %v, want ErrCodeExpired", err)
	}
	got, err := f.core.GetUserContact(ctx, f.userID, c.ID)
	if err != nil {
		t.Fatalf("get contact: %v", err)
	}
	if got.Verified() {
		t.Fatal("an expired code verified the contact")
	}
}

func TestVerifyUserContactWithoutAChallenge(t *testing.T) {
	f := newContactFixture(t)
	c := f.add(t, "ctc-1", "email", "ben@example.com")
	// No code was ever sent. An empty stored hash must not match an empty
	// submitted one.
	if _, err := f.core.VerifyUserContact(context.Background(), f.userID, c.ID, ""); !errors.Is(err, ErrCodeExpired) {
		t.Fatalf("verify with no challenge = %v, want ErrCodeExpired", err)
	}
}

func TestStartContactChallengeThrottlesResends(t *testing.T) {
	f := newContactFixture(t)
	c := f.add(t, "ctc-1", "sms", "+15551234567")
	ctx := context.Background()
	if _, err := f.core.StartContactChallenge(ctx, f.userID, c.ID, "one"); err != nil {
		t.Fatalf("first challenge: %v", err)
	}
	// Immediately again: this is the button that texts somebody who may not
	// have asked to be texted.
	if _, err := f.core.StartContactChallenge(ctx, f.userID, c.ID, "two"); !errors.Is(err, ErrResendTooSoon) {
		t.Fatalf("immediate resend = %v, want ErrResendTooSoon", err)
	}
	f.now = f.now.Add(ContactResendInterval + time.Second)
	if _, err := f.core.StartContactChallenge(ctx, f.userID, c.ID, "three"); err != nil {
		t.Fatalf("resend after the interval: %v", err)
	}
}

func TestStartContactChallengeRefusesVerifiedContact(t *testing.T) {
	f := newContactFixture(t)
	c := f.add(t, "ctc-1", "email", "ben@example.com")
	ctx := context.Background()
	if _, err := f.core.StartContactChallenge(ctx, f.userID, c.ID, "hash"); err != nil {
		t.Fatalf("start challenge: %v", err)
	}
	if _, err := f.core.VerifyUserContact(ctx, f.userID, c.ID, "hash"); err != nil {
		t.Fatalf("verify: %v", err)
	}
	f.now = f.now.Add(time.Hour)
	if _, err := f.core.StartContactChallenge(ctx, f.userID, c.ID, "hash2"); err == nil {
		t.Fatal("started a challenge on an already-verified contact")
	}
}

func TestListVerifiedContactsExcludesUnverified(t *testing.T) {
	f := newContactFixture(t)
	ctx := context.Background()
	verified := f.add(t, "ctc-1", "email", "ben@example.com")
	f.add(t, "ctc-2", "sms", "+15551234567") // left unverified
	if _, err := f.core.StartContactChallenge(ctx, f.userID, verified.ID, "hash"); err != nil {
		t.Fatalf("start challenge: %v", err)
	}
	if _, err := f.core.VerifyUserContact(ctx, f.userID, verified.ID, "hash"); err != nil {
		t.Fatalf("verify: %v", err)
	}

	got, err := f.core.ListVerifiedContacts(ctx, f.userID)
	if err != nil {
		t.Fatalf("list verified: %v", err)
	}
	if len(got) != 1 || got[0].ID != verified.ID {
		t.Fatalf("ListVerifiedContacts = %+v, want only the verified one", got)
	}
	// The bystander is still listed by the unfiltered read — it exists, it
	// is just not deliverable.
	all, err := f.core.ListUserContacts(ctx, f.userID)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("ListUserContacts = %d contacts, want 2", len(all))
	}
}

// One user must not reach another's destinations, however the id was found.
func TestUserContactsAreScopedToTheirOwner(t *testing.T) {
	f := newContactFixture(t)
	ctx := context.Background()
	mine := f.add(t, "ctc-1", "email", "ben@example.com")

	intruder, err := f.core.CreateUser(ctx, "mallory", "Mallory")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	if _, err := f.core.GetUserContact(ctx, intruder.ID, mine.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-user get = %v, want ErrNotFound", err)
	}
	if err := f.core.DeleteUserContact(ctx, intruder.ID, mine.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-user delete = %v, want ErrNotFound", err)
	}
	if _, err := f.core.StartContactChallenge(ctx, intruder.ID, mine.ID, "hash"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-user challenge = %v, want ErrNotFound", err)
	}
	if _, err := f.core.VerifyUserContact(ctx, intruder.ID, mine.ID, "hash"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-user verify = %v, want ErrNotFound", err)
	}
	// And the bystander survived every one of those.
	if _, err := f.core.GetUserContact(ctx, f.userID, mine.ID); err != nil {
		t.Fatalf("owner's own contact after the cross-user attempts: %v", err)
	}
}

func TestDeleteUserContact(t *testing.T) {
	f := newContactFixture(t)
	ctx := context.Background()
	c := f.add(t, "ctc-1", "email", "ben@example.com")
	bystander := f.add(t, "ctc-2", "sms", "+15551234567")

	if err := f.core.DeleteUserContact(ctx, f.userID, c.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := f.core.GetUserContact(ctx, f.userID, c.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get after delete = %v, want ErrNotFound", err)
	}
	// A delete that took the whole table with it would pass a single-row
	// fixture, so the bystander is the assertion that matters.
	if _, err := f.core.GetUserContact(ctx, f.userID, bystander.ID); err != nil {
		t.Fatalf("bystander removed by an unrelated delete: %v", err)
	}
	if err := f.core.DeleteUserContact(ctx, f.userID, c.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete = %v, want ErrNotFound", err)
	}
}

func TestPendingCodeReflectsLiveChallenge(t *testing.T) {
	f := newContactFixture(t)
	ctx := context.Background()
	c := f.add(t, "ctc-1", "email", "ben@example.com")

	got, err := f.core.GetUserContact(ctx, f.userID, c.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.PendingCode {
		t.Fatal("PendingCode set before any code was sent")
	}

	if _, err := f.core.StartContactChallenge(ctx, f.userID, c.ID, "hash"); err != nil {
		t.Fatalf("start challenge: %v", err)
	}
	if got, _ = f.core.GetUserContact(ctx, f.userID, c.ID); !got.PendingCode {
		t.Fatal("PendingCode not set with a live challenge")
	}

	f.now = f.now.Add(ContactCodeTTL + time.Second)
	if got, _ = f.core.GetUserContact(ctx, f.userID, c.ID); got.PendingCode {
		t.Fatal("PendingCode still set after the challenge expired")
	}
}

// Finding #3: a user cannot hold more than MaxContactsPerUser destinations —
// each unverified add sends a message, so an unbounded list is an
// unbounded-message lever.
func TestCreateUserContactEnforcesCap(t *testing.T) {
	f := newContactFixture(t)
	ctx := context.Background()
	for i := 0; i < MaxContactsPerUser; i++ {
		if _, err := f.core.CreateUserContact(ctx, UserContact{
			ID: fmt.Sprintf("ctc-%02d", i), UserID: f.userID, Channel: "sms",
			Address: fmt.Sprintf("+1555000%04d", i),
		}); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}
	_, err := f.core.CreateUserContact(ctx, UserContact{
		ID: "ctc-over", UserID: f.userID, Channel: "sms", Address: "+15559999999",
	})
	if !errors.Is(err, ErrTooManyContacts) {
		t.Fatalf("create past the cap = %v, want ErrTooManyContacts", err)
	}
	// The cap is per user: a different account is unaffected.
	other, err := f.core.CreateUser(ctx, "mallory", "Mallory")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := f.core.CreateUserContact(ctx, UserContact{
		ID: "ctc-other", UserID: other.ID, Channel: "sms", Address: "+15558888888",
	}); err != nil {
		t.Fatalf("other user's first contact = %v, want success", err)
	}
}

// Finding #3: StartContactChallenge is atomic — concurrent calls on one
// contact produce exactly one send, not several. The old check-then-act read
// the send stamp, compared it in Go, then wrote separately, so racing callers
// all saw the same stale stamp and every one got through. This fails against
// that code.
func TestStartContactChallengeIsRaceFree(t *testing.T) {
	f := newContactFixture(t)
	ctx := context.Background()
	c := f.add(t, "ctc-1", "sms", "+15551234567")

	const racers = 16
	results := make(chan error, racers)
	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, err := f.core.StartContactChallenge(ctx, f.userID, c.ID, fmt.Sprintf("hash-%d", n))
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)

	var ok, tooSoon, other int
	for err := range results {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, ErrResendTooSoon):
			tooSoon++
		default:
			other++
		}
	}
	if ok != 1 {
		t.Fatalf("%d concurrent challenges produced %d sends, want exactly 1 (the rest throttled)", racers, ok)
	}
	if tooSoon != racers-1 {
		t.Fatalf("throttled = %d, want %d (other errors: %d)", tooSoon, racers-1, other)
	}
}
