// Package api implements donezod's HTTP API on the standard library's
// net/http mux, using Go 1.22 method+pattern routing (no router
// dependency).
package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/bgrewell/donezo/internal/store"
)

// Server wires the stores to the HTTP surface.
type Server struct {
	core   *store.CoreStore
	spaces *store.SpaceStore
	auth   Authenticator
	logger *log.Logger
}

// ServerOption configures a Server (functional options pattern).
type ServerOption func(*Server)

// WithAuthenticator replaces the phase 1 static dev authenticator.
func WithAuthenticator(a Authenticator) ServerOption {
	return func(s *Server) { s.auth = a }
}

// WithLogger replaces the default stderr request logger.
func WithLogger(l *log.Logger) ServerOption {
	return func(s *Server) { s.logger = l }
}

// NewServer builds a Server around the given stores. By default requests
// are attributed to a fixed dev user (see StaticAuthenticator) and logged
// to stderr.
func NewServer(core *store.CoreStore, spaces *store.SpaceStore, opts ...ServerOption) *Server {
	s := &Server{
		core:   core,
		spaces: spaces,
		auth:   StaticAuthenticator{User: store.User{ID: 1, Username: "ben"}},
		logger: defaultLogger(),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Handler returns the fully assembled HTTP handler: routes wrapped in the
// auth and request-logging middleware.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/healthz", s.handleHealthz)
	mux.HandleFunc("GET /api/spaces", s.handleListSpaces)
	mux.HandleFunc("GET /api/spaces/{id}/state", s.handleSpaceState)
	// Method-agnostic fallbacks: with the JSON catch-all below registered,
	// ServeMux's built-in 405 logic never fires, so known paths get an
	// explicit JSON 405 for non-GET methods (method patterns win for GET).
	for _, path := range []string{"/api/healthz", "/api/spaces", "/api/spaces/{id}/state"} {
		mux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Allow", http.MethodGet)
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
	// Per-space ownership filter; with the phase 1 dev user this passes
	// everything, and with phase 2 auth it scopes the listing for free.
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
