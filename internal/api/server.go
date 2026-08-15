// Package api implements donezod's HTTP API on the standard library's
// net/http mux, using Go 1.22 method+pattern routing (no router
// dependency).
package api

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"net/netip"
	"time"

	"github.com/bgrewell/donezo/internal/auth"
	"github.com/bgrewell/donezo/internal/llm"
	"github.com/bgrewell/donezo/internal/mcp"
	"github.com/bgrewell/donezo/internal/notify"
	"github.com/bgrewell/donezo/internal/store"
)

// Model-call limits: generous for a person polishing captures, low enough
// that a client stuck in a loop stops before it costs anything real.
const (
	defaultLLMLimit  = 20
	defaultLLMWindow = 5 * time.Minute
)

// Notification-send limits: each add or resend texts/emails a destination,
// which costs the operator money and can reach someone who did not ask for
// it, so the send endpoints are capped per user. Generous for a person
// setting up a couple of destinations, low enough that a member cannot turn
// them into an outbound-message cannon.
const (
	defaultNotifyLimit  = 5
	defaultNotifyWindow = time.Hour
)

// Server wires the stores to the HTTP surface.
type Server struct {
	core        *store.CoreStore
	spaces      *store.SpaceStore
	auth        Authenticator
	passwords   auth.PasswordHasher
	limiter     *auth.RateLimiter
	mcpLimiter  *auth.RateLimiter
	llm         llm.Client
	prompts     *llm.PromptSet
	revisions   *revisions
	hideVersion bool
	llmLimiter  *auth.RateLimiter
	// notifyLimiter caps notification-send endpoints per user (contact add
	// and code resend), so a member cannot spend the operator's SMS/email
	// budget or spray messages at addresses they do not control.
	notifyLimiter *auth.RateLimiter
	clock         func() time.Time
	location      *time.Location
	// trashRetention is how long a deleted item stays restorable before the
	// sweep purges it. Zero or less disables the sweep.
	trashRetention time.Duration
	// notifiers are the configured delivery channels for reminders. Empty
	// is normal: it means nothing was configured and reminders stay in the
	// app, which is how donezo worked until #52.
	notifiers *notify.Registry
	// reminderMaxLateness bounds how overdue a reminder may be and still be
	// delivered. Zero or less delivers however late.
	reminderMaxLateness time.Duration
	// publicURL is where this instance is reachable, for the link in a
	// delivered reminder. Empty leaves the link out.
	publicURL string
	// operatorName and supportEmail identify who runs this instance, for the
	// published policy pages. Both empty leaves those pages unserved.
	operatorName string
	supportEmail string
	trustProxy   bool
	// trustedProxyNets are networks — beyond loopback — whose socket peers may
	// set the forwarded headers. Empty means loopback only. Only consulted
	// when trustProxy is on.
	trustedProxyNets []netip.Prefix
	logger           *log.Logger
	ui               fs.FS
	version          string
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

// WithMCPRateLimiter replaces the default per-token tools/call rate
// limiter (120 calls per minute) on the /mcp endpoint. The caller can
// share the limiter with a background sweeper so its idle entries are
// pruned, same as WithRateLimiter.
func WithMCPRateLimiter(l *auth.RateLimiter) ServerOption {
	return func(s *Server) { s.mcpLimiter = l }
}

// WithLLM installs the optional language-model client. Without it, model
// features report themselves as unavailable and every model endpoint
// answers 503 — a donezo with no model configured is a fully supported
// deployment, not a degraded one.
func WithLLM(c llm.Client) ServerOption {
	return func(s *Server) { s.llm = c }
}

// WithTrashRetention sets how long a deleted item stays restorable before the
// sweep purges it for good. Zero or less turns the sweep off, leaving the
// trash to be emptied by hand. Defaults to DefaultTrashRetention.
func WithTrashRetention(d time.Duration) ServerOption {
	return func(s *Server) { s.trashRetention = d }
}

// WithNotifiers installs the configured reminder delivery channels. Without
// it nothing is delivered outside the app, which is the default and a fully
// supported state — every other feature works the same either way.
func WithNotifiers(r *notify.Registry) ServerOption {
	return func(s *Server) { s.notifiers = r }
}

// WithReminderMaxLateness bounds how overdue a reminder may be and still be
// delivered. It is what stops an instance that was down for a week coming
// back and sending every missed reminder at once. Zero or less delivers
// however late. Defaults to config.DefaultReminderMaxLatenessHours.
func WithReminderMaxLateness(d time.Duration) ServerOption {
	return func(s *Server) { s.reminderMaxLateness = d }
}

// WithPublicURL sets the address a delivered reminder links back to. Empty
// leaves the link out, which is correct but means re-finding the thing by
// hand.
func WithPublicURL(u string) ServerOption {
	return func(s *Server) { s.publicURL = u }
}

// WithOperator names who runs this instance and how to reach them, which is
// what the published privacy policy and terms are given by. Without it,
// /privacy and /terms are not served: whoever hosts donezo makes those
// promises, not whoever wrote it.
func WithOperator(name, supportEmail string) ServerOption {
	return func(s *Server) {
		s.operatorName = name
		s.supportEmail = supportEmail
	}
}

// WithLocation sets the instance's fallback zone for calendar days, used for
// a user whose account carries no timezone of its own. It reaches the MCP
// handler, which is the surface that has to decide what "today" means without
// a browser to ask. Defaults to the host's zone.
func WithLocation(loc *time.Location) ServerOption {
	return func(s *Server) {
		if loc != nil {
			s.location = loc
		}
	}
}

// WithHideVersion suppresses the version in GET /api/instance, so the web UI
// has nothing to show. The operator's call: a version in the corner of the
// screen is useful while dogfooding and tells a stranger which build to look
// up exploits for once the instance is public.
func WithHideVersion(hide bool) ServerOption {
	return func(s *Server) { s.hideVersion = hide }
}

// WithPrompts installs the prompt set the model endpoints serve, which is
// how operator overrides read off disk reach the handlers. Without it the
// server uses the prompts donezo ships.
func WithPrompts(p *llm.PromptSet) ServerOption {
	return func(s *Server) { s.prompts = p }
}

// WithLLMRateLimiter replaces the default per-user model-call limiter
// (20 calls per 5 minutes). Model calls cost money and seconds upstream,
// so they are capped separately from the rest of the API.
func WithLLMRateLimiter(l *auth.RateLimiter) ServerOption {
	return func(s *Server) { s.llmLimiter = l }
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

// WithTrustedProxies sets the networks — beyond loopback — whose socket peers
// are trusted to have set the forwarded headers. It matters only with
// WithTrustProxy on, and only when the proxy is not on the same host as
// donezod. Without it, the forwarded headers are honoured for loopback peers
// alone.
func WithTrustedProxies(nets []netip.Prefix) ServerOption {
	return func(s *Server) { s.trustedProxyNets = nets }
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

// WithServerVersion sets the build version reported by the MCP endpoint's
// initialize handshake (serverInfo.version). Defaults to "dev".
func WithServerVersion(v string) ServerOption {
	return func(s *Server) {
		if v != "" {
			s.version = v
		}
	}
}

// WithWebUI serves the given filesystem — a production web bundle with
// index.html at its root — for every non-/api path: files by path,
// index.html for "/" and as the SPA fallback for unknown paths.
// Release builds pass webui.FS() (the go:embed'd web/dist); tests
// inject fixture filesystems. Without this option non-/api paths keep
// the JSON 404 behavior of API-only builds.
func WithWebUI(fsys fs.FS) ServerOption {
	return func(s *Server) { s.ui = fsys }
}

// NewServer builds a Server around the given stores. By default
// requests are authenticated by the donezo_session cookie against
// core.db, passwords are hashed with argon2id, login and setup are
// rate-limited per client IP, and requests are logged to stderr.
func NewServer(core *store.CoreStore, spaces *store.SpaceStore, opts ...ServerOption) *Server {
	s := &Server{
		core:           core,
		spaces:         spaces,
		clock:          time.Now,
		location:       time.Local,
		trashRetention: DefaultTrashRetention,
		logger:         defaultLogger(),
		// Nothing configured is the default: reminders stay in the app
		// unless an operator opts in.
		notifiers:           notify.NewRegistry(),
		reminderMaxLateness: DefaultReminderMaxLateness,
	}
	for _, opt := range opts {
		opt(s)
	}
	if s.notifiers == nil {
		s.notifiers = notify.NewRegistry()
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
	if s.llmLimiter == nil {
		s.llmLimiter = auth.NewRateLimiter(
			auth.WithLimit(defaultLLMLimit),
			auth.WithWindow(defaultLLMWindow),
			auth.WithLimiterClock(s.clock),
		)
	}
	if s.notifyLimiter == nil {
		s.notifyLimiter = auth.NewRateLimiter(
			auth.WithLimit(defaultNotifyLimit),
			auth.WithWindow(defaultNotifyWindow),
			auth.WithLimiterClock(s.clock),
		)
	}
	if s.llm == nil {
		s.llm = llm.Disabled{}
	}
	if s.prompts == nil {
		s.prompts = llm.BuiltInPromptSet()
	}
	if s.revisions == nil {
		s.revisions = newRevisions()
	}
	if s.version == "" {
		s.version = "dev"
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
	mux.HandleFunc("POST /api/auth/register", s.handleAuthRegister)
	mux.HandleFunc("GET /api/auth/me", s.handleAuthMe)
	mux.HandleFunc("GET /api/invites", s.handleListInvites)
	mux.HandleFunc("POST /api/invites", s.handleCreateInvite)
	mux.HandleFunc("DELETE /api/invites/{id}", s.handleRevokeInvite)
	mux.HandleFunc("GET /api/llm", s.handleLLMStatus)
	mux.HandleFunc("POST /api/llm/rewrite", s.handleLLMRewrite)
	mux.HandleFunc("GET /api/instance", s.handleInstance)
	mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	mux.HandleFunc("PATCH /api/settings", s.handlePatchSettings)
	// Public by construction: a policy nobody can read without an account is
	// not published, and the carrier reviewing it has no account.
	mux.HandleFunc("GET /privacy", s.handlePrivacy)
	mux.HandleFunc("GET /terms", s.handleTerms)
	mux.HandleFunc("GET /sms-opt-in", s.handleSMSOptIn)
	mux.HandleFunc("GET /api/admin/usage", s.handleUsageStats)
	mux.HandleFunc("GET /api/notify/status", s.handleNotifyStatus)
	mux.HandleFunc("GET /api/notify/contacts", s.handleListContacts)
	mux.HandleFunc("POST /api/notify/contacts", s.handleCreateContact)
	mux.HandleFunc("DELETE /api/notify/contacts/{id}", s.handleDeleteContact)
	mux.HandleFunc("POST /api/notify/contacts/{id}/code", s.handleSendContactCode)
	mux.HandleFunc("POST /api/notify/contacts/{id}/verify", s.handleVerifyContact)
	mux.HandleFunc("GET /api/tokens", s.handleListTokens)
	mux.HandleFunc("POST /api/tokens", s.handleCreateToken)
	mux.HandleFunc("DELETE /api/tokens/{id}", s.handleDeleteToken)
	mux.HandleFunc("GET /api/spaces", s.handleListSpaces)
	mux.HandleFunc("POST /api/spaces", s.handleCreateSpace)
	mux.HandleFunc("PATCH /api/spaces/{id}", s.handlePatchSpace)
	mux.HandleFunc("POST /api/spaces/{id}/archive", s.handleArchiveSpace)
	mux.HandleFunc("POST /api/spaces/{id}/unarchive", s.handleUnarchiveSpace)
	mux.HandleFunc("GET /api/spaces/{id}/state", s.handleSpaceState)
	mux.HandleFunc("GET /api/spaces/{id}/revision", s.handleSpaceRevision)
	mux.HandleFunc("POST /api/spaces/{id}/projects", s.handleCreateProject)
	mux.HandleFunc("PATCH /api/spaces/{id}/projects/{pid}", s.handlePatchProject)
	mux.HandleFunc("DELETE /api/spaces/{id}/projects/{pid}", s.handleDeleteProject)
	mux.HandleFunc("POST /api/spaces/{id}/activities", s.handleCreateActivity)
	mux.HandleFunc("PATCH /api/spaces/{id}/activities/{aid}", s.handlePatchActivity)
	mux.HandleFunc("DELETE /api/spaces/{id}/activities/{aid}", s.handleDeleteActivity)
	mux.HandleFunc("POST /api/spaces/{id}/tasks", s.handleCreateTask)
	mux.HandleFunc("PATCH /api/spaces/{id}/tasks/{tid}", s.handlePatchTask)
	mux.HandleFunc("POST /api/spaces/{id}/notes", s.handleCreateNote)
	mux.HandleFunc("PATCH /api/spaces/{id}/notes/{nid}", s.handlePatchNote)
	mux.HandleFunc("DELETE /api/spaces/{id}/notes/{nid}", s.handleDeleteNote)
	mux.HandleFunc("POST /api/spaces/{id}/notes/{nid}/convert", s.handleConvertNote)
	mux.HandleFunc("POST /api/spaces/{id}/reminders", s.handleCreateReminder)
	mux.HandleFunc("PATCH /api/spaces/{id}/reminders/{rid}", s.handlePatchReminder)
	mux.HandleFunc("POST /api/spaces/{id}/inbox", s.handleCreateInboxItem)
	mux.HandleFunc("PATCH /api/spaces/{id}/inbox/{iid}", s.handlePatchInboxItem)
	mux.HandleFunc("POST /api/spaces/{id}/inbox/{iid}/convert", s.handleConvertInboxItem)
	// The trash (#16). Deleting anywhere above moves a row here; these make
	// that visible and reversible.
	mux.HandleFunc("GET /api/spaces/{id}/trash", s.handleListTrash)
	mux.HandleFunc("POST /api/spaces/{id}/trash/empty", s.handleEmptyTrash)
	mux.HandleFunc("POST /api/spaces/{id}/trash/{entity}/{tid}/restore", s.handleRestoreTrash)
	mux.HandleFunc("DELETE /api/spaces/{id}/trash/{entity}/{tid}", s.handlePurgeTrash)
	// Method-agnostic fallbacks: with the JSON catch-all below registered,
	// ServeMux's built-in 405 logic never fires, so known paths get an
	// explicit JSON 405 for other methods (method patterns win when they
	// match). Values are the Allow header for each path.
	allowed := map[string]string{
		"/api/healthz":                                  http.MethodGet,
		"/api/auth/status":                              http.MethodGet,
		"/api/auth/setup":                               http.MethodPost,
		"/api/auth/login":                               http.MethodPost,
		"/api/auth/logout":                              http.MethodPost,
		"/api/auth/register":                            http.MethodPost,
		"/api/auth/me":                                  http.MethodGet,
		"/api/invites":                                  "GET, POST",
		"/api/invites/{id}":                             http.MethodDelete,
		"/api/llm":                                      http.MethodGet,
		"/api/llm/rewrite":                              http.MethodPost,
		"/api/instance":                                 http.MethodGet,
		"/api/settings":                                 "GET, PATCH",
		"/api/notify/status":                            http.MethodGet,
		"/api/notify/contacts":                          "GET, POST",
		"/api/notify/contacts/{id}":                     http.MethodDelete,
		"/api/notify/contacts/{id}/code":                http.MethodPost,
		"/api/notify/contacts/{id}/verify":              http.MethodPost,
		"/api/tokens":                                   "GET, POST",
		"/api/tokens/{id}":                              http.MethodDelete,
		"/api/spaces":                                   "GET, POST",
		"/api/spaces/{id}":                              http.MethodPatch,
		"/api/spaces/{id}/archive":                      http.MethodPost,
		"/api/spaces/{id}/unarchive":                    http.MethodPost,
		"/api/spaces/{id}/state":                        http.MethodGet,
		"/api/spaces/{id}/trash":                        http.MethodGet,
		"/api/spaces/{id}/trash/empty":                  http.MethodPost,
		"/api/spaces/{id}/trash/{entity}/{tid}":         http.MethodDelete,
		"/api/spaces/{id}/trash/{entity}/{tid}/restore": http.MethodPost,
		"/api/spaces/{id}/revision":                     http.MethodGet,
		"/api/spaces/{id}/projects":                     http.MethodPost,
		"/api/spaces/{id}/projects/{pid}":               "PATCH, DELETE",
		"/api/spaces/{id}/activities":                   http.MethodPost,
		"/api/spaces/{id}/activities/{aid}":             "PATCH, DELETE",
		"/api/spaces/{id}/tasks":                        http.MethodPost,
		"/api/spaces/{id}/tasks/{tid}":                  http.MethodPatch,
		"/api/spaces/{id}/notes":                        http.MethodPost,
		"/api/spaces/{id}/notes/{nid}":                  "PATCH, DELETE",
		"/api/spaces/{id}/notes/{nid}/convert":          http.MethodPost,
		"/api/spaces/{id}/reminders":                    http.MethodPost,
		"/api/spaces/{id}/reminders/{rid}":              http.MethodPatch,
		"/api/spaces/{id}/inbox":                        http.MethodPost,
		"/api/spaces/{id}/inbox/{iid}":                  http.MethodPatch,
		"/api/spaces/{id}/inbox/{iid}/convert":          http.MethodPost,
	}
	for path, methods := range allowed {
		methods := methods // capture (golangci-lint predates Go 1.22 loopvar)
		mux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Allow", methods)
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		})
	}
	// The MCP endpoint: a stateless Streamable HTTP server (built on the
	// official MCP Go SDK) with its own bearer-token auth. It is not under
	// /api/, so the session-cookie auth middleware passes it through
	// untouched (no cookies accepted, no CSRF surface); the mcp handler
	// authenticates every request itself.
	mcpOpts := []mcp.Option{
		mcp.WithClock(s.clock),
		mcp.WithLocation(s.location),
		mcp.WithLogger(s.logger),
		mcp.WithVersion(s.version),
		mcp.WithTrustProxy(s.trustProxy),
		// Writes over MCP change the same spaces the REST surface does, and
		// a browser watching one has no other way to learn about them.
		mcp.WithOnWrite(s.revisions.Bump),
	}
	if s.mcpLimiter != nil {
		mcpOpts = append(mcpOpts, mcp.WithRateLimiter(s.mcpLimiter))
	}
	mcpHandler := mcp.NewHandler(s.core, s.spaces, mcpOpts...)
	mux.Handle("/mcp", mcpHandler)
	// The "/" catch-all: the web bundle when one is wired in (release
	// builds, via WithWebUI), otherwise the API-only JSON 404. Either
	// way every /api/* pattern registered above is more specific and
	// wins, so the API surface is identical in both modes.
	if s.ui != nil {
		mux.Handle("/", s.webUIHandler())
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
			writeError(w, http.StatusNotFound, "not found")
		})
	}
	return s.withLogging(s.withAuth(s.withRevisionTracking(mux)))
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

// handleInstance describes the running instance to a signed-in client.
//
// Version only, for now. It is behind auth and omitted entirely when the
// operator sets --hide-version: an exact build number is of more use to
// somebody probing a public instance than to the people using it, so this is
// theirs to switch off rather than something the UI decides.
func (s *Server) handleInstance(w http.ResponseWriter, r *http.Request) {
	if _, ok := userFrom(r.Context()); !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	out := map[string]string{}
	if !s.hideVersion {
		out["version"] = s.version
	}
	writeJSON(w, http.StatusOK, out)
}

// handleSpaceRevision reports a space's change counter.
//
// This is the endpoint clients poll, so it stays deliberately tiny: no store
// access at all, just an owned-space check and an integer. Anything heavier
// would be paid every few seconds by every open tab.
//
// A client compares the number to the one it holds and refetches state only
// when it moves. The counter is meaningful only within one donezod process —
// a restart returns it to zero, which reads as "changed" and costs one
// refetch. Erring towards a spurious refetch is deliberate: a missed one
// leaves the screen quietly wrong, which is the failure this exists to remove.
func (s *Server) handleSpaceRevision(w http.ResponseWriter, r *http.Request) {
	sp, ok := s.ownedSpace(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]uint64{"revision": s.revisions.Current(sp.ID)})
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
