package api

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"testing"

	"github.com/bgrewell/donezo/internal/notify"
	"github.com/bgrewell/donezo/internal/store"
)

// addVerifiedEmailContact gives a user a verified email destination, set up
// through the store directly (the API path would require intercepting the
// emailed verification code).
func addVerifiedEmailContact(t *testing.T, f *authFixture, username, address string) {
	t.Helper()
	ctx := context.Background()
	u, err := f.core.GetUserByUsername(ctx, username)
	if err != nil {
		t.Fatalf("lookup %s: %v", username, err)
	}
	id := "ctc-" + username
	if _, err := f.core.CreateUserContact(ctx, store.UserContact{ID: id, UserID: u.ID, Channel: "email", Address: address}); err != nil {
		t.Fatalf("create contact: %v", err)
	}
	if _, err := f.core.StartContactChallenge(ctx, u.ID, id, "codehash"); err != nil {
		t.Fatalf("start challenge: %v", err)
	}
	if _, err := f.core.VerifyUserContact(ctx, u.ID, id, "codehash"); err != nil {
		t.Fatalf("verify contact: %v", err)
	}
}

// resetLinkToken pulls the token out of a reset email body.
var resetLinkToken = regexp.MustCompile(`#/reset/([A-Za-z0-9_-]+)`)

// newResetFixture builds an auth fixture with a recording email channel and a
// public URL, and completes owner setup with a known email + password.
func newResetFixture(t *testing.T, ownerEmail, ownerPassword string) (*authFixture, *recordingSender) {
	t.Helper()
	email := &recordingSender{channel: notify.ChannelEmail}
	f := newAuthFixture(t, WithNotifiers(notify.NewRegistry(email)), WithPublicURL("https://donezo.example"))
	body := fmt.Sprintf(`{"username":"owner","displayName":"Owner","password":%q,"email":%q}`, ownerPassword, ownerEmail)
	if rec := f.do(http.MethodPost, "/api/auth/setup", body, nil); rec.Code != http.StatusOK {
		t.Fatalf("setup owner: %d (body %s)", rec.Code, rec.Body.String())
	}
	return f, email
}

