// Package mcp implements donezod's Model Context Protocol endpoint: a
// stateless Streamable HTTP server that lets a user's LLM manage their
// donezo data through a curated set of tools.
//
// The wire protocol is provided by the official Go SDK
// (github.com/modelcontextprotocol/go-sdk), served over plain net/http as a
// stateless Streamable HTTP handler mounted at POST /mcp. This package is a
// thin wrapper around the SDK that supplies donezo's own concerns:
//
//   - Authentication: a per-user API token in the Authorization: Bearer
//     header (see internal/auth and the api_tokens store). Session cookies
//     are deliberately not accepted, so /mcp carries no CSRF surface. A
//     donezo-specific HTTP middleware (withAuth) resolves the token to a
//     caller BEFORE the SDK handler runs, answering 401 with a
//     WWW-Authenticate challenge on any missing, malformed, unknown, or
//     revoked credential, and storing the resolved caller in the request
//     context under an unexported key.
//   - Scope, rate limiting, and instructions: a single SDK receiving
//     middleware (the gate) reads the caller back out of the request context
//     and centralizes the read_only-vs-read_write scope check, the per-token
//     tools/call rate limit (applied before tool dispatch), the scope-aware
//     filtering of tools/list, and the scope-aware initialize instructions.
//
// Tool handlers call the store layer directly with the same ownership checks
// the REST handlers use; entity ids are always server-generated.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"strings"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/bgrewell/donezo/internal/auth"
	"github.com/bgrewell/donezo/internal/store"
)

// serverName is the MCP serverInfo.name reported at initialize.
const serverName = "donezo"

// mcpToolCallLimit is the per-token tools/call budget per minute. It is
// generous — an interactive LLM makes far fewer calls — but bounds a
// runaway agent's write rate against the store. It is also the figure named
// in the rate-limit tool result, independent of any tighter limiter a test
// injects.
const mcpToolCallLimit = 120

// maxMCPBodyBytes bounds a request body; JSON-RPC tool calls are small.
// Requests past it are rejected by the SDK transport with 413.
const maxMCPBodyBytes = 1 << 20

// Handler serves the MCP endpoint over the donezo stores. Construct it with
// NewHandler and mount its ServeHTTP at /mcp. It wraps a single SDK
// *mcp.Server (the whole tool surface registered) with donezo's bearer-auth HTTP
// middleware and a scope/rate-limit receiving middleware.
type Handler struct {
	core       *store.CoreStore
	spaces     *store.SpaceStore
	limiter    *auth.RateLimiter
	clock      func() time.Time
	logger     *log.Logger
	version    string
	trustProxy bool
	onWrite    func(spaceID string)

	server      *mcpsdk.Server
	httpHandler http.Handler
}

// Option configures a Handler (functional options pattern).
type Option func(*Handler)

// WithClock overrides the time source (server-generated timestamps and the
// rate-limiter window). Defaults to time.Now; deterministic tests inject a
// fixed clock.
func WithClock(clock func() time.Time) Option {
	return func(h *Handler) {
		if clock != nil {
			h.clock = clock
		}
	}
}

// WithLogger replaces the default stderr logger.
func WithLogger(l *log.Logger) Option {
	return func(h *Handler) {
		if l != nil {
			h.logger = l
		}
	}
}

// WithVersion sets the version reported in initialize's serverInfo.
func WithVersion(v string) Option {
	return func(h *Handler) {
		if v != "" {
			h.version = v
		}
	}
}

// WithRateLimiter replaces the default per-token tools/call limiter (120
// calls per minute). Tests inject a tighter one.
func WithRateLimiter(l *auth.RateLimiter) Option {
	return func(h *Handler) { h.limiter = l }
}

// WithTrustProxy mirrors api.WithTrustProxy: pass true only when a trusted
// reverse proxy sits directly in front of donezod. It controls the SDK
// transport's DNS-rebinding guard (see the comment on
// DisableLocalhostProtection in build()) — disabled behind a trusted proxy,
// where the guard would reject every legitimate request, and left enabled
// for a direct-exposure instance with no proxy, where it adds a second,
// independent layer under bearer-token auth against loopback rebinding.
func WithTrustProxy(trust bool) Option {
	return func(h *Handler) { h.trustProxy = trust }
}

