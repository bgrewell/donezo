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
	mux.HandleFunc("POST /api/spaces", s.handleCreateSpace)
	mux.HandleFunc("PATCH /api/spaces/{id}", s.handlePatchSpace)
	mux.HandleFunc("POST /api/spaces/{id}/archive", s.handleArchiveSpace)
	mux.HandleFunc("POST /api/spaces/{id}/unarchive", s.handleUnarchiveSpace)
	mux.HandleFunc("GET /api/spaces/{id}/state", s.handleSpaceState)
	mux.HandleFunc("POST /api/spaces/{id}/projects", s.handleCreateProject)
	mux.HandleFunc("PATCH /api/spaces/{id}/projects/{pid}", s.handlePatchProject)
	mux.HandleFunc("POST /api/spaces/{id}/activities", s.handleCreateActivity)
	mux.HandleFunc("PATCH /api/spaces/{id}/activities/{aid}", s.handlePatchActivity)
	mux.HandleFunc("DELETE /api/spaces/{id}/activities/{aid}", s.handleDeleteActivity)
	mux.HandleFunc("POST /api/spaces/{id}/tasks", s.handleCreateTask)
	mux.HandleFunc("PATCH /api/spaces/{id}/tasks/{tid}", s.handlePatchTask)
	mux.HandleFunc("POST /api/spaces/{id}/notes", s.handleCreateNote)
	mux.HandleFunc("POST /api/spaces/{id}/reminders", s.handleCreateReminder)
	mux.HandleFunc("PATCH /api/spaces/{id}/reminders/{rid}", s.handlePatchReminder)
	mux.HandleFunc("POST /api/spaces/{id}/inbox", s.handleCreateInboxItem)
	mux.HandleFunc("PATCH /api/spaces/{id}/inbox/{iid}", s.handlePatchInboxItem)
	mux.HandleFunc("POST /api/spaces/{id}/inbox/{iid}/convert", s.handleConvertInboxItem)
	// Method-agnostic fallbacks: with the JSON catch-all below registered,
	// ServeMux's built-in 405 logic never fires, so known paths get an
	// explicit JSON 405 for other methods (method patterns win when they
	// match). Values are the Allow header for each path.
	allowed := map[string]string{
		"/api/healthz":                         http.MethodGet,
		"/api/auth/status":                     http.MethodGet,
		"/api/auth/setup":                      http.MethodPost,
		"/api/auth/login":                      http.MethodPost,
		"/api/auth/logout":                     http.MethodPost,
		"/api/auth/me":                         http.MethodGet,
		"/api/spaces":                          "GET, POST",
		"/api/spaces/{id}":                     http.MethodPatch,
		"/api/spaces/{id}/archive":             http.MethodPost,
		"/api/spaces/{id}/unarchive":           http.MethodPost,
		"/api/spaces/{id}/state":               http.MethodGet,
		"/api/spaces/{id}/projects":            http.MethodPost,
		"/api/spaces/{id}/projects/{pid}":      http.MethodPatch,
		"/api/spaces/{id}/activities":          http.MethodPost,
		"/api/spaces/{id}/activities/{aid}":    "PATCH, DELETE",
		"/api/spaces/{id}/tasks":               http.MethodPost,
		"/api/spaces/{id}/tasks/{tid}":         http.MethodPatch,
		"/api/spaces/{id}/notes":               http.MethodPost,
		"/api/spaces/{id}/reminders":           http.MethodPost,
		"/api/spaces/{id}/reminders/{rid}":     http.MethodPatch,
		"/api/spaces/{id}/inbox":               http.MethodPost,
		"/api/spaces/{id}/inbox/{iid}":         http.MethodPatch,
		"/api/spaces/{id}/inbox/{iid}/convert": http.MethodPost,
	}
	for path, methods := range allowed {
		methods := methods // capture (golangci-lint predates Go 1.22 loopvar)
		mux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Allow", methods)
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
	sp, ok := s.ownedSpace(w, r)
	if !ok {
		return
	}
	state, err := s.spaces.State(r.Context(), sp.ID)
	if err != nil {
		s.logger.Printf("space %s state: %v", sp.ID, err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, state)
}

// ownedSpace resolves the {id} path segment to a space owned by the
// requesting user. On failure it writes the response and reports false.
// Unknown spaces and spaces belonging to other users both read as 404,
// so ids are not probeable.
func (s *Server) ownedSpace(w http.ResponseWriter, r *http.Request) (store.Space, bool) {
	user, ok := userFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return store.Space{}, false
	}
	id := r.PathValue("id")
	sp, err := s.core.GetSpace(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "space not found")
			return store.Space{}, false
		}
		s.logger.Printf("get space %s: %v", id, err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return store.Space{}, false
	}
	if sp.UserID != user.ID {
		writeError(w, http.StatusNotFound, "space not found")
		return store.Space{}, false
	}
	return sp, true
}

// ownedLiveSpace resolves {id} like ownedSpace and additionally requires
// the space to be unarchived. Content mutations use it so the archive
// state is a real write barrier server-side — not just chips the UI
// hides — while reads and the space lifecycle endpoints (rename,
// unarchive) keep working on archived spaces. On failure it writes a 409
// and reports false.
func (s *Server) ownedLiveSpace(w http.ResponseWriter, r *http.Request) (store.Space, bool) {
	sp, ok := s.ownedSpace(w, r)
	if !ok {
		return store.Space{}, false
	}
	if sp.ArchivedAt != nil {
		writeError(w, http.StatusConflict, "space is archived — unarchive it to make changes")
		return store.Space{}, false
	}
	return sp, true
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
