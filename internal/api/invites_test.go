package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bgrewell/donezo/internal/auth"
	"github.com/bgrewell/donezo/internal/notify"
)

// renderedInviteCode matches the documented "dz-XXXXX-XXXXX" form.
var renderedInviteCode = regexp.MustCompile(`^dz-[0-9ABCDEFGHJKMNPQRSTVWXYZ]{5}-[0-9ABCDEFGHJKMNPQRSTVWXYZ]{5}$`)

// setupAdmin completes first-run setup as username and returns the
// admin's session cookie.
func (f *authFixture) setupAdmin(username string) *http.Cookie {
	f.t.Helper()
	body := fmt.Sprintf(`{"username":%q,"displayName":"Admin","password":"a very fine password"}`, username)
	rec := f.do(http.MethodPost, "/api/auth/setup", body, nil)
	if rec.Code != http.StatusOK {
		f.t.Fatalf("setup status = %d (body %s)", rec.Code, rec.Body.String())
	}
	return sessionCookieFrom(f.t, rec)
}

// login signs username in and returns the session cookie.
func (f *authFixture) login(username, password string) *http.Cookie {
	f.t.Helper()
	body := fmt.Sprintf(`{"username":%q,"password":%q}`, username, password)
	rec := f.do(http.MethodPost, "/api/auth/login", body, nil)
	if rec.Code != http.StatusOK {
		f.t.Fatalf("login status = %d (body %s)", rec.Code, rec.Body.String())
	}
	return sessionCookieFrom(f.t, rec)
}

// createdInvite is the parsed 201 body of POST /api/invites.
type createdInvite struct {
	ID         string `json:"id"`
	Code       string `json:"code"`
	CodePrefix string `json:"codePrefix"`
	ExpiresAt  string `json:"expiresAt"`
	Email      string `json:"email"`
	Sent       bool   `json:"sent"`
	Warning    string `json:"warning"`
}