// WithOnWrite installs a callback invoked with the space id after a write
// tool succeeds. donezod uses it to bump that space's revision, so a browser
// watching the space learns about changes an LLM made without it.
//
// It fires only for tools marked as writes, and only when the call did not
// report an error: a refused or failed tool changed nothing, and treating it
// as a change would make every connected client refetch identical state.
func WithOnWrite(fn func(spaceID string)) Option {
	return func(h *Handler) { h.onWrite = fn }
}

// NewHandler builds an MCP Handler over the given stores. By default
// tools/call is rate-limited to 120 calls per minute per token, timestamps
// use time.Now, and logs go to stderr.
func NewHandler(core *store.CoreStore, spaces *store.SpaceStore, opts ...Option) *Handler {
	h := &Handler{
		core:    core,
		spaces:  spaces,
		clock:   time.Now,
		logger:  log.New(os.Stderr, "donezod-mcp ", log.LstdFlags),
		version: "dev",
	}
	for _, opt := range opts {
		opt(h)
	}
	if h.limiter == nil {
		h.limiter = auth.NewRateLimiter(
			auth.WithLimit(mcpToolCallLimit),
			auth.WithWindow(time.Minute),
			auth.WithLimiterClock(h.clock),
		)
	}
	h.build()
	return h
}

// build constructs the SDK server, registers the tool surface, installs the
// scope/rate-limit receiving middleware, and wraps the stateless Streamable
// HTTP handler in donezo's bearer-auth middleware. It runs once, at
// construction.
func (h *Handler) build() {
	h.server = mcpsdk.NewServer(
		&mcpsdk.Implementation{Name: serverName, Version: h.version},
		&mcpsdk.ServerOptions{
			// Instructions are set per request by the gate middleware, tailored
			// to the caller's scope; the static field is a scope-agnostic
			// fallback for any path that skips the middleware.
			Instructions: instructions(""),
		},
	)
	registerTools(h.server, h)
	h.server.AddReceivingMiddleware(h.gate)

	streamable := mcpsdk.NewStreamableHTTPHandler(
		func(*http.Request) *mcpsdk.Server { return h.server },
		&mcpsdk.StreamableHTTPOptions{
			// Stateless: one ephemeral session per POST, GET/DELETE -> 405.
			Stateless: true,
			// Respond with a single application/json body rather than an SSE
			// stream, matching the endpoint's request/response tool surface.
			JSONResponse: true,
			// Preserve the 1 MiB body cap (SDK rejects overflow with 413).
			MaxRequestBodyBytes: maxMCPBodyBytes,
			// Bearer-token auth (never cookies, never ambient credentials a
			// browser could be tricked into sending) is a complete substitute
			// for this guard, independent of network position — a request
			// that reaches it without a valid token still gets 401 before any
			// data is read or written. Behind a trusted reverse proxy the
			// guard would actively break every legitimate request (loopback
			// socket, public Host header), so it must be off there; with no
			// proxy in front, leave it on as a second, independent layer
			// against loopback DNS rebinding.
			DisableLocalhostProtection: h.trustProxy,
		},
	)
	h.httpHandler = h.withAuth(streamable)
}

// ServeHTTP implements http.Handler by delegating to the auth-wrapped
// stateless Streamable HTTP handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.httpHandler.ServeHTTP(w, r)
}

// caller carries the identity resolved from the bearer token for the life
// of one request.
type caller struct {
	user    store.User
	tokenID string
	scope   string
}

// callerCtxKey is the unexported key under which a resolved caller is stored
// in the request context by withAuth and read back by the gate middleware
// and tool handlers.
type callerCtxKey struct{}

// withCaller returns ctx carrying c.
func withCaller(ctx context.Context, c caller) context.Context {
	return context.WithValue(ctx, callerCtxKey{}, c)
}

// callerFrom returns the caller stored in ctx, if any. The SDK plumbs the
// HTTP request context through to receiving middleware and tool handlers
// (stateless sessions connect with the request context), so a caller set by
// withAuth is visible to both.
func callerFrom(ctx context.Context) (caller, bool) {
	c, ok := ctx.Value(callerCtxKey{}).(caller)
	return c, ok
}

