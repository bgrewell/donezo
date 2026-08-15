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
		// %q on the path: it is percent-decoded by net/http, so a raw %0a in
		// the request line arrives as a real newline. Logged with %s that
		// would let an unauthenticated client forge whole log lines (this
		// wrapper is outermost, so it runs before auth can refuse anything).
		// strconv.Quote escapes CR/LF/NUL, keeping one request to one line.
		s.logger.Printf("%s %q %d %s", r.Method, r.URL.Path, rec.status, time.Since(start).Round(time.Microsecond))
	})
}

// defaultLogger returns the stderr logger used when none is injected.
func defaultLogger() *log.Logger {
	return log.New(os.Stderr, "donezod ", log.LstdFlags)
}

// spaceIDFromPath returns the space id in an "/api/spaces/{id}/..." path, or
// "" for any other path.
//
// The revision middleware sits outside the mux, so r.PathValue is not
// populated yet — the pattern has not been matched at that point. Parsing the
// one path shape that matters is cheaper and clearer than routing twice.
func spaceIDFromPath(path string) string {
	const prefix = "/api/spaces/"
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	rest := strings.TrimPrefix(path, prefix)
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		return rest[:i]
	}
	// "/api/spaces/{id}" itself — a space rename or archive, which changes
	// the space rather than its contents. Still worth reporting.
	return rest
}

// isMutatingMethod reports whether a method can change stored state.
func isMutatingMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPatch, http.MethodPut, http.MethodDelete:
		return true
	default:
		return false
	}
}

// withRevisionTracking bumps a space's revision after a mutating request
// against it succeeds, so pollers learn that something changed.
//
// It deliberately keys on the response status rather than the request: a
// rejected write (validation, an archived space, a missing row) changes
// nothing, and bumping for it would make every client refetch identical state.
//
// This covers the whole REST write surface in one place. Hanging the bump off
// each of the store's ~30 mutating methods instead would work right up until
// somebody adds the thirty-first and forgets. Writes arriving over MCP do not
// pass through here at all — internal/mcp bumps the same counter from its own
// single choke point.
func (s *Server) withRevisionTracking(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		spaceID := ""
		if isMutatingMethod(r.Method) {
			spaceID = spaceIDFromPath(r.URL.Path)
		}
		if spaceID == "" {
			next.ServeHTTP(w, r)
			return
		}
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if rec.status >= 200 && rec.status < 300 {
			s.revisions.Bump(spaceID)
		}
	})
}