// createInvite mints an invite as the cookie's user and parses the 201.
func (f *authFixture) createInvite(cookie *http.Cookie, body string) createdInvite {
	f.t.Helper()
	rec := f.do(http.MethodPost, "/api/invites", body, withCookie(cookie))
	if rec.Code != http.StatusCreated {
		f.t.Fatalf("create invite status = %d (body %s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Invite createdInvite `json:"invite"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		f.t.Fatalf("parse invite envelope: %v (body %s)", err, rec.Body.String())
	}
	return resp.Invite
}

// listedInvite is one element of GET /api/invites.
type listedInvite struct {
	ID         string  `json:"id"`
	CodePrefix string  `json:"codePrefix"`
	Status     string  `json:"status"`
	CreatedBy  string  `json:"createdBy"`
	CreatedAt  string  `json:"createdAt"`
	ExpiresAt  string  `json:"expiresAt"`
	UsedBy     *string `json:"usedBy"`
	UsedAt     *string `json:"usedAt"`
	RevokedAt  *string `json:"revokedAt"`
	Email      *string `json:"email"`
}

// listInvites fetches the admin invite list and indexes it by id,
// returning the raw body too so callers can assert on what is absent.
func (f *authFixture) listInvites(cookie *http.Cookie) (map[string]listedInvite, string) {
	f.t.Helper()
	rec := f.do(http.MethodGet, "/api/invites", "", withCookie(cookie))
	if rec.Code != http.StatusOK {
		f.t.Fatalf("list invites status = %d (body %s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Invites []listedInvite `json:"invites"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		f.t.Fatalf("parse invites list: %v (body %s)", err, rec.Body.String())
	}
	byID := make(map[string]listedInvite, len(resp.Invites))
	for _, inv := range resp.Invites {
		byID[inv.ID] = inv
	}
	return byID, rec.Body.String()
}

// registerBody builds a POST /api/auth/register payload.
func registerBody(code, username string) string {
	return fmt.Sprintf(`{"code":%q,"username":%q,"displayName":"New Member","password":"a very fine password"}`,
		code, username)
}

func TestSetupAssignsAdminRole(t *testing.T) {
	t.Parallel()
	f := newAuthFixture(t)
	cookie := f.setupAdmin("owner")

	rec := f.do(http.MethodGet, "/api/auth/me", "", withCookie(cookie))
	if rec.Code != http.StatusOK {
		t.Fatalf("me status = %d (body %s)", rec.Code, rec.Body.String())
	}
	if user := userFromBody(t, rec.Body.Bytes()); user["role"] != "admin" {
		t.Errorf("owner role = %v, want admin (me must include role)", user["role"])
	}
}

func TestInviteAdminGuard(t *testing.T) {
	t.Parallel()
	endpoints := []struct {
		method string
		path   string
	}{
		{method: http.MethodPost, path: "/api/invites"},
		{method: http.MethodGet, path: "/api/invites"},
		{method: http.MethodDelete, path: "/api/invites/inv-x"},
	}
	for _, ep := range endpoints {
		ep := ep // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(ep.method+" "+ep.path, func(t *testing.T) {
			t.Parallel()
			f := newAuthFixture(t)
			// A credentialed non-admin: created directly (not via setup),
			// so the store default — member — applies.
			f.credentialUser("mallory", "a very fine password")
			member := f.login("mallory", "a very fine password")

			if rec := f.do(ep.method, ep.path, "", nil); rec.Code != http.StatusUnauthorized {
				t.Errorf("anonymous status = %d, want 401", rec.Code)
			}
			rec := f.do(ep.method, ep.path, "", withCookie(member))
			if rec.Code != http.StatusForbidden {
				t.Fatalf("member status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "admin required") {
				t.Errorf("member body = %s, want the admin-required error", rec.Body.String())
			}
		})
	}
}

func TestInviteLifecycle(t *testing.T) {
	t.Parallel()
	f := newAuthFixture(t)
	admin := f.setupAdmin("owner")

	// Mint with defaults (empty body allowed): 7-day expiry.
	inv := f.createInvite(admin, "")
	if inv.ID == "" || !strings.HasPrefix(inv.ID, "inv-") {
		t.Errorf("invite id = %q, want inv- prefix", inv.ID)
	}
	if !renderedInviteCode.MatchString(inv.Code) {
		t.Errorf("code = %q, want dz-XXXXX-XXXXX", inv.Code)
	}
	if inv.CodePrefix != inv.Code[:auth.InviteCodePrefixLen] {
		t.Errorf("codePrefix = %q, want %q", inv.CodePrefix, inv.Code[:auth.InviteCodePrefixLen])
	}
	wantExpiry := f.clock.Now().Add(7 * 24 * time.Hour).UTC().Format(time.RFC3339)
	if inv.ExpiresAt != wantExpiry {
		t.Errorf("default expiresAt = %q, want %q (+7d)", inv.ExpiresAt, wantExpiry)
	}

	// The list shows it active, attributed, and code-free.
	listed, body := f.listInvites(admin)
	got, ok := listed[inv.ID]
	if !ok {
		t.Fatalf("minted invite missing from list: %s", body)
	}
	if got.Status != "active" || got.CreatedBy != "owner" || got.CodePrefix != inv.CodePrefix {
		t.Errorf("listed = %+v, want active, created by owner, prefix %q", got, inv.CodePrefix)
	}
	if strings.Contains(body, inv.Code) {
		t.Errorf("list body leaks the full code: %s", body)
	}
	if strings.Contains(body, inv.Code[auth.InviteCodePrefixLen+1:]) {
		t.Errorf("list body leaks the code's second group: %s", body)
	}

	// Revoke: 204, listed as revoked, idempotent, and unknown ids 404.
	if rec := f.do(http.MethodDelete, "/api/invites/"+inv.ID, "", withCookie(admin)); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke status = %d (body %s)", rec.Code, rec.Body.String())
	}
	listed, _ = f.listInvites(admin)
	if listed[inv.ID].Status != "revoked" || listed[inv.ID].RevokedAt == nil {
		t.Errorf("after revoke: %+v, want status revoked with a stamp", listed[inv.ID])
	}
	if rec := f.do(http.MethodDelete, "/api/invites/"+inv.ID, "", withCookie(admin)); rec.Code != http.StatusNoContent {
		t.Errorf("second revoke status = %d, want 204 (idempotent)", rec.Code)
	}
	if rec := f.do(http.MethodDelete, "/api/invites/inv-ghost", "", withCookie(admin)); rec.Code != http.StatusNotFound {
		t.Errorf("revoke unknown status = %d, want 404", rec.Code)
	}

	// Expiry is derived at read time: a short-lived invite flips to
	// expired once the clock passes it.
	short := f.createInvite(admin, `{"expiresInDays":1}`)
	f.clock.Advance(48 * time.Hour)
	listed, _ = f.listInvites(admin)
	if listed[short.ID].Status != "expired" {
		t.Errorf("short invite status = %q, want expired", listed[short.ID].Status)
	}
}

