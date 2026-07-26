package api

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/bgrewell/donezo/internal/store"
)

// Authenticator resolves the user making a request. It is the seam where
// phase 2 session authentication slots in: a session-backed implementation
// will read a token from the request, look it up in core.db's sessions
// table, and return the owning user (or an error for 401).
//
// TODO(phase 2): replace StaticAuthenticator with a session-token
// implementation backed by core.db sessions.
type Authenticator interface {
	// Authenticate returns the requesting user, or an error if the
	// request carries no valid identity.
	Authenticate(r *http.Request) (store.User, error)
}

// StaticAuthenticator is the phase 1 development stub: every request is
// attributed to one fixed user.
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

// withAuth authenticates every request and stores the resulting user in
// the request context; failures become 401 JSON errors.
func (s *Server) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, err := s.auth.Authenticate(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userContextKey{}, u)))
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
