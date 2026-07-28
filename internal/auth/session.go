package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/bgrewell/donezo/internal/store"
)

// SessionCookieName is the browser cookie that carries the session
// token.
const SessionCookieName = "donezo_session"

// SessionTTL is the absolute session lifetime, measured from creation.
// Sessions are not extended by use; after 30 days the user signs in
// again.
const SessionTTL = 30 * 24 * time.Hour

// touchInterval caps how often a session's last_seen_at is rewritten:
// at most once per minute, so routine polling does not turn every
// request into a write.
const touchInterval = time.Minute

// ErrUnauthenticated reports that a request carries no valid session:
// no cookie, an unknown token, or an expired session. The API layer
// turns it into a 401 without logging; any other authentication error
// is an internal fault and gets logged.
var ErrUnauthenticated = errors.New("auth: unauthenticated")

// SessionStore is the slice of the core store the session authenticator
// needs.
type SessionStore interface {
	// GetSessionUser resolves a stored token hash to its session row
	// and owning user, or store.ErrNotFound.
	GetSessionUser(ctx context.Context, tokenHash string) (store.Session, store.User, error)
	// TouchSession records that the session was just used.
	TouchSession(ctx context.Context, tokenHash string) error
}

// SessionAuthenticator authenticates requests from the donezo_session
// cookie: the cookie value is hashed with SHA-256 and looked up in
// core.db, expired sessions are rejected, and last_seen_at is refreshed
// at most once per minute. It is donezod's default Authenticator.
type SessionAuthenticator struct {
	sessions SessionStore
	clock    func() time.Time
}

// SessionOption configures a SessionAuthenticator (functional options
// pattern).
type SessionOption func(*SessionAuthenticator)

// WithSessionClock overrides the time source used for expiry checks.
// Defaults to time.Now; deterministic tests inject a fixed clock.
func WithSessionClock(clock func() time.Time) SessionOption {
	return func(a *SessionAuthenticator) {
		if clock != nil {
			a.clock = clock
		}
	}
}

// NewSessionAuthenticator builds a SessionAuthenticator over sessions,
// which must be non-nil.
func NewSessionAuthenticator(sessions SessionStore, opts ...SessionOption) *SessionAuthenticator {
	a := &SessionAuthenticator{sessions: sessions, clock: time.Now}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// Authenticate resolves the request's session cookie to a user. Missing
// cookies, unknown tokens, and expired sessions all return
// ErrUnauthenticated; other errors indicate store faults.
func (a *SessionAuthenticator) Authenticate(r *http.Request) (store.User, error) {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil || cookie.Value == "" {
		return store.User{}, ErrUnauthenticated
	}
	tokenHash := HashToken(cookie.Value)
	sess, user, err := a.sessions.GetSessionUser(r.Context(), tokenHash)
	if errors.Is(err, store.ErrNotFound) {
		return store.User{}, ErrUnauthenticated
	}
	if err != nil {
		return store.User{}, fmt.Errorf("auth: look up session: %w", err)
	}
	expires, err := time.Parse(time.RFC3339, sess.ExpiresAt)
	if err != nil {
		return store.User{}, fmt.Errorf("auth: malformed session expiry %q: %w", sess.ExpiresAt, err)
	}
	now := a.clock()
	if !now.Before(expires) {
		return store.User{}, ErrUnauthenticated
	}
	if shouldTouch(sess, now) {
		// Best effort: last_seen_at is advisory bookkeeping, and a
		// failed write must not reject an otherwise valid session.
		_ = a.sessions.TouchSession(r.Context(), tokenHash)
	}
	return user, nil
}

// shouldTouch reports whether the session's last_seen_at is absent,
// unreadable, or older than touchInterval.
func shouldTouch(sess store.Session, now time.Time) bool {
	if sess.LastSeenAt == nil {
		return true
	}
	last, err := time.Parse(time.RFC3339, *sess.LastSeenAt)
	if err != nil {
		return true
	}
	return now.Sub(last) >= touchInterval
}