func TestCreateInviteExpiryBounds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantDays   int // asserted only on 201
	}{
		{name: "empty body defaults to 7 days", body: "", wantStatus: http.StatusCreated, wantDays: 7},
		{name: "empty object defaults to 7 days", body: "{}", wantStatus: http.StatusCreated, wantDays: 7},
		{name: "explicit days", body: `{"expiresInDays":30}`, wantStatus: http.StatusCreated, wantDays: 30},
		{name: "capped at 90 days", body: `{"expiresInDays":400}`, wantStatus: http.StatusCreated, wantDays: 90},
		{name: "zero is rejected", body: `{"expiresInDays":0}`, wantStatus: http.StatusBadRequest},
		{name: "negative is rejected", body: `{"expiresInDays":-3}`, wantStatus: http.StatusBadRequest},
		{name: "unknown field is rejected", body: `{"days":3}`, wantStatus: http.StatusBadRequest},
		{name: "wrong type is rejected", body: `{"expiresInDays":"soon"}`, wantStatus: http.StatusBadRequest},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newAuthFixture(t)
			admin := f.setupAdmin("owner")
			rec := f.do(http.MethodPost, "/api/invites", tt.body, withCookie(admin))
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.wantStatus != http.StatusCreated {
				return
			}
			var resp struct {
				Invite createdInvite `json:"invite"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("parse: %v", err)
			}
			want := f.clock.Now().Add(time.Duration(tt.wantDays) * 24 * time.Hour).UTC().Format(time.RFC3339)
			if resp.Invite.ExpiresAt != want {
				t.Errorf("expiresAt = %q, want %q (%d days)", resp.Invite.ExpiresAt, want, tt.wantDays)
			}
		})
	}
}

func TestAuthRegister(t *testing.T) {
	t.Parallel()
	f := newAuthFixture(t)
	admin := f.setupAdmin("owner")
	inv := f.createInvite(admin, "")

	rec := f.do(http.MethodPost, "/api/auth/register", registerBody(inv.Code, "nina"), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("register status = %d (body %s)", rec.Code, rec.Body.String())
	}
	user := userFromBody(t, rec.Body.Bytes())
	if user["username"] != "nina" || user["role"] != "member" {
		t.Errorf("registered user = %v, want nina with role member", user)
	}
	cookie := sessionCookieFrom(t, rec)

	// The session works, and me reports the member role.
	rec = f.do(http.MethodGet, "/api/auth/me", "", withCookie(cookie))
	if rec.Code != http.StatusOK {
		t.Fatalf("me status = %d", rec.Code)
	}
	if me := userFromBody(t, rec.Body.Bytes()); me["role"] != "member" {
		t.Errorf("me role = %v, want member", me["role"])
	}

	// The member owns exactly one space, "main", and it is usable
	// immediately (its content database exists).
	rec = f.do(http.MethodGet, "/api/spaces", "", withCookie(cookie))
	if rec.Code != http.StatusOK {
		t.Fatalf("spaces status = %d", rec.Code)
	}
	var spacesResp struct {
		Spaces []struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Position int    `json:"position"`
		} `json:"spaces"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &spacesResp); err != nil {
		t.Fatalf("parse spaces: %v", err)
	}
	if len(spacesResp.Spaces) != 1 || spacesResp.Spaces[0].Name != "main" || spacesResp.Spaces[0].Position != 0 {
		t.Fatalf("spaces = %+v, want exactly one 'main' at position 0", spacesResp.Spaces)
	}
	rec = f.do(http.MethodGet, "/api/spaces/"+spacesResp.Spaces[0].ID+"/state", "", withCookie(cookie))
	if rec.Code != http.StatusOK {
		t.Fatalf("state status = %d (body %s)", rec.Code, rec.Body.String())
	}

	// The admin list shows the claim.
	listed, _ := f.listInvites(admin)
	got := listed[inv.ID]
	if got.Status != "used" || got.UsedBy == nil || *got.UsedBy != "nina" || got.UsedAt == nil {
		t.Errorf("claimed invite = %+v, want used by nina", got)
	}

	// A used code cannot be claimed again, and says only the uniform text.
	rec = f.do(http.MethodPost, "/api/auth/register", registerBody(inv.Code, "otto"), nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("reuse status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid or expired invite code") {
		t.Errorf("reuse body = %s", rec.Body.String())
	}
}

func TestAuthRegisterValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
	}{
		{name: "missing code", body: `{"username":"nina","password":"a very fine password"}`},
		{name: "blank code", body: `{"code":"   ","username":"nina","password":"a very fine password"}`},
		{name: "missing username", body: `{"code":"dz-AAAAA-AAAAA","password":"a very fine password"}`},
		{name: "short password", body: `{"code":"dz-AAAAA-AAAAA","username":"nina","password":"nine char"}`},
		{name: "malformed JSON", body: `{"code":`},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newAuthFixture(t)
			f.setupAdmin("owner")
			rec := f.do(http.MethodPost, "/api/auth/register", tt.body, nil)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
			}
		})
	}
}

