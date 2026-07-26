package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bgrewell/donezo/internal/auth"
	"github.com/bgrewell/donezo/internal/store"
)

// testClock is a manually advanced clock shared by the stores, the
// session authenticator, and the rate limiter in auth tests.
type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func newTestClock() *testClock {
	return &testClock{t: fixedClock()}
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// countingHasher is a fast PasswordHasher that counts Verify calls: the
// seam for asserting timing equalization (unknown users must still cost
// one verification) without wall-clock measurements.
type countingHasher struct {
	mu          sync.Mutex
	verifyCalls int
}

func (h *countingHasher) Hash(password string) (string, error) {
	if password == "" {
		return "", fmt.Errorf("empty password")
	}
	return "fake$" + password, nil
}

func (h *countingHasher) Verify(encoded, password string) (bool, error) {
	h.mu.Lock()
	h.verifyCalls++
	h.mu.Unlock()
	return encoded == "fake$"+password, nil
}

func (h *countingHasher) calls() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.verifyCalls
}

// authFixture is a Server wired for session-auth tests: real stores in
// a temp dir, fake hasher, and everything on one advanceable clock.
type authFixture struct {
	t       *testing.T
	handler http.Handler
	core    *store.CoreStore
	hasher  *countingHasher
	clock   *testClock
}

// newAuthFixture builds the fixture; opts are applied after (and can
// override) the test defaults.
func newAuthFixture(t *testing.T, opts ...ServerOption) *authFixture {
	t.Helper()
	dir := t.TempDir()
	clock := newTestClock()
	core, err := store.NewCoreStore(store.WithDataDir(dir), store.WithClock(clock.Now))
	if err != nil {
		t.Fatalf("NewCoreStore: %v", err)
	}
	spaces, err := store.NewSpaceStore(store.WithDataDir(dir), store.WithClock(clock.Now))
	if err != nil {
		t.Fatalf("NewSpaceStore: %v", err)
	}
	t.Cleanup(func() {
		if err := core.Close(); err != nil {
			t.Errorf("close core: %v", err)
		}
		if err := spaces.Close(); err != nil {
			t.Errorf("close spaces: %v", err)
		}
	})
	hasher := &countingHasher{}
	base := []ServerOption{
		WithLogger(log.New(io.Discard, "", 0)),
		WithClock(clock.Now),
		WithPasswordHasher(hasher),
		WithRateLimiter(auth.NewRateLimiter(auth.WithLimiterClock(clock.Now))),
	}
	srv := NewServer(core, spaces, append(base, opts...)...)
	return &authFixture{t: t, handler: srv.Handler(), core: core, hasher: hasher, clock: clock}
}

// seedUser creates a password-less user, mirroring what --seed does.
func (f *authFixture) seedUser(username, displayName string) store.User {
	f.t.Helper()
	u, err := f.core.CreateUser(context.Background(), username, displayName)
	if err != nil {
		f.t.Fatalf("seed user %s: %v", username, err)
	}
	return u
}

// credentialUser creates a user that can log in with password (hashed
// with the fixture's fake hasher scheme).
func (f *authFixture) credentialUser(username, password string) store.User {
	f.t.Helper()
	u := f.seedUser(username, username)
	if err := f.core.SetUserPassword(context.Background(), u.ID, "fake$"+password); err != nil {
		f.t.Fatalf("set password for %s: %v", username, err)
	}
	return u
}

// do performs one request against the fixture's handler. mod, when
// non-nil, adjusts the request (cookies, headers, remote address).
func (f *authFixture) do(method, path, body string, mod func(*http.Request)) *httptest.ResponseRecorder {
	f.t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rdr)
	if mod != nil {
		mod(req)
	}
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	return rec
}

// withCookie returns a request modifier that attaches the cookie.
func withCookie(c *http.Cookie) func(*http.Request) {
	return func(r *http.Request) { r.AddCookie(c) }
}

// sessionCookieFrom extracts the donezo_session Set-Cookie from a
// response, failing the test if absent.
func sessionCookieFrom(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.SessionCookieName {
			return c
		}
	}
	t.Fatalf("no %s cookie in response (headers %v)", auth.SessionCookieName, rec.Header())
	return nil
}

