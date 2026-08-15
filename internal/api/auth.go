package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/bgrewell/donezo/internal/auth"
	"github.com/bgrewell/donezo/internal/store"
)

// loginFailedMessage is deliberately identical for unknown usernames,
// wrong passwords, and accounts that have no password yet, so login
// responses never reveal which usernames exist.
const loginFailedMessage = "invalid username or password"

// maxAuthBodyBytes bounds credential request bodies; usernames and
// passwords fit in far less.
const maxAuthBodyBytes = 1 << 16

// handleAuthStatus reports whether first-run setup is still open and
// whether this request is authenticated. It is public so the frontend
// can decide between the setup, login, and app screens.
func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	credentialed, err := s.core.HasCredentialedUser(r.Context())
	if err != nil {
		s.logger.Printf("auth status: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	_, authenticated := userFrom(r.Context())
	writeJSON(w, http.StatusOK, map[string]bool{
		"needsSetup":    !credentialed,
		"authenticated": authenticated,
	})
}

// handleAuthSetup performs first-run setup: it creates the owner
// account and issues a session. Setup stays open until some user has a
// password; after that it always answers 409. When the requested
// username already exists without a password — the seeded "ben" user —
// setup claims that account instead of erroring, since --seed followed
// by setup is the expected first run.
func (s *Server) handleAuthSetup(w http.ResponseWriter, r *http.Request) {
	if !s.allowAttempt(w, r) {
		return
	}
	var req struct {
		Username    string `json:"username"`
		DisplayName string `json:"displayName"`
		Password    string `json:"password"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if err := validateUsername(req.Username); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := auth.ValidatePassword(req.Password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.DisplayName == "" {
		req.DisplayName = req.Username
	}
	// Fast path: once setup is complete, answer 409 before burning an
	// argon2 hash. This check is advisory only — SetupOwner re-enforces
	// the invariant atomically, so concurrent setups past this point
	// still produce exactly one owner.
	credentialed, err := s.core.HasCredentialedUser(r.Context())
	if err != nil {
		s.logger.Printf("setup: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if credentialed {
		writeError(w, http.StatusConflict, "setup is already complete; log in instead")
		return
	}
	hash, err := s.passwords.Hash(req.Password)
	if err != nil {
		s.logger.Printf("setup: hash password: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// SetupOwner claims the seeded password-less row when the username
	// matches, creates the owner otherwise, and refuses — atomically, at
	// the SQL layer — when any credentialed user already exists.
	user, err := s.core.SetupOwner(r.Context(), req.Username, req.DisplayName, hash)
	switch {
	case errors.Is(err, store.ErrSetupComplete):
		// Lost the race against a concurrent setup: same answer as the
		// fast path above.
		writeError(w, http.StatusConflict, "setup is already complete; log in instead")
		return
	case err != nil:
		s.logger.Printf("setup: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.issueSession(w, r, user)
}

// handleAuthLogin verifies credentials and issues a session cookie.
// Every failure mode answers the same 401, and unknown or password-less
// accounts still burn one argon2 verification so the paths cannot be
// told apart by timing.
func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if !s.allowAttempt(w, r) {
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	user, err := s.core.GetUserByUsername(r.Context(), req.Username)
	if errors.Is(err, store.ErrNotFound) || (err == nil && user.PasswordHash == "") {
		// Unknown username, or a seeded account that has not completed
		// setup: such users must not be able to log in. Verify against
		// DummyHash anyway (result discarded) for timing equalization.
		s.verifyDummy(req.Password)
		writeError(w, http.StatusUnauthorized, loginFailedMessage)
		return
	}
	if err != nil {
		s.logger.Printf("login: look up user: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	ok, err := s.passwords.Verify(user.PasswordHash, req.Password)
	if err != nil {
		// A stored hash that cannot be parsed is a server-side fault,
		// but surfacing it would leak account state: log and refuse
		// uniformly.
		s.logger.Printf("login: verify password for user %d: %v", user.ID, err)
		writeError(w, http.StatusUnauthorized, loginFailedMessage)
		return
	}
	if !ok {
		writeError(w, http.StatusUnauthorized, loginFailedMessage)
		return
	}
	s.issueSession(w, r, user)
}

// handleAuthLogout deletes the request's session row, if any, and
// expires the cookie. It succeeds even without a valid session so a
// half-logged-out client can always converge to logged out.
func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(auth.SessionCookieName); err == nil && cookie.Value != "" {
		err := s.core.DeleteSession(r.Context(), auth.HashToken(cookie.Value))
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			s.logger.Printf("logout: delete session: %v", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}
	http.SetCookie(w, s.expiredSessionCookie(r))
	w.WriteHeader(http.StatusNoContent)
}

// handleAuthMe returns the authenticated user, or 401.
func (s *Server) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	user, ok := userFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	writeJSON(w, http.StatusOK, map[string]store.User{"user": user})
}

// verifyDummy runs one full-cost password verification whose result is
// discarded, equalizing response timing between unknown and known
// usernames.
func (s *Server) verifyDummy(password string) {
	if _, err := s.passwords.Verify(auth.DummyHash, password); err != nil {
		s.logger.Printf("dummy verify: %v", err)
	}
}

// issueSession creates a session row for user, sets the session cookie,
// and answers {user}. Any failure is an internal error: the credentials
// were already accepted.
func (s *Server) issueSession(w http.ResponseWriter, r *http.Request, user store.User) {
	token, tokenHash, err := auth.NewToken()
	if err != nil {
		s.logger.Printf("issue session: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	sess, err := s.core.CreateSession(r.Context(), user.ID, tokenHash, auth.SessionTTL)
	if err != nil {
		s.logger.Printf("issue session: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	expires, err := time.Parse(time.RFC3339, sess.ExpiresAt)
	if err != nil {
		s.logger.Printf("issue session: parse expiry: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	http.SetCookie(w, s.sessionCookie(r, token, expires))
	writeJSON(w, http.StatusOK, map[string]store.User{"user": user})
}

// sessionCookie builds the donezo_session cookie: HttpOnly, SameSite
// Lax, Path /, expiring with the session, and Secure when the request
// arrived over TLS — directly or according to a trusted reverse proxy.
func (s *Server) sessionCookie(r *http.Request, value string, expires time.Time) *http.Cookie {
	return &http.Cookie{
		Name:     auth.SessionCookieName,
		Value:    value,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.requestIsTLS(r),
	}
}

// expiredSessionCookie tells the browser to drop the session cookie
// immediately.
func (s *Server) expiredSessionCookie(r *http.Request) *http.Cookie {
	c := s.sessionCookie(r, "", time.Unix(0, 0).UTC())
	c.MaxAge = -1
	return c
}

// maxUsernameRunes caps a username. Generous; the point is a bound, not a
// short name.
const maxUsernameRunes = 64

// validateUsername accepts a username created at setup or registration: 1–64
// characters with no whitespace and no control characters.
//
// The charset limit is not cosmetic. A username is echoed in the UI and
// written to the request/audit log, so a control character or newline in one
// is both a display hazard and a log-forgery vector; rejecting it at the one
// place a username is chosen is cleaner than escaping it everywhere it is
// later shown.
func validateUsername(u string) error {
	if u == "" {
		return errors.New("username is required")
	}
	if utf8.RuneCountInString(u) > maxUsernameRunes {
		return fmt.Errorf("username must be %d characters or fewer", maxUsernameRunes)
	}
	for _, r := range u {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return errors.New("username must not contain spaces or control characters")
		}
	}
	return nil
}

// requestIsTLS reports whether the request arrived over HTTPS:
// terminated here, or — only from a trusted proxy peer — according to
// X-Forwarded-Proto. The header sits on the same trust boundary as
// X-Forwarded-For: unless the socket peer is the proxy, anything a client
// puts in it is ignored.
func (s *Server) requestIsTLS(r *http.Request) bool {
	return r.TLS != nil || (s.peerIsTrustedProxy(r) && r.Header.Get("X-Forwarded-Proto") == "https")
}

// peerIsTrustedProxy reports whether the request's socket peer is the reverse
// proxy whose forwarded headers may be believed.
//
// --trust-proxy alone is not enough: it says "a proxy is in front", but if
// the listen port is reachable directly (see config.BindAddress), an attacker
// opening their own connection could forge X-Forwarded-For to mint a fresh
// rate-limit key per request and defeat the credential limiter entirely. So
// the headers are honoured only when the connection actually came from the
// proxy — loopback (the same-host case), or a configured trusted-proxy CIDR.
func (s *Server) peerIsTrustedProxy(r *http.Request) bool {
	if !s.trustProxy {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	ip = ip.Unmap()
	if ip.IsLoopback() {
		return true
	}
	for _, n := range s.trustedProxyNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// allowAttempt applies the credential-guessing rate limit. When the
// client is over budget it answers 429 with Retry-After and reports
// false.
func (s *Server) allowAttempt(w http.ResponseWriter, r *http.Request) bool {
	ok, retryAfter := s.limiter.Allow(rateLimitKey(s.clientIP(r)))
	if ok {
		return true
	}
	secs := int(math.Ceil(retryAfter.Seconds()))
	if secs < 1 {
		secs = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	writeError(w, http.StatusTooManyRequests, "too many attempts; please wait and try again")
	return false
}

// clientIP returns the client address for rate limiting: the last
// X-Forwarded-For hop when the server was told to trust its proxy,
// otherwise the socket's remote IP. The last hop is the one the
// trusted proxy itself appended (proxies append the peer address to
// whatever header arrived); every hop left of it came from the
// client's own header and is attacker-chosen, so it is never used.
func (s *Server) clientIP(r *http.Request) string {
	if s.peerIsTrustedProxy(r) {
		// With several X-Forwarded-For header lines the proxy appends to
		// the final one, so the trustworthy hop is the last value of the
		// last line.
		if vals := r.Header.Values("X-Forwarded-For"); len(vals) > 0 {
			hops := strings.Split(vals[len(vals)-1], ",")
			if ip := strings.TrimSpace(hops[len(hops)-1]); ip != "" {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// rateLimitKey canonicalizes a client address into a rate limiter key.
// IPv6 addresses collapse to their /64 prefix: a single client
// routinely controls an entire /64, so keying on exact addresses would
// let it reset the attempt budget 2^64 times for free. IPv4 addresses
// key exactly; strings that parse as neither (including zoned IPv6,
// which cannot be masked) key on their literal value, which still
// isolates the client.
func rateLimitKey(addr string) string {
	ip, err := netip.ParseAddr(addr)
	if err != nil {
		return addr
	}
	ip = ip.Unmap()
	if ip.Is4() {
		return ip.String()
	}
	prefix, err := ip.Prefix(64)
	if err != nil {
		return addr
	}
	return prefix.String()
}

// decodeJSON parses the request body into dst, answering 400 and
// reporting false on malformed or oversized input.
func (s *Server) decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if !requireJSONContentType(w, r) {
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAuthBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}