// TestAuthRegisterUniformInviteFailure proves registration is not an
// invite-state oracle: garbage, expired, revoked, and used codes all
// fail with byte-identical bodies — even when the username is taken,
// since the code is judged first.
func TestAuthRegisterUniformInviteFailure(t *testing.T) {
	t.Parallel()
	f := newAuthFixture(t, WithRateLimiter(auth.NewRateLimiter(auth.WithLimit(1000))))
	admin := f.setupAdmin("owner")

	used := f.createInvite(admin, "")
	if rec := f.do(http.MethodPost, "/api/auth/register", registerBody(used.Code, "nina"), nil); rec.Code != http.StatusOK {
		t.Fatalf("claim setup register status = %d (body %s)", rec.Code, rec.Body.String())
	}
	revoked := f.createInvite(admin, "")
	if rec := f.do(http.MethodDelete, "/api/invites/"+revoked.ID, "", withCookie(admin)); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke status = %d", rec.Code)
	}
	expired := f.createInvite(admin, `{"expiresInDays":1}`)
	f.clock.Advance(48 * time.Hour)

	attempts := []struct {
		name     string
		code     string
		username string
	}{
		{name: "garbage code", code: "dz-AAAAA-AAAAA", username: "fresh1"},
		{name: "expired code", code: expired.Code, username: "fresh2"},
		{name: "revoked code", code: revoked.Code, username: "fresh3"},
		{name: "used code", code: used.Code, username: "fresh4"},
		{name: "bad code with taken username", code: "dz-AAAAA-AAAAB", username: "nina"},
		{name: "used code with taken username", code: used.Code, username: "nina"},
	}
	var firstBody string
	for i, at := range attempts {
		rec := f.do(http.MethodPost, "/api/auth/register", registerBody(at.code, at.username), nil)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s: status = %d, want 403 (body %s)", at.name, rec.Code, rec.Body.String())
		}
		if i == 0 {
			firstBody = rec.Body.String()
			if !strings.Contains(firstBody, "invalid or expired invite code") {
				t.Errorf("%s: body = %s", at.name, firstBody)
			}
		} else if rec.Body.String() != firstBody {
			t.Errorf("%s: body %q differs from first rejection %q (invite-state oracle)",
				at.name, rec.Body.String(), firstBody)
		}
		// No half-registered users.
		if at.username != "nina" {
			rec := f.do(http.MethodPost, "/api/auth/login",
				fmt.Sprintf(`{"username":%q,"password":"a very fine password"}`, at.username), nil)
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("%s: refused register still created a loggable user (login = %d)", at.name, rec.Code)
			}
		}
	}
}

