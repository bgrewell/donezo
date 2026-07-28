package api

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/bgrewell/donezo/internal/auth"
	"github.com/bgrewell/donezo/internal/store"
)

// Authenticator resolves the user making a request. The default is
// auth.SessionAuthenticator, which validates the donezo_session cookie
// against core.db's sessions table. StaticAuthenticator remains only as
// a development bypass.
type Authenticator interface {
	// Authenticate returns the requesting user, or an error if the
	// request carries no valid identity. Implementations return an
	// error wrapping auth.ErrUnauthenticated for absent or invalid
	// credentials; any other error is treated as an internal fault and
	// logged, but the request is refused either way.
	Authenticate(r *http.Request) (store.User, error)
}

// StaticAuthenticator attributes every request to one fixed user
// WITHOUT checking anything: it bypasses authentication entirely.
//
// It exists only for phase-3 frontend development and tests, behind the
// --dev-auto-login flag, and config validation refuses that flag unless
// the data dir is under /tmp or DONEZOD_I_KNOW_WHAT_IM_DOING=1 is set.
// Never wire it into a real deployment.
type StaticAuthenticator struct {
	// User is attached to every request.
	User store.User
}

// Authenticate implements Authenticator by always returning the fixed
// user.
func (a StaticAuthenticator) Authenticate(*http.Request) (store.User, error) {
	return a.User, nil
}

// userContextKey is the private context key for the authenticated user.
type userContextKey struct{}

// userFrom extracts the authenticated user placed in the request context
// by the auth middleware.
func userFrom(ctx context.Context) (store.User, bool) {
	u, ok := ctx.Value(userContextKey{}).(store.User)
	return u, ok
}

// publicPath reports whether the (cleaned) path is reachable without
// authentication: the liveness probe and the auth endpoints themselves.
// Login, setup, and status must work for logged-out clients;
// /api/auth/me and logout handle their own identity checks.
func publicPath(path string) bool {
	return path == "/api/healthz" || strings.HasPrefix(path, "/api/auth/")
}

// cleanRequestPath normalizes a request path for authorization checks:
// dot-segments and duplicate slashes are resolved so a path like
// /api/auth/../spaces cannot masquerade as public. The auth decision
// must not depend on how the router downstream treats unclean paths
// (today's ServeMux redirects them, but that is its behavior, not this
// middleware's guarantee).
func cleanRequestPath(p string) string {
	if p == "" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return path.Clean(p)
}

// withAuth resolves the request's user and stores it in the request
// context. Everything under /api/ except the public endpoints requires
// a valid identity and is refused with a 401 JSON error without one;
// public endpoints pass through regardless, with the user attached when
// present so handlers like /api/auth/status can report it.
func (s *Server) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, err := s.auth.Authenticate(r)
		if err == nil {
			r = r.WithContext(context.WithValue(r.Context(), userContextKey{}, u))
		} else if !errors.Is(err, auth.ErrUnauthenticated) {
			// A store fault, not a bad credential: log it, then refuse
			// like any unauthenticated request.
			s.logger.Printf("authenticate: %v", err)
		}
		cleaned := cleanRequestPath(r.URL.Path)
		if err != nil && strings.HasPrefix(cleaned, "/api/") && !publicPath(cleaned) {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// statusRecorder captures the response status for request logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

// WriteHeader records the status code before delegating.
func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// withLogging logs one line per request to the server's logger (stderr by
// default): method, path, status, and duration.
func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.logger.Printf("%s %s %d %s", r.Method, r.URL.Path, rec.status, time.Since(start).Round(time.Microsecond))
	})
}

// defaultLogger returns the stderr logger used when none is injected.
func defaultLogger() *log.Logger {
	return log.New(os.Stderr, "donezod ", log.LstdFlags)
}