// withAuth authenticates POST requests before the SDK handler runs. Non-POST
// requests are passed straight through so the stateless transport answers
// them with 405 (Allow: POST), matching the endpoint's POST-only contract.
func (h *Handler) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			next.ServeHTTP(w, r)
			return
		}
		c, ok := h.authenticate(w, r)
		if !ok {
			return
		}
		// Best-effort last-used bookkeeping; the store throttles to once/minute
		// and a failed write must never reject an otherwise valid request.
		_ = h.core.TouchAPITokenLastUsed(r.Context(), c.tokenID)
		next.ServeHTTP(w, r.WithContext(withCaller(r.Context(), c)))
	})
}

// authenticate resolves the Authorization: Bearer token to a caller. It
// accepts only bearer tokens — never session cookies — and answers 401 with
// a WWW-Authenticate challenge on any missing, malformed, unknown, or
// revoked credential.
func (h *Handler) authenticate(w http.ResponseWriter, r *http.Request) (caller, bool) {
	// RFC 7235 defines the auth-scheme token as case-insensitive; match it
	// that way so any spec-compliant client (not just ones that happen to
	// send the canonical "Bearer" casing) can authenticate.
	const scheme = "bearer "
	authz := r.Header.Get("Authorization")
	if len(authz) < len(scheme) || !strings.EqualFold(authz[:len(scheme)], scheme) {
		h.unauthorized(w, "a bearer token is required")
		return caller{}, false
	}
	token := strings.TrimSpace(authz[len(scheme):])
	if token == "" || !strings.HasPrefix(token, auth.APITokenScheme) {
		h.unauthorized(w, "invalid bearer token")
		return caller{}, false
	}
	user, tokenID, scope, err := h.core.GetUserByAPIToken(r.Context(), auth.HashToken(token))
	if errors.Is(err, store.ErrNotFound) {
		h.unauthorized(w, "invalid or revoked token")
		return caller{}, false
	}
	if err != nil {
		h.logger.Printf("mcp authenticate: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return caller{}, false
	}
	return caller{user: user, tokenID: tokenID, scope: scope}, true
}

// unauthorized answers 401 with the bearer challenge every MCP client
// expects.
func (h *Handler) unauthorized(w http.ResponseWriter, msg string) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="donezo"`)
	writeJSON(w, http.StatusUnauthorized, map[string]string{"error": msg})
}

// gate is the SDK receiving middleware that centralizes donezo's per-request
// policy. It runs after withAuth has stored the caller in the request
// context, so callerFrom always resolves here.
//
//   - tools/call: the per-token rate limit is applied FIRST — before the tool
//     name is even read — so an unknown-tool or bad-arguments call still costs
//     budget and cannot be used to probe around the ceiling for free. Then the
//     single scope gate: a write tool invoked by a read_only token is refused
//     with an isError result (and the handler, hence any store write, is never
//     reached).
//   - tools/list: the result is filtered to the tools the caller's scope
//     permits (read_only sees the read tools only).
//   - initialize: the result's instructions are tailored to the caller's
//     scope.
func (h *Handler) gate(next mcpsdk.MethodHandler) mcpsdk.MethodHandler {
	return func(ctx context.Context, method string, req mcpsdk.Request) (mcpsdk.Result, error) {
		c, _ := callerFrom(ctx)
		switch method {
		case "tools/call":
			if over, secs := h.rateLimited(c.tokenID); over {
				return toolTextResult(fmt.Sprintf(
					"Rate limit exceeded (%d tool calls per minute per token). Wait about %d seconds and retry.",
					mcpToolCallLimit, secs), true), nil
			}
			if p, ok := req.GetParams().(*mcpsdk.CallToolParamsRaw); ok {
				if t, found := toolByName[p.Name]; found && t.write && c.scope != store.ScopeReadWrite {
					return toolTextResult(
						"This tool requires a read_write token. Your token is read_only, so it cannot make changes.", true), nil
				}
			}
			return next(ctx, method, req)
		case "tools/list":
			res, err := next(ctx, method, req)
			if err != nil {
				return res, err
			}
			if c.scope != store.ScopeReadWrite {
				if lr, ok := res.(*mcpsdk.ListToolsResult); ok {
					lr.Tools = filterReadTools(lr.Tools)
				}
			}
			return res, nil
		case "initialize", "server/discover":
			// Tailor the orientation to the caller's scope on both the legacy
			// initialize handshake and the sessionless server/discover used by
			// the 2026-07-28+ protocol.
			res, err := next(ctx, method, req)
			if err == nil {
				switch r := res.(type) {
				case *mcpsdk.InitializeResult:
					r.Instructions = instructions(c.scope)
				case *mcpsdk.DiscoverResult:
					r.Instructions = instructions(c.scope)
				}
			}
			return res, err
		default:
			return next(ctx, method, req)
		}
	}
}

// filterReadTools returns the subset of tools a read_only caller may see:
// the read (non-write) tools. It keeps the shared *Tool pointers untouched,
// only dropping write entries from the slice.
func filterReadTools(all []*mcpsdk.Tool) []*mcpsdk.Tool {
	out := make([]*mcpsdk.Tool, 0, len(all))
	for _, t := range all {
		if meta, ok := toolByName[t.Name]; ok && meta.write {
			continue
		}
		out = append(out, t)
	}
	return out
}

// instructions is the prescriptive orientation returned at initialize,
// tailored to the token's scope so the calling LLM knows which verbs it may
// use. An empty scope yields the scope-agnostic base (used as the SDK's
// static fallback).
func instructions(scope string) string {
	base := "donezo is a personal work-memory system. Data is organized into spaces " +
		"(isolated workspaces); each space holds projects, activities (PAST facts on a " +
		"timeline), tasks (FUTURE work with a lifecycle), notes, reminders, and an inbox of " +
		"raw captures. Always call list_spaces first to discover space ids, then " +
		"get_space_overview(space_id) to orient before acting in a space. Use log_activity " +
		"for things that already happened and create_task for things that might happen; when " +
		"unsure how to classify something, capture_to_inbox is the zero-decision default. " +
		"Your token reaches every space its owner owns, not just one — there is no 'active' " +
		"space over MCP, so always pass the space_id the user actually means (ask if it isn't " +
		"clear from context) rather than assuming the first or most recent one. " +
		"capture_to_inbox in particular is designed to write into whichever space the captured " +
		"thought belongs to, even if you are mid-conversation about a different one. This " +
		"surface does not create, rename, archive, or delete spaces, delete projects, or manage " +
		"tokens/invites/other users — those stay in the web app; say so plainly rather than " +
		"improvising a workaround if asked. delete_item permanently removes an item and cannot " +
		"be undone: confirm with the user before calling it, and prefer complete_task for " +
		"finished work or dismiss_inbox_item for a reviewed capture. tools/call is rate-limited per token; a limited " +
		"call returns an isError result naming how long to wait — relay that to the user rather " +
		"than silently retrying in a loop."
	switch scope {
	case store.ScopeReadWrite:
		return base + " This token has read_write scope: both read and write tools are available."
	case store.ScopeReadOnly:
		return base + " This token has read_only scope: only the read tools are listed and callable; " +
			"minting a read_write token unlocks the write tools."
	default:
		return base
	}
}

// rateLimited applies the per-token tools/call rate limit, reporting whether
// the caller is over budget and, if so, a retry hint in seconds.
func (h *Handler) rateLimited(tokenID string) (bool, int) {
	ok, retry := h.limiter.Allow(tokenID)
	if ok {
		return false, 0
	}
	secs := int(math.Ceil(retry.Seconds()))
	if secs < 1 {
		secs = 1
	}
	return true, secs
}

// nowRFC3339 renders the handler clock as an RFC 3339 UTC timestamp.
func (h *Handler) nowRFC3339() string {
	return h.clock().UTC().Format(time.RFC3339)
}

// today renders the handler clock as a yyyy-MM-dd date.
func (h *Handler) today() string {
	return h.clock().UTC().Format("2006-01-02")
}

// writeJSON serializes v with the given status code. It backs the bearer-auth
// middleware's error responses (401/500), which are emitted before the SDK
// handler runs.
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("mcp: encode response: %v", err)
	}
}