// TestAuthRegisterCodeCaseInsensitive pins that a code survives being
// retyped, not just pasted: claims match the canonical rendering
// whatever case (or stray padding) the registrant's keyboard produced.
func TestAuthRegisterCodeCaseInsensitive(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		rewrite func(code string) string
	}{
		{name: "all lowercase", rewrite: strings.ToLower},
		{name: "all uppercase including the dz tag", rewrite: strings.ToUpper},
		{name: "padded with whitespace", rewrite: func(c string) string { return "  " + c + "\t" }},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newAuthFixture(t)
			admin := f.setupAdmin("owner")
			inv := f.createInvite(admin, "")
			rec := f.do(http.MethodPost, "/api/auth/register", registerBody(tt.rewrite(inv.Code), "nina"), nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("register with rewritten code status = %d, want 200 (body %s)",
					rec.Code, rec.Body.String())
			}
			// The rewritten form claimed the same invite, not a phantom.
			listed, _ := f.listInvites(admin)
			if got := listed[inv.ID]; got.Status != "used" || got.UsedBy == nil || *got.UsedBy != "nina" {
				t.Errorf("invite after rewritten claim = %+v, want used by nina", got)
			}
		})
	}
}

// TestAuthRegisterCompensation breaks the spaces directory so the new
// member's content database cannot be created: registration must answer
// 500 while leaving no loggable account behind and the invite unburned
// — and once the fault clears, that same code must mint exactly one
// account. This exercises the single-transaction unwind end to end.
func TestAuthRegisterCompensation(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("running as root: directory permissions cannot block writes")
	}
	f := newAuthFixture(t)
	admin := f.setupAdmin("owner")
	inv := f.createInvite(admin, "")

	spacesDir := filepath.Join(f.dir, "spaces")
	if err := os.Chmod(spacesDir, 0o500); err != nil {
		t.Fatalf("chmod spaces dir: %v", err)
	}
	t.Cleanup(func() {
		// TempDir cleanup needs the write bit back on every exit path.
		if err := os.Chmod(spacesDir, 0o700); err != nil {
			t.Errorf("restore spaces dir: %v", err)
		}
	})

	rec := f.do(http.MethodPost, "/api/auth/register", registerBody(inv.Code, "nina"), nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("register with broken spaces dir status = %d, want 500 (body %s)",
			rec.Code, rec.Body.String())
	}
	// Compensation unwound the account: nothing to log in to...
	rec = f.do(http.MethodPost, "/api/auth/login", `{"username":"nina","password":"a very fine password"}`, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("login after failed register status = %d, want 401 (half-made account survived)", rec.Code)
	}
	// ...and the invite is claimable again, not burned by a server fault.
	listed, _ := f.listInvites(admin)
	if got := listed[inv.ID]; got.Status != "active" || got.UsedBy != nil {
		t.Errorf("invite after failed register = %+v, want active and unclaimed", got)
	}

	if err := os.Chmod(spacesDir, 0o700); err != nil {
		t.Fatalf("clear spaces dir fault: %v", err)
	}
	rec = f.do(http.MethodPost, "/api/auth/register", registerBody(inv.Code, "nina"), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("register after fault cleared status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	// One invite, one account — even across the failure and retry.
	rec = f.do(http.MethodPost, "/api/auth/register", registerBody(inv.Code, "otto"), nil)
	if rec.Code != http.StatusForbidden {
		t.Errorf("code reuse after recovery status = %d, want 403", rec.Code)
	}
}

// TestAuthRegisterUsernameProbingRateLimited pins the bound on the
// deliberate 409 disclosure (see handleAuthRegister): probing usernames
// with one valid code spends the shared credential budget like any
// other attempt, and the probes leave the invite unclaimed.
func TestAuthRegisterUsernameProbingRateLimited(t *testing.T) {
	t.Parallel()
	f := newAuthFixture(t)
	admin := f.setupAdmin("owner") // spends attempt 1 of the 10 budget
	inv := f.createInvite(admin, "")

	for i := 0; i < 9; i++ { // attempts 2..10: allowed, each answering 409
		rec := f.do(http.MethodPost, "/api/auth/register", registerBody(inv.Code, "owner"), nil)
		if rec.Code != http.StatusConflict {
			t.Fatalf("probe %d status = %d, want 409 (body %s)", i+1, rec.Code, rec.Body.String())
		}
	}
	rec := f.do(http.MethodPost, "/api/auth/register", registerBody(inv.Code, "owner"), nil)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("probe 10 status = %d, want 429 — username probing must spend the credential budget", rec.Code)
	}
	// Every probe rolled back: the invite is still active and unclaimed.
	listed, _ := f.listInvites(admin)
	if got := listed[inv.ID]; got.Status != "active" || got.UsedBy != nil {
		t.Errorf("invite after probing = %+v, want active and unclaimed", got)
	}
}