// userFromBody decodes the {"user": {...}} envelope.
func userFromBody(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var resp struct {
		User map[string]any `json:"user"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("parse user envelope: %v (body %s)", err, body)
	}
	if resp.User == nil {
		t.Fatalf("no user in body %s", body)
	}
	return resp.User
}

func TestAuthSetup(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		prep       func(f *authFixture)
		body       string
		wantStatus int
		check      func(t *testing.T, f *authFixture, rec *httptest.ResponseRecorder)
	}{
		{
			name:       "creates owner on empty table",
			body:       `{"username":"alice","displayName":"Alice","password":"a very fine password"}`,
			wantStatus: http.StatusOK,
			check: func(t *testing.T, f *authFixture, rec *httptest.ResponseRecorder) {
				t.Helper()
				user := userFromBody(t, rec.Body.Bytes())
				if user["username"] != "alice" || user["displayName"] != "Alice" {
					t.Errorf("user = %v", user)
				}
				sessionCookieFrom(t, rec) // a session is issued immediately
				stored, err := f.core.GetUserByUsername(context.Background(), "alice")
				if err != nil {
					t.Fatalf("stored user: %v", err)
				}
				if stored.PasswordHash != "fake$a very fine password" {
					t.Errorf("stored hash = %q", stored.PasswordHash)
				}
			},
		},
		{
			name:       "claims the seeded password-less user",
			prep:       func(f *authFixture) { f.seedUser("ben", "Ben") },
			body:       `{"username":"ben","displayName":"Big Ben","password":"a very fine password"}`,
			wantStatus: http.StatusOK,
			check: func(t *testing.T, f *authFixture, rec *httptest.ResponseRecorder) {
				t.Helper()
				user := userFromBody(t, rec.Body.Bytes())
				if user["username"] != "ben" || user["displayName"] != "Big Ben" {
					t.Errorf("user = %v", user)
				}
				stored, err := f.core.GetUserByUsername(context.Background(), "ben")
				if err != nil {
					t.Fatalf("stored user: %v", err)
				}
				if stored.ID != 1 {
					t.Errorf("claimed user id = %d, want the seeded row (1), not a new user", stored.ID)
				}
				if stored.PasswordHash != "fake$a very fine password" {
					t.Errorf("stored hash = %q, seeded user not claimed", stored.PasswordHash)
				}
				if stored.DisplayName != "Big Ben" {
					t.Errorf("display name = %q, want %q", stored.DisplayName, "Big Ben")
				}
			},
		},
		{
			name: "setup keeps working while only password-less users exist",
			prep: func(f *authFixture) { f.seedUser("ben", "Ben") },
			body: `{"username":"alice","displayName":"Alice","password":"a very fine password"}`,
			// A seeded-but-unclaimed dir is still "first run": a different
			// username creates the owner alongside the dormant seed user.
			wantStatus: http.StatusOK,
		},
		{
			name:       "409 once any user has a password",
			prep:       func(f *authFixture) { f.credentialUser("ben", "a very fine password") },
			body:       `{"username":"mallory","displayName":"M","password":"another password"}`,
			wantStatus: http.StatusConflict,
			check: func(t *testing.T, f *authFixture, _ *httptest.ResponseRecorder) {
				t.Helper()
				if _, err := f.core.GetUserByUsername(context.Background(), "mallory"); err == nil {
					t.Error("409 setup still created a user")
				}
			},
		},
		{
			name:       "empty username",
			body:       `{"username":"","displayName":"X","password":"a very fine password"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "empty password",
			body:       `{"username":"alice","displayName":"Alice","password":""}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "password below minimum length",
			body:       `{"username":"alice","displayName":"Alice","password":"nine char"}`,
			wantStatus: http.StatusBadRequest,
			check: func(t *testing.T, _ *authFixture, rec *httptest.ResponseRecorder) {
				t.Helper()
				if !strings.Contains(rec.Body.String(), "at least 10 characters") {
					t.Errorf("body = %s, want calm minimum-length message", rec.Body.String())
				}
			},
		},
		{
			name:       "malformed JSON",
			body:       `{"username":`,
			wantStatus: http.StatusBadRequest,
		},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newAuthFixture(t)
			if tt.prep != nil {
				tt.prep(f)
			}
			rec := f.do(http.MethodPost, "/api/auth/setup", tt.body, nil)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.check != nil {
				tt.check(t, f, rec)
			}
		})
	}
}

// TestAuthSetupConcurrent proves first-run setup is exclusive: however
// many setup requests race on a fresh instance, exactly one wins, every
// loser gets 409, and exactly one credentialed account exists after the
// dust settles. Guards the TOCTOU where concurrent setups each passed
// the has-credentialed-user check before any of them wrote a password.
func TestAuthSetupConcurrent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		prep func(f *authFixture)
	}{
		{name: "fresh instance"},
		{
			// One racer targets the seeded username (the claim path) while
			// the rest create fresh ones; the invariant is identical.
			name: "seeded instance",
			prep: func(f *authFixture) { f.seedUser("ben", "Ben") },
		},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// A permissive limiter so 429s cannot mask the race.
			f := newAuthFixture(t, WithRateLimiter(auth.NewRateLimiter(auth.WithLimit(1000))))
			if tt.prep != nil {
				tt.prep(f)
			}
			const racers = 16
			usernames := make([]string, racers)
			for i := range usernames {
				usernames[i] = fmt.Sprintf("racer%d", i)
			}
			usernames[0] = "ben" // claim path when the seeded user exists
			codes := make([]int, racers)
			var wg sync.WaitGroup
			for i := 0; i < racers; i++ {
				i := i // capture (golangci-lint predates Go 1.22 loopvar)
				wg.Add(1)
				go func() {
					defer wg.Done()
					body := fmt.Sprintf(`{"username":%q,"password":"a very fine password"}`, usernames[i])
					codes[i] = f.do(http.MethodPost, "/api/auth/setup", body, nil).Code
				}()
			}
			wg.Wait()
			okCount, conflictCount := 0, 0
			for i, c := range codes {
				switch c {
				case http.StatusOK:
					okCount++
				case http.StatusConflict:
					conflictCount++
				default:
					t.Errorf("racer %d: unexpected status %d", i, c)
				}
			}
			if okCount != 1 || conflictCount != racers-1 {
				t.Fatalf("got %d OK / %d conflict, want exactly 1 / %d (codes %v)", okCount, conflictCount, racers-1, codes)
			}
			// Exactly one credentialed account, and no half-created losers.
			credentialed := 0
			for _, name := range usernames {
				u, err := f.core.GetUserByUsername(context.Background(), name)
				if err != nil {
					continue // losers must not leave rows behind
				}
				if u.PasswordHash != "" {
					credentialed++
				} else if name != "ben" {
					t.Errorf("loser %q left a password-less user row behind", name)
				}
			}
			if credentialed != 1 {
				t.Errorf("credentialed users = %d, want exactly 1", credentialed)
			}
		})
	}
}

func TestAuthLoginRejections(t *testing.T) {
	t.Parallel()
	f := newAuthFixture(t)
	f.credentialUser("ben", "a very fine password")
	f.seedUser("carol", "Carol") // empty hash: must never log in

	tests := []struct {
		name string
		body string
	}{
		{name: "unknown username", body: `{"username":"ghost","password":"a very fine password"}`},
		{name: "wrong password", body: `{"username":"ben","password":"not the password"}`},
		{name: "empty-hash user with empty password", body: `{"username":"carol","password":""}`},
		{name: "empty-hash user with any password", body: `{"username":"carol","password":"a very fine password"}`},
	}
	var firstBody string
	for i, tt := range tests {
		before := f.hasher.calls()
		rec := f.do(http.MethodPost, "/api/auth/login", tt.body, nil)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s: status = %d, want 401 (body %s)", tt.name, rec.Code, rec.Body.String())
		}
		// Uniform 401: every rejection reads identically, so responses
		// cannot be used as a username oracle.
		if i == 0 {
			firstBody = rec.Body.String()
			if !strings.Contains(firstBody, "invalid username or password") {
				t.Errorf("%s: body = %s", tt.name, firstBody)
			}
		} else if rec.Body.String() != firstBody {
			t.Errorf("%s: body %q differs from first rejection %q (username oracle)", tt.name, rec.Body.String(), firstBody)
		}
		// Timing equalization: every path, including unknown usernames
		// and password-less accounts, costs exactly one Verify call.
		if got := f.hasher.calls() - before; got != 1 {
			t.Errorf("%s: verify calls = %d, want exactly 1", tt.name, got)
		}
	}
}

func TestAuthSessionLifecycle(t *testing.T) {
	t.Parallel()
	f := newAuthFixture(t)
	ben := f.credentialUser("ben", "a very fine password")
	if _, err := f.core.CreateSpace(context.Background(), store.Space{ID: "sandbox", UserID: ben.ID, Name: "Sandbox", Color: "blue"}); err != nil {
		t.Fatalf("create space: %v", err)
	}

	// Login issues a session cookie.
	rec := f.do(http.MethodPost, "/api/auth/login", `{"username":"ben","password":"a very fine password"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d (body %s)", rec.Code, rec.Body.String())
	}
	cookie := sessionCookieFrom(t, rec)
	if cookie.Value == "" {
		t.Fatal("session cookie is empty")
	}

	// The cookie authenticates /api/auth/me and protected endpoints.
	rec = f.do(http.MethodGet, "/api/auth/me", "", withCookie(cookie))
	if rec.Code != http.StatusOK {
		t.Fatalf("me status = %d (body %s)", rec.Code, rec.Body.String())
	}
	if user := userFromBody(t, rec.Body.Bytes()); user["username"] != "ben" {
		t.Errorf("me user = %v", user)
	}
	rec = f.do(http.MethodGet, "/api/spaces/sandbox/state", "", withCookie(cookie))
	if rec.Code != http.StatusOK {
		t.Fatalf("space state status = %d (body %s)", rec.Code, rec.Body.String())
	}

	// Logout deletes the session row and expires the cookie.
	rec = f.do(http.MethodPost, "/api/auth/logout", "", withCookie(cookie))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d (body %s)", rec.Code, rec.Body.String())
	}
	expired := sessionCookieFrom(t, rec)
	if expired.MaxAge >= 0 || expired.Value != "" {
		t.Errorf("logout cookie = MaxAge %d Value %q, want MaxAge < 0 and empty value", expired.MaxAge, expired.Value)
	}

	// The old cookie is dead server-side, not just in the browser.
	rec = f.do(http.MethodGet, "/api/auth/me", "", withCookie(cookie))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("me after logout status = %d, want 401", rec.Code)
	}

	// A fresh session dies when its absolute expiry passes.
	rec = f.do(http.MethodPost, "/api/auth/login", `{"username":"ben","password":"a very fine password"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("re-login status = %d", rec.Code)
	}
	cookie = sessionCookieFrom(t, rec)
	f.clock.Advance(auth.SessionTTL + time.Minute)
	rec = f.do(http.MethodGet, "/api/auth/me", "", withCookie(cookie))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("me with expired session status = %d, want 401", rec.Code)
	}
}

func TestAuthCookieFlags(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		opts       []ServerOption
		mod        func(*http.Request)
		wantSecure bool
	}{
		{name: "plain http", mod: nil, wantSecure: false},
		{
			// X-Forwarded-Proto sits on the same trust boundary as
			// X-Forwarded-For: without --trust-proxy it is client noise.
			name:       "spoofed X-Forwarded-Proto without trust-proxy is ignored",
			mod:        func(r *http.Request) { r.Header.Set("X-Forwarded-Proto", "https") },
			wantSecure: false,
		},
		{
			name:       "behind a trusted TLS-terminating proxy",
			opts:       []ServerOption{WithTrustProxy(true)},
			mod:        func(r *http.Request) { r.Header.Set("X-Forwarded-Proto", "https") },
			wantSecure: true,
		},
		{
			name:       "trusted proxy reporting plain http",
			opts:       []ServerOption{WithTrustProxy(true)},
			mod:        func(r *http.Request) { r.Header.Set("X-Forwarded-Proto", "http") },
			wantSecure: false,
		},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newAuthFixture(t, tt.opts...)
			f.credentialUser("ben", "a very fine password")
			rec := f.do(http.MethodPost, "/api/auth/login", `{"username":"ben","password":"a very fine password"}`, tt.mod)
			if rec.Code != http.StatusOK {
				t.Fatalf("login status = %d (body %s)", rec.Code, rec.Body.String())
			}
			c := sessionCookieFrom(t, rec)
			if !c.HttpOnly {
				t.Error("cookie not HttpOnly")
			}
			if c.Path != "/" {
				t.Errorf("cookie Path = %q, want /", c.Path)
			}
			if c.SameSite != http.SameSiteLaxMode {
				t.Errorf("cookie SameSite = %v, want Lax", c.SameSite)
			}
			if c.Secure != tt.wantSecure {
				t.Errorf("cookie Secure = %v, want %v", c.Secure, tt.wantSecure)
			}
			wantExpiry := f.clock.Now().Add(auth.SessionTTL)
			if !c.Expires.Equal(wantExpiry) {
				t.Errorf("cookie Expires = %v, want %v (session expiry)", c.Expires, wantExpiry)
			}
		})
	}
}

func TestAuthMiddlewareProtection(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
	}{
		{name: "spaces requires auth", method: http.MethodGet, path: "/api/spaces", wantStatus: http.StatusUnauthorized},
		{name: "space state requires auth", method: http.MethodGet, path: "/api/spaces/sandbox/state", wantStatus: http.StatusUnauthorized},
		{name: "unknown api path requires auth", method: http.MethodGet, path: "/api/nope", wantStatus: http.StatusUnauthorized},
		{name: "healthz is public", method: http.MethodGet, path: "/api/healthz", wantStatus: http.StatusOK},
		{name: "auth status is public", method: http.MethodGet, path: "/api/auth/status", wantStatus: http.StatusOK},
		{name: "me is reachable but answers 401 itself", method: http.MethodGet, path: "/api/auth/me", wantStatus: http.StatusUnauthorized},
		{name: "logout without a session still succeeds", method: http.MethodPost, path: "/api/auth/logout", wantStatus: http.StatusNoContent},
		{name: "wrong method on public path is 405", method: http.MethodGet, path: "/api/auth/login", wantStatus: http.StatusMethodNotAllowed},
		{name: "non-api path falls through to 404", method: http.MethodGet, path: "/somewhere", wantStatus: http.StatusNotFound},
		// The auth decision must run on the cleaned path: dot-segments
		// under the public prefix must not smuggle a protected path past
		// the middleware, regardless of how the router downstream would
		// treat the unclean path.
		{name: "dot-segments cannot hide behind the public prefix", method: http.MethodGet, path: "/api/auth/../spaces", wantStatus: http.StatusUnauthorized},
		{name: "double slashes cannot dodge the api prefix", method: http.MethodGet, path: "//api/spaces", wantStatus: http.StatusUnauthorized},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newAuthFixture(t)
			f.credentialUser("ben", "a very fine password") // a user exists; requests just lack a session
			rec := f.do(tt.method, tt.path, "", nil)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestAuthStatusReflectsState(t *testing.T) {
	t.Parallel()
	f := newAuthFixture(t)

	status := func(mod func(*http.Request)) (needsSetup, authenticated bool) {
		t.Helper()
		rec := f.do(http.MethodGet, "/api/auth/status", "", mod)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d (body %s)", rec.Code, rec.Body.String())
		}
		var resp struct {
			NeedsSetup    bool `json:"needsSetup"`
			Authenticated bool `json:"authenticated"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("parse: %v", err)
		}
		return resp.NeedsSetup, resp.Authenticated
	}

	if needs, authed := status(nil); !needs || authed {
		t.Errorf("fresh dir: needsSetup=%v authenticated=%v, want true/false", needs, authed)
	}

	f.seedUser("ben", "Ben") // seeded but password-less: still needs setup
	if needs, _ := status(nil); !needs {
		t.Error("seeded dir: needsSetup = false, want true (empty hash must not count)")
	}

	rec := f.do(http.MethodPost, "/api/auth/setup", `{"username":"ben","displayName":"Ben","password":"a very fine password"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("setup status = %d (body %s)", rec.Code, rec.Body.String())
	}
	cookie := sessionCookieFrom(t, rec)

	if needs, authed := status(nil); needs || authed {
		t.Errorf("after setup, no cookie: needsSetup=%v authenticated=%v, want false/false", needs, authed)
	}
	if needs, authed := status(withCookie(cookie)); needs || !authed {
		t.Errorf("after setup, with cookie: needsSetup=%v authenticated=%v, want false/true", needs, authed)
	}
}

func TestAuthRateLimiting(t *testing.T) {
	t.Parallel()

	login := func(f *authFixture, mod func(*http.Request)) *httptest.ResponseRecorder {
		return f.do(http.MethodPost, "/api/auth/login", `{"username":"ghost","password":"guess guess guess"}`, mod)
	}

	t.Run("blocks the 11th attempt with Retry-After", func(t *testing.T) {
		t.Parallel()
		f := newAuthFixture(t)
		for i := 0; i < 10; i++ {
			if rec := login(f, nil); rec.Code != http.StatusUnauthorized {
				t.Fatalf("attempt %d status = %d, want 401", i+1, rec.Code)
			}
		}
		rec := login(f, nil)
		if rec.Code != http.StatusTooManyRequests {
			t.Fatalf("attempt 11 status = %d, want 429", rec.Code)
		}
		retry, err := strconv.Atoi(rec.Header().Get("Retry-After"))
		if err != nil || retry < 1 {
			t.Errorf("Retry-After = %q, want a positive integer", rec.Header().Get("Retry-After"))
		}
		// Setup shares the same budget: a blocked guesser cannot pivot.
		rec = f.do(http.MethodPost, "/api/auth/setup", `{"username":"x","displayName":"X","password":"a very fine password"}`, nil)
		if rec.Code != http.StatusTooManyRequests {
			t.Errorf("setup while blocked status = %d, want 429", rec.Code)
		}
		// The window slides: after it passes, attempts flow again.
		f.clock.Advance(5*time.Minute + time.Second)
		if rec := login(f, nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("attempt after window status = %d, want 401 again", rec.Code)
		}
	})

	t.Run("isolates clients by IP", func(t *testing.T) {
		t.Parallel()
		f := newAuthFixture(t)
		for i := 0; i < 10; i++ {
			login(f, nil)
		}
		if rec := login(f, nil); rec.Code != http.StatusTooManyRequests {
			t.Fatalf("same-IP attempt status = %d, want 429", rec.Code)
		}
		other := func(r *http.Request) { r.RemoteAddr = "198.51.100.7:4242" }
		if rec := login(f, other); rec.Code != http.StatusUnauthorized {
			t.Errorf("other-IP attempt status = %d, want 401 (per-IP isolation)", rec.Code)
		}
	})

	t.Run("ignores X-Forwarded-For unless trust-proxy is set", func(t *testing.T) {
		t.Parallel()
		f := newAuthFixture(t)
		spoof := func(r *http.Request) { r.Header.Set("X-Forwarded-For", "203.0.113.9") }
		for i := 0; i < 10; i++ {
			login(f, nil)
		}
		if rec := login(f, spoof); rec.Code != http.StatusTooManyRequests {
			t.Errorf("spoofed-XFF attempt status = %d, want 429 (header must be ignored)", rec.Code)
		}
	})

	t.Run("keys on last X-Forwarded-For hop with trust-proxy", func(t *testing.T) {
		t.Parallel()
		f := newAuthFixture(t, WithTrustProxy(true))
		// Reverse proxies append the peer they observed to any header the
		// client sent, so only the rightmost hop is trustworthy; everything
		// left of it is attacker-chosen.
		proxied := func(forged, observed string) func(*http.Request) {
			return func(r *http.Request) { r.Header.Set("X-Forwarded-For", forged+", "+observed) }
		}
		for i := 0; i < 10; i++ {
			// A forged first hop that rotates on every attempt must not
			// reset the budget of the real client behind it.
			if rec := login(f, proxied(fmt.Sprintf("6.6.6.%d", i), "203.0.113.9")); rec.Code != http.StatusUnauthorized {
				t.Fatalf("attempt %d status = %d, want 401", i+1, rec.Code)
			}
		}
		if rec := login(f, proxied("6.6.6.250", "203.0.113.9")); rec.Code != http.StatusTooManyRequests {
			t.Fatalf("rotating forged first hop status = %d, want 429 (limiter bypass)", rec.Code)
		}
		// A genuinely different client — different proxy-observed hop —
		// keeps its own budget.
		if rec := login(f, proxied("6.6.6.6", "198.51.100.7")); rec.Code != http.StatusUnauthorized {
			t.Errorf("different observed hop status = %d, want 401", rec.Code)
		}
		if rec := login(f, nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("no-XFF (socket address) status = %d, want 401", rec.Code)
		}
	})

	t.Run("collapses IPv6 clients to their /64 prefix", func(t *testing.T) {
		t.Parallel()
		f := newAuthFixture(t)
		from := func(addr string) func(*http.Request) {
			return func(r *http.Request) { r.RemoteAddr = addr }
		}
		// A single IPv6 client typically controls an entire /64: rotating
		// the interface half on every attempt must not reset the budget.
		for i := 0; i < 10; i++ {
			addr := fmt.Sprintf("[2001:db8:1:2:%x::1]:4242", i+1)
			if rec := login(f, from(addr)); rec.Code != http.StatusUnauthorized {
				t.Fatalf("attempt %d status = %d, want 401", i+1, rec.Code)
			}
		}
		if rec := login(f, from("[2001:db8:1:2:ffff::9]:4242")); rec.Code != http.StatusTooManyRequests {
			t.Fatalf("same-/64 attempt status = %d, want 429 (address rotation bypass)", rec.Code)
		}
		if rec := login(f, from("[2001:db8:9:9::1]:4242")); rec.Code != http.StatusUnauthorized {
			t.Errorf("different-/64 attempt status = %d, want 401 (own budget)", rec.Code)
		}
	})
}