func TestPasswordResetFlow(t *testing.T) {
	t.Parallel()
	const oldPw, newPw = "the old password", "a brand new password"
	f, email := newResetFixture(t, "owner@example.com", oldPw)

	// Request a reset: uniform 200, one email with a reset link.
	rec := f.do(http.MethodPost, "/api/auth/forgot-password", `{"email":"owner@example.com"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("forgot-password status = %d (body %s)", rec.Code, rec.Body.String())
	}
	sent := email.deliveries()
	if len(sent) != 1 || sent[0].to != "owner@example.com" {
		t.Fatalf("reset email deliveries = %+v, want one to owner@example.com", sent)
	}
	m := resetLinkToken.FindStringSubmatch(sent[0].msg.Body)
	if m == nil {
		t.Fatalf("reset email carries no #/reset/<token> link:\n%s", sent[0].msg.Body)
	}
	token := m[1]

	// Spend the token: 200 with a fresh session cookie.
	rec = f.do(http.MethodPost, "/api/auth/reset-password",
		fmt.Sprintf(`{"token":%q,"password":%q}`, token, newPw), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("reset-password status = %d (body %s)", rec.Code, rec.Body.String())
	}
	sessionCookieFrom(t, rec) // logged straight in

	// The new password works; the old one does not.
	if rec := f.do(http.MethodPost, "/api/auth/login", fmt.Sprintf(`{"username":"owner","password":%q}`, newPw), nil); rec.Code != http.StatusOK {
		t.Errorf("login with new password = %d, want 200", rec.Code)
	}
	if rec := f.do(http.MethodPost, "/api/auth/login", fmt.Sprintf(`{"username":"owner","password":%q}`, oldPw), nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("login with old password = %d, want 401", rec.Code)
	}

	// The token is single-use: a replay is refused.
	if rec := f.do(http.MethodPost, "/api/auth/reset-password",
		fmt.Sprintf(`{"token":%q,"password":"yet another password"}`, token), nil); rec.Code != http.StatusBadRequest {
		t.Errorf("token replay = %d, want 400", rec.Code)
	}
}

func TestForgotPasswordDoesNotEnumerate(t *testing.T) {
	t.Parallel()
	f, email := newResetFixture(t, "owner@example.com", "the old password")

	known := f.do(http.MethodPost, "/api/auth/forgot-password", `{"email":"owner@example.com"}`, nil)
	unknown := f.do(http.MethodPost, "/api/auth/forgot-password", `{"email":"stranger@example.com"}`, nil)

	// Identical status and body whether or not the address is on file.
	if known.Code != http.StatusOK || unknown.Code != http.StatusOK {
		t.Fatalf("statuses = %d / %d, want 200 / 200", known.Code, unknown.Code)
	}
	if known.Body.String() != unknown.Body.String() {
		t.Errorf("responses differ — an enumeration oracle:\n known:   %s\n unknown: %s", known.Body, unknown.Body)
	}
	// Only the real address got an email.
	sent := email.deliveries()
	if len(sent) != 1 || sent[0].to != "owner@example.com" {
		t.Errorf("deliveries = %+v, want exactly one to owner@example.com", sent)
	}
}

func TestResetPasswordRejectsBadToken(t *testing.T) {
	t.Parallel()
	f, _ := newResetFixture(t, "owner@example.com", "the old password")
	for _, tok := range []string{"never-issued", ""} {
		rec := f.do(http.MethodPost, "/api/auth/reset-password",
			fmt.Sprintf(`{"token":%q,"password":"a brand new password"}`, tok), nil)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("reset with token %q = %d, want 400 (body %s)", tok, rec.Code, rec.Body.String())
		}
	}
}

func TestResetPasswordInvalidatesExistingSessions(t *testing.T) {
	t.Parallel()
	const oldPw = "the old password"
	f, email := newResetFixture(t, "owner@example.com", oldPw)

	// A live session that predates the reset.
	old := f.login("owner", oldPw)
	if rec := f.do(http.MethodGet, "/api/auth/me", "", withCookie(old)); rec.Code != http.StatusOK {
		t.Fatalf("pre-reset me = %d, want 200", rec.Code)
	}

	f.do(http.MethodPost, "/api/auth/forgot-password", `{"email":"owner@example.com"}`, nil)
	token := resetLinkToken.FindStringSubmatch(email.deliveries()[0].msg.Body)[1]
	if rec := f.do(http.MethodPost, "/api/auth/reset-password",
		fmt.Sprintf(`{"token":%q,"password":"a brand new password"}`, token), nil); rec.Code != http.StatusOK {
		t.Fatalf("reset = %d, want 200", rec.Code)
	}

	// The old session is dead — a thief holding it is cut off.
	if rec := f.do(http.MethodGet, "/api/auth/me", "", withCookie(old)); rec.Code != http.StatusUnauthorized {
		t.Errorf("old session after reset = %d, want 401", rec.Code)
	}
}

func TestForgotPasswordViaVerifiedContact(t *testing.T) {
	t.Parallel()
	f, email := newResetFixture(t, "owner@example.com", "the old password")

	// A member whose account email differs from a verified email contact.
	// The owner set up by newResetFixture is the admin who mints the invite.
	admin := f.login("owner", "the old password")
	inv := f.createInvite(admin, "")
	if rec := f.do(http.MethodPost, "/api/auth/register",
		`{"code":"`+inv.Code+`","username":"nina","displayName":"Nina","password":"a very fine password","email":"nina-account@example.com"}`, nil); rec.Code != http.StatusOK {
		t.Fatalf("register nina = %d (body %s)", rec.Code, rec.Body.String())
	}
	addVerifiedEmailContact(t, f, "nina", "nina-contact@example.com")

	before := len(email.deliveries())
	// Reset by the CONTACT address (not the account email) still finds her.
	if rec := f.do(http.MethodPost, "/api/auth/forgot-password", `{"email":"nina-contact@example.com"}`, nil); rec.Code != http.StatusOK {
		t.Fatalf("forgot via contact = %d", rec.Code)
	}
	sent := email.deliveries()
	if len(sent) != before+1 || sent[len(sent)-1].to != "nina-contact@example.com" {
		t.Errorf("deliveries = %+v, want a reset to the verified contact", sent)
	}
}

func TestRegisterRejectsDuplicateEmail(t *testing.T) {
	t.Parallel()
	f := newAuthFixture(t)
	admin := f.setupAdmin("owner")

	inv1 := f.createInvite(admin, "")
	if rec := f.do(http.MethodPost, "/api/auth/register",
		`{"code":"`+inv1.Code+`","username":"nina","displayName":"Nina","password":"a very fine password","email":"shared@example.com"}`, nil); rec.Code != http.StatusOK {
		t.Fatalf("register nina = %d (body %s)", rec.Code, rec.Body.String())
	}
	inv2 := f.createInvite(admin, "")
	rec := f.do(http.MethodPost, "/api/auth/register",
		`{"code":"`+inv2.Code+`","username":"otto","displayName":"Otto","password":"a very fine password","email":"Shared@example.com"}`, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate email (case-folded) = %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}
	// The rejected registration did not burn the invite.
	if listed, _ := f.listInvites(admin); listed[inv2.ID].Status != "active" {
		t.Errorf("invite after a rejected duplicate-email register = %q, want active", listed[inv2.ID].Status)
	}
}