func TestAuthRegisterUsernameTaken(t *testing.T) {
	t.Parallel()
	f := newAuthFixture(t)
	admin := f.setupAdmin("owner")
	inv := f.createInvite(admin, "")

	rec := f.do(http.MethodPost, "/api/auth/register", registerBody(inv.Code, "owner"), nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "username is already taken") {
		t.Errorf("body = %s", rec.Body.String())
	}

	// The failed attempt must not burn the invite.
	rec = f.do(http.MethodPost, "/api/auth/register", registerBody(inv.Code, "nina"), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("retry status = %d, want 200 — the 409 burned the invite (body %s)", rec.Code, rec.Body.String())
	}
}

// TestAuthRegisterDoubleClaimRace fires concurrent registrations at one
// code: exactly one may win, however the race lands, and exactly one
// account may exist afterwards.
func TestAuthRegisterDoubleClaimRace(t *testing.T) {
	t.Parallel()
	f := newAuthFixture(t, WithRateLimiter(auth.NewRateLimiter(auth.WithLimit(1000))))
	admin := f.setupAdmin("owner")
	inv := f.createInvite(admin, "")

	const racers = 8
	codes := make([]int, racers)
	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		i := i // capture (golangci-lint predates Go 1.22 loopvar)
		wg.Add(1)
		go func() {
			defer wg.Done()
			body := registerBody(inv.Code, fmt.Sprintf("racer%d", i))
			codes[i] = f.do(http.MethodPost, "/api/auth/register", body, nil).Code
		}()
	}
	wg.Wait()

	okCount, forbiddenCount := 0, 0
	for i, c := range codes {
		switch c {
		case http.StatusOK:
			okCount++
		case http.StatusForbidden:
			forbiddenCount++
		default:
			t.Errorf("racer %d: unexpected status %d", i, c)
		}
	}
	if okCount != 1 || forbiddenCount != racers-1 {
		t.Fatalf("got %d OK / %d forbidden, want exactly 1 / %d (codes %v)", okCount, forbiddenCount, racers-1, codes)
	}
	// Exactly one racer account exists, and the invite shows one claim.
	registered := 0
	for i := 0; i < racers; i++ {
		body := fmt.Sprintf(`{"username":"racer%d","password":"a very fine password"}`, i)
		if rec := f.do(http.MethodPost, "/api/auth/login", body, nil); rec.Code == http.StatusOK {
			registered++
		}
	}
	if registered != 1 {
		t.Errorf("registered accounts = %d, want exactly 1", registered)
	}
	listed, _ := f.listInvites(admin)
	if got := listed[inv.ID]; got.Status != "used" || got.UsedBy == nil {
		t.Errorf("raced invite = %+v, want exactly one recorded claim", got)
	}
}

func TestAuthRegisterRateLimited(t *testing.T) {
	t.Parallel()
	f := newAuthFixture(t)
	body := registerBody("dz-AAAAA-AAAAA", "nina")
	for i := 0; i < 10; i++ {
		if rec := f.do(http.MethodPost, "/api/auth/register", body, nil); rec.Code != http.StatusForbidden {
			t.Fatalf("attempt %d status = %d, want 403", i+1, rec.Code)
		}
	}
	rec := f.do(http.MethodPost, "/api/auth/register", body, nil)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("attempt 11 status = %d, want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("429 without Retry-After")
	}
	// Register shares the login/setup budget: a blocked guesser cannot
	// pivot to credential guessing.
	rec = f.do(http.MethodPost, "/api/auth/login", `{"username":"ghost","password":"guess guess guess"}`, nil)
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("login while blocked status = %d, want 429", rec.Code)
	}
	// The window slides open again.
	f.clock.Advance(5*time.Minute + time.Second)
	if rec := f.do(http.MethodPost, "/api/auth/register", body, nil); rec.Code != http.StatusForbidden {
		t.Errorf("attempt after window status = %d, want 403 again", rec.Code)
	}
}

