package api

import (
	"net/http"
	"net/netip"
	"testing"
)

// Finding #1: with --trust-proxy on, X-Forwarded-For must be honoured only
// when the socket peer is actually the proxy (loopback or a trusted CIDR). A
// direct connection from an untrusted peer that forges the header must NOT be
// able to mint a fresh rate-limit key per request and bypass the credential
// limiter.
//
// This is the exploit the fix closes: reverting peerIsTrustedProxy to a bare
// `if s.trustProxy` lets the rotating forged header reset the budget, so this
// test fails against the vulnerable code.
func TestForwardedForFromUntrustedPeerDoesNotBypassLimiter(t *testing.T) {
	f := newAuthFixture(t, WithTrustProxy(true))

	// The attacker connects directly to the port (not through the proxy), so
	// the socket peer is a public address, and rotates X-Forwarded-For every
	// request to try to look like a new client each time.
	attacker := func(nth int) func(*http.Request) {
		return func(r *http.Request) {
			r.RemoteAddr = "203.0.113.50:5555" // not loopback, not trusted
			r.Header.Set("X-Forwarded-For", "10.0.0."+itoa(nth))
		}
	}
	var last int
	for i := 0; i < 10; i++ {
		rec := f.do(http.MethodPost, "/api/auth/login",
			`{"username":"ghost","password":"nope nope nope"}`, attacker(i))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d = %d, want 401", i+1, rec.Code)
		}
		last = i
	}
	// The 11th attempt, still rotating the forged header, must be blocked:
	// the untrusted peer's key is its socket address, unchanged by the header.
	rec := f.do(http.MethodPost, "/api/auth/login",
		`{"username":"ghost","password":"nope nope nope"}`, attacker(last+1))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("rotating forged XFF from an untrusted peer = %d, want 429 (limiter must not be bypassable)", rec.Code)
	}
}

// A trusted-proxy CIDR peer's X-Forwarded-For IS honoured, so a real
// off-host proxy still gives per-client rate limiting.
func TestForwardedForFromTrustedCIDRIsHonoured(t *testing.T) {
	proxyNet := netip.MustParsePrefix("192.168.1.0/24")
	f := newAuthFixture(t, WithTrustProxy(true), WithTrustedProxies([]netip.Prefix{proxyNet}))

	// Requests arrive from the trusted proxy at 192.168.1.10, carrying the
	// real client in the last XFF hop.
	fromProxy := func(client string) func(*http.Request) {
		return func(r *http.Request) {
			r.RemoteAddr = "192.168.1.10:4444"
			r.Header.Set("X-Forwarded-For", client)
		}
	}
	for i := 0; i < 10; i++ {
		if rec := f.do(http.MethodPost, "/api/auth/login",
			`{"username":"ghost","password":"nope nope nope"}`, fromProxy("203.0.113.9")); rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d = %d, want 401", i+1, rec.Code)
		}
	}
	if rec := f.do(http.MethodPost, "/api/auth/login",
		`{"username":"ghost","password":"nope nope nope"}`, fromProxy("203.0.113.9")); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("11th from same client behind trusted proxy = %d, want 429", rec.Code)
	}
	// A different real client behind the same proxy keeps its own budget.
	if rec := f.do(http.MethodPost, "/api/auth/login",
		`{"username":"ghost","password":"nope nope nope"}`, fromProxy("198.51.100.7")); rec.Code != http.StatusUnauthorized {
		t.Fatalf("different client behind trusted proxy = %d, want 401", rec.Code)
	}
}

// Finding #2: a bodied request without Content-Type: application/json is
// refused. This is what takes the cookie-authenticated routes out of the CORS
// "simple request" set, so a cross-origin form POST cannot drive them.
func TestBodiedRequestsRequireJSONContentType(t *testing.T) {
	f := newAuthFixture(t)
	f.credentialUser("ben", "a very fine password")

	// A text/plain login body — the CSRF-simple-request shape — is rejected.
	textPlain := func(r *http.Request) { r.Header.Set("Content-Type", "text/plain;charset=UTF-8") }
	rec := f.do(http.MethodPost, "/api/auth/login",
		`{"username":"ben","password":"a very fine password"}`, textPlain)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("text/plain login = %d, want 415", rec.Code)
	}

	// So is a missing Content-Type.
	noType := func(r *http.Request) { r.Header.Del("Content-Type") }
	rec = f.do(http.MethodPost, "/api/auth/login",
		`{"username":"ben","password":"a very fine password"}`, noType)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("no Content-Type login = %d, want 415", rec.Code)
	}

	// application/json (what the frontend sends, charset included) is accepted.
	withCharset := func(r *http.Request) { r.Header.Set("Content-Type", "application/json; charset=utf-8") }
	rec = f.do(http.MethodPost, "/api/auth/login",
		`{"username":"ben","password":"a very fine password"}`, withCharset)
	if rec.Code != http.StatusOK {
		t.Fatalf("application/json login = %d, want 200 (body %s)", rec.Code, rec.Body)
	}
}

// itoa avoids pulling strconv into the test for one small use.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
