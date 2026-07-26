// Package api implements donezod's HTTP API on the standard library's
// net/http mux, using Go 1.22 method+pattern routing (no router
// dependency).
package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/bgrewell/donezo/internal/auth"
	"github.com/bgrewell/donezo/internal/store"
)

// Server wires the stores to the HTTP surface.
type Server struct {
	core       *store.CoreStore
	spaces     *store.SpaceStore
	auth       Authenticator
	passwords  auth.PasswordHasher
	limiter    *auth.RateLimiter
	clock      func() time.Time
	trustProxy bool
	logger     *log.Logger
}

// ServerOption configures a Server (functional options pattern).
type ServerOption func(*Server)

// WithAuthenticator replaces the default session-cookie authenticator.
// Production has no reason to; --dev-auto-login installs the
// StaticAuthenticator bypass through this.
func WithAuthenticator(a Authenticator) ServerOption {
	return func(s *Server) { s.auth = a }
}

// WithPasswordHasher replaces the default argon2id password hasher.
// Tests inject fast or call-counting implementations.
func WithPasswordHasher(h auth.PasswordHasher) ServerOption {
	return func(s *Server) { s.passwords = h }
}

// WithRateLimiter replaces the default login/setup rate limiter (10
// attempts per 5 minutes per client IP). The caller can share the
// limiter with a background sweeper.
func WithRateLimiter(l *auth.RateLimiter) ServerOption {
	return func(s *Server) { s.limiter = l }
}

// WithTrustProxy declares that a reverse proxy donezod trusts sits
// directly in front of it: rate limiting keys on the last
// X-Forwarded-For hop (the one that proxy appended) instead of the
// socket address, and X-Forwarded-Proto: https marks session cookies
// Secure. Enable only when such a proxy is actually there — without
// one, both headers arrive attacker-controlled, which is why they are
// ignored by default.
func WithTrustProxy(trust bool) ServerOption {
	return func(s *Server) { s.trustProxy = trust }
}

// WithClock overrides the server's time source (session issuance and
// cookie expiry). Defaults to time.Now; deterministic tests inject a
// fixed clock.
func WithClock(clock func() time.Time) ServerOption {
	return func(s *Server) {
		if clock != nil {
			s.clock = clock
		}
	}
}

// WithLogger replaces the default stderr request logger.
func WithLogger(l *log.Logger) ServerOption {
	return func(s *Server) { s.logger = l }
}

// NewServer builds a Server around the given stores. By default
// requests are authenticated by the donezo_session cookie against
// core.db, passwords are hashed with argon2id, login and setup are
// rate-limited per client IP, and requests are logged to stderr.
func NewServer(core *store.CoreStore, spaces *store.SpaceStore, opts ...ServerOption) *Server {
	s := &Server{
		core:   core,
		spaces: spaces,
		clock:  time.Now,
		logger: defaultLogger(),
	}
	for _, opt := range opts {
		opt(s)
	}
	if s.auth == nil {
		s.auth = auth.NewSessionAuthenticator(core, auth.WithSessionClock(s.clock))
	}
	if s.passwords == nil {
		s.passwords = auth.NewArgon2()
	}
	if s.limiter == nil {
		s.limiter = auth.NewRateLimiter(auth.WithLimiterClock(s.clock))
	}
	return s
}

// Handler returns the fully assembled HTTP handler: routes wrapped in the
// auth and request-logging middleware.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/healthz", s.handleHealthz)
	mux.HandleFunc("GET /api/auth/status", s.handleAuthStatus)
	mux.HandleFunc("POST /api/auth/setup", s.handleAuthSetup)
	mux.HandleFunc("POST /api/auth/login", s.handleAuthLogin)
	mux.HandleFunc("POST /api/auth/logout", s.handleAuthLogout)
	mux.HandleFunc("GET /api/auth/me", s.handleAuthMe)
	mux.HandleFunc("GET /api/spaces", s.handleListSpaces)
	mux.HandleFunc("GET /api/spaces/{id}/state", s.handleSpaceState)
	// Method-agnostic fallbacks: with the JSON catch-all below registered,
	// ServeMux's built-in 405 logic never fires, so known paths get an
	// explicit JSON 405 for other methods (method patterns win when they
	// match).
	allowed := map[string]string{
		"/api/healthz":           http.MethodGet,
		"/api/auth/status":       http.MethodGet,
		"/api/auth/setup":        http.MethodPost,
		"/api/auth/login":        http.MethodPost,
		"/api/auth/logout":       http.MethodPost,
		"/api/auth/me":           http.MethodGet,
		"/api/spaces":            http.MethodGet,
		"/api/spaces/{id}/state": http.MethodGet,
	}
	for path, method := range allowed {
		method := method // capture (golangci-lint predates Go 1.22 loopvar)
		mux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Allow", method)
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		})
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})
	return s.withLogging(s.withAuth(mux))
}

// handleHealthz reports liveness.
func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleListSpaces returns the authenticated user's spaces from the
// registry.
func (s *Server) handleListSpaces(w http.ResponseWriter, r *http.Request) {
	user, ok := userFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	all, err := s.core.ListSpaces(r.Context())
	if err != nil {
		s.logger.Printf("list spaces: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// Per-space ownership filter scopes the listing to the requester.
	mine := []store.Space{}
	for _, sp := range all {
		if sp.UserID == user.ID {
			mine = append(mine, sp)
		}
	}
	writeJSON(w, http.StatusOK, map[string][]store.Space{"spaces": mine})
}

// handleSpaceState returns the full content of one space.
func (s *Server) handleSpaceState(w http.ResponseWriter, r *http.Request) {
	user, ok := userFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id := r.PathValue("id")
	sp, err := s.core.GetSpace(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "space not found")
			return
		}
		s.logger.Printf("get space %s: %v", id, err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// Ownership check: spaces belonging to other users read as absent
	// rather than forbidden, so ids are not probeable.
	if sp.UserID != user.ID {
		writeError(w, http.StatusNotFound, "space not found")
		return
	}
	state, err := s.spaces.State(r.Context(), id)
	if err != nil {
		s.logger.Printf("space %s state: %v", id, err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, state)
}

// writeJSON serializes v with the given status code.
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// Headers are already sent; nothing useful can reach the client.
		log.Printf("api: encode response: %v", err)
	}
}

// writeError emits the canonical JSON error envelope {"error": msg}.
func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