// TestInviteCodeNeverLogged runs the full invite lifecycle against a
// capturing logger and asserts the plaintext code appears nowhere in
// the log stream (only its dz- prefix group may, via nothing — even the
// prefix is never logged today).
func TestInviteCodeNeverLogged(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	f := newAuthFixture(t, WithLogger(log.New(&buf, "", 0)))
	admin := f.setupAdmin("owner")

	inv := f.createInvite(admin, "")
	f.listInvites(admin)
	if rec := f.do(http.MethodPost, "/api/auth/register", registerBody(inv.Code, "nina"), nil); rec.Code != http.StatusOK {
		t.Fatalf("register status = %d", rec.Code)
	}
	// A failing attempt too: error paths must not log the code either.
	if rec := f.do(http.MethodPost, "/api/auth/register", registerBody(inv.Code, "otto"), nil); rec.Code != http.StatusForbidden {
		t.Fatalf("reuse status = %d", rec.Code)
	}
	if rec := f.do(http.MethodDelete, "/api/invites/"+inv.ID, "", withCookie(admin)); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke status = %d", rec.Code)
	}

	logs := buf.String()
	if logs == "" {
		t.Fatal("logger captured nothing; the request log should be here")
	}
	if strings.Contains(logs, inv.Code) {
		t.Errorf("logs contain the plaintext invite code:\n%s", logs)
	}
	if strings.Contains(logs, inv.Code[auth.InviteCodePrefixLen+1:]) {
		t.Errorf("logs contain the code's secret second group:\n%s", logs)
	}
}

// TestRegisterMethodNotAllowed pins the 405 surface of the new routes.
func TestInviteRouteMethodFallbacks(t *testing.T) {
	t.Parallel()
	f := newAuthFixture(t)
	admin := f.setupAdmin("owner")
	tests := []struct {
		method string
		path   string
		cookie *http.Cookie
	}{
		{method: http.MethodGet, path: "/api/auth/register"},
		{method: http.MethodPut, path: "/api/invites", cookie: admin},
		{method: http.MethodPatch, path: "/api/invites/inv-x", cookie: admin},
	}
	for _, tt := range tests {
		var mod func(*http.Request)
		if tt.cookie != nil {
			mod = withCookie(tt.cookie)
		}
		rec := f.do(tt.method, tt.path, "", mod)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s status = %d, want 405", tt.method, tt.path, rec.Code)
		}
		if rec.Header().Get("Allow") == "" {
			t.Errorf("%s %s: missing Allow header", tt.method, tt.path)
		}
	}
}

// TestCreateInviteWithEmail covers the email path added to invite creation: a
// valid address is validated, the invite is sent through the email channel
// with the code and a join link, and the recipient is recorded on the invite.
func TestCreateInviteWithEmail(t *testing.T) {
	t.Parallel()
	email := &recordingSender{channel: notify.ChannelEmail}
	f := newAuthFixture(t,
		WithNotifiers(notify.NewRegistry(email)),
		WithPublicURL("https://donezo.example/"),
	)
	admin := f.setupAdmin("owner")

	inv := f.createInvite(admin, `{"email":"nina@example.com"}`)
	if !inv.Sent {
		t.Errorf("response did not report the invite as sent: %+v", inv)
	}
	if inv.Email != "nina@example.com" {
		t.Errorf("response email = %q, want nina@example.com", inv.Email)
	}

	sent := email.deliveries()
	if len(sent) != 1 {
		t.Fatalf("delivered %d messages, want 1", len(sent))
	}
	if sent[0].to != "nina@example.com" {
		t.Errorf("delivered to %q, want nina@example.com", sent[0].to)
	}
	// The body must carry the code and the join link (fragment form keeps the
	// code out of server logs and referrers).
	if !strings.Contains(sent[0].msg.Body, inv.Code) {
		t.Errorf("email body missing the code:\n%s", sent[0].msg.Body)
	}
	wantLink := "https://donezo.example/#/join/" + inv.Code
	if !strings.Contains(sent[0].msg.Body, wantLink) {
		t.Errorf("email body missing the join link %q:\n%s", wantLink, sent[0].msg.Body)
	}

	// The recipient is recorded on the invite listing.
	listed, _ := f.listInvites(admin)
	got := listed[inv.ID]
	if got.Email == nil || *got.Email != "nina@example.com" {
		t.Errorf("listed invite email = %v, want nina@example.com", got.Email)
	}
}

// TestCreateInviteEmailNotConfigured: asking to email an invite on an instance
// with no email channel is refused up front, and mints nothing.
func TestCreateInviteEmailNotConfigured(t *testing.T) {
	t.Parallel()
	f := newAuthFixture(t) // no notifiers
	admin := f.setupAdmin("owner")

	rec := f.do(http.MethodPost, "/api/invites", `{"email":"nina@example.com"}`, withCookie(admin))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}
	if listed, _ := f.listInvites(admin); len(listed) != 0 {
		t.Errorf("a refused email invite still minted a code: %+v", listed)
	}
}

// TestCreateInviteInvalidEmail: a malformed address is a 400 and mints nothing,
// even when the channel is configured.
func TestCreateInviteInvalidEmail(t *testing.T) {
	t.Parallel()
	email := &recordingSender{channel: notify.ChannelEmail}
	f := newAuthFixture(t, WithNotifiers(notify.NewRegistry(email)))
	admin := f.setupAdmin("owner")

	rec := f.do(http.MethodPost, "/api/invites", `{"email":"not-an-email"}`, withCookie(admin))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	if got := len(email.deliveries()); got != 0 {
		t.Errorf("a rejected address still sent %d messages", got)
	}
	if listed, _ := f.listInvites(admin); len(listed) != 0 {
		t.Errorf("an invalid-email invite still minted a code: %+v", listed)
	}
}

// TestCreateInviteWithoutEmailStillMintsBareCode: the original code-only path
// is unchanged — no email field means no send and no recorded recipient.
func TestCreateInviteWithoutEmailStillMintsBareCode(t *testing.T) {
	t.Parallel()
	email := &recordingSender{channel: notify.ChannelEmail}
	f := newAuthFixture(t, WithNotifiers(notify.NewRegistry(email)))
	admin := f.setupAdmin("owner")

	inv := f.createInvite(admin, "")
	if inv.Sent || inv.Email != "" {
		t.Errorf("a bare code reported email state: %+v", inv)
	}
	if got := len(email.deliveries()); got != 0 {
		t.Errorf("a bare code sent %d emails", got)
	}
	listed, _ := f.listInvites(admin)
	if got := listed[inv.ID].Email; got != nil {
		t.Errorf("bare invite listed email = %v, want nil", got)
	}
}

// TestCreateInviteEmailSendFailureKeepsCode: when the email channel accepts the
// address but the send itself fails, the invite is still created and its code
// returned (with a warning), and the recipient is still recorded — so a relay
// hiccup never loses an invite the admin can hand over by hand.
func TestCreateInviteEmailSendFailureKeepsCode(t *testing.T) {
	t.Parallel()
	email := &recordingSender{channel: notify.ChannelEmail, err: errors.New("relay refused")}
	f := newAuthFixture(t, WithNotifiers(notify.NewRegistry(email)), WithPublicURL("https://donezo.example"))
	admin := f.setupAdmin("owner")

	inv := f.createInvite(admin, `{"email":"nina@example.com"}`)
	if inv.Code == "" || !renderedInviteCode.MatchString(inv.Code) {
		t.Errorf("failed send did not still return a usable code: %+v", inv)
	}
	if inv.Sent {
		t.Errorf("sent = true despite the relay refusing")
	}
	if inv.Warning == "" {
		t.Errorf("a failed send should carry a warning, got none: %+v", inv)
	}
	// The invite (with its recipient) survives the send failure.
	listed, _ := f.listInvites(admin)
	got := listed[inv.ID]
	if got.Email == nil || *got.Email != "nina@example.com" {
		t.Errorf("failed-send invite listed email = %v, want nina@example.com", got.Email)
	}
	if got.Status != "active" {
		t.Errorf("failed-send invite status = %q, want active", got.Status)
	}
}
