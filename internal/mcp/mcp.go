// Package mcp implements donezod's Model Context Protocol endpoint: a
// hand-rolled, stateless Streamable HTTP server that lets a user's LLM
// manage their donezo data through a curated set of tools.
//
// It speaks JSON-RPC 2.0 over POST /mcp and responds with a single
// application/json body (no SSE, no server-initiated streams, no session
// ids). The official and community Go MCP SDKs both require Go >= 1.25,
// which would force a toolchain bump off donezod's pinned 1.22.4, so this
// minimal server implements exactly the methods a tool-only server needs:
// initialize, notifications/initialized, tools/list, tools/call, and ping.
//
// Authentication is a per-user API token in the Authorization: Bearer
// header (see internal/auth and the api_tokens store) — session cookies
// are deliberately not accepted, so /mcp carries no CSRF surface. Tool
// handlers call the store layer directly with the same ownership checks
// the REST handlers use.
package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/bgrewell/donezo/internal/auth"
	"github.com/bgrewell/donezo/internal/store"
)

// ProtocolVersion is the latest MCP revision this server implements. When
// a client requests a different but recognized revision, initialize echoes
// the client's; otherwise it negotiates to this one.
const ProtocolVersion = "2025-06-18"

// serverName is the MCP serverInfo.name reported at initialize.
const serverName = "donezo"

// mcpToolCallLimit is the per-token tools/call budget per minute. It is
// generous — an interactive LLM makes far fewer calls — but bounds a
// runaway agent's write rate against the store.
const mcpToolCallLimit = 120

// maxMCPBodyBytes bounds a request body; JSON-RPC tool calls are small.
const maxMCPBodyBytes = 1 << 20

// supportedVersions are the protocol revisions initialize will echo back
// when the client asks for them. Any unrecognized request negotiates to
// ProtocolVersion.
var supportedVersions = map[string]bool{
	"2025-06-18": true,
	"2025-03-26": true,
	"2024-11-05": true,
}

// JSON-RPC 2.0 error codes.
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternalError  = -32603
)

// rpcRequest is one decoded JSON-RPC request. ID is raw so a string,
// number, or absent (notification) id all round-trip unchanged.
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// rpcError is the error object of a JSON-RPC error response.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// rpcResultEnvelope and rpcErrorEnvelope are kept distinct so a response
// carries exactly one of result/error (never both, never neither) — a
// shared struct with omitempty would drop an empty-object result like
// ping's, which JSON-RPC requires to be present.
type rpcResultEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result"`
}

type rpcErrorEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Error   rpcError        `json:"error"`
}

// Handler serves the MCP endpoint over the donezo stores. Construct it
// with NewHandler and mount its ServeHTTP at /mcp.
type Handler struct {
	core    *store.CoreStore
	spaces  *store.SpaceStore
	limiter *auth.RateLimiter
	clock   func() time.Time
	logger  *log.Logger
	version string
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
	return h
}

// caller carries the identity resolved from the bearer token for the life
// of one request.
type caller struct {
	user    store.User
	tokenID string
	scope   string
}

// ServeHTTP implements the stateless Streamable HTTP transport. Only POST
// is accepted; GET (server-initiated streams) answers 405 as the spec
// permits for servers that do not offer them.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
			"error": "the MCP endpoint accepts POST only (no server-initiated streams)",
		})
		return
	}

	c, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	// Best-effort last-used bookkeeping; the store throttles to once/minute
	// and a failed write must never reject an otherwise valid request.
	_ = h.core.TouchAPITokenLastUsed(r.Context(), c.tokenID)

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxMCPBodyBytes))
	if err != nil {
		h.writeRPCError(w, http.StatusBadRequest, nil, codeInvalidRequest, "request body is too large")
		return
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		h.writeRPCError(w, http.StatusBadRequest, nil, codeInvalidRequest, "empty request body")
		return
	}
	// JSON-RPC batching was removed in the 2025-06-18 revision; reject the
	// array form politely rather than silently mishandling it.
	if trimmed[0] == '[' {
		h.writeRPCError(w, http.StatusBadRequest, nil, codeInvalidRequest,
			"JSON-RPC batch requests are not supported; send one request per POST")
		return
	}
	var req rpcRequest
	if err := json.Unmarshal(trimmed, &req); err != nil {
		h.writeRPCError(w, http.StatusBadRequest, nil, codeParseError, "invalid JSON")
		return
	}
	if req.JSONRPC != "2.0" || req.Method == "" {
		h.writeRPCError(w, http.StatusBadRequest, req.ID, codeInvalidRequest,
			"not a valid JSON-RPC 2.0 request")
		return
	}
	h.dispatch(w, r, req, c)
}

// dispatch routes a well-formed request to its method handler. Any method
// under notifications/ is a fire-and-forget notification: accept it with
// 202 and no body.
func (h *Handler) dispatch(w http.ResponseWriter, r *http.Request, req rpcRequest, c caller) {
	if strings.HasPrefix(req.Method, "notifications/") {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	switch req.Method {
	case "initialize":
		h.handleInitialize(w, req, c)
	case "ping":
		h.writeRPCResult(w, req.ID, struct{}{})
	case "tools/list":
		h.handleToolsList(w, req, c)
	case "tools/call":
		h.handleToolsCall(w, r, req, c)
	default:
		h.writeRPCError(w, http.StatusOK, req.ID, codeMethodNotFound, "method not found: "+req.Method)
	}
}

// authenticate resolves the Authorization: Bearer token to a caller. It
// accepts only bearer tokens — never session cookies — and answers 401
// with a WWW-Authenticate challenge on any missing, malformed, unknown, or
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

// handleInitialize answers the capability handshake: it echoes the
// client's protocol version when recognized (else negotiates to
// ProtocolVersion), advertises the tools capability, and returns
// serverInfo plus scope-aware usage instructions.
func (h *Handler) handleInitialize(w http.ResponseWriter, req rpcRequest, c caller) {
	var params struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if len(req.Params) > 0 {
		// Tolerant: unusable params still initialize at the default version.
		_ = json.Unmarshal(req.Params, &params)
	}
	version := ProtocolVersion
	if params.ProtocolVersion != "" && supportedVersions[params.ProtocolVersion] {
		version = params.ProtocolVersion
	}
	h.writeRPCResult(w, req.ID, map[string]any{
		"protocolVersion": version,
		"capabilities":    map[string]any{"tools": map[string]any{}},
		"serverInfo":      map[string]any{"name": serverName, "version": h.version},
		"instructions":    instructions(c.scope),
	})
}

// instructions is the prescriptive orientation returned at initialize,
// tailored to the token's scope so the calling LLM knows which verbs it
// may use.
func instructions(scope string) string {
	base := "donezo is a personal work-memory system. Data is organized into spaces " +
		"(isolated workspaces); each space holds projects, activities (PAST facts on a " +
		"timeline), tasks (FUTURE work with a lifecycle), notes, reminders, and an inbox of " +
		"raw captures. Always call list_spaces first to discover space ids, then " +
		"get_space_overview(space_id) to orient before acting in a space. Use log_activity " +
		"for things that already happened and create_task for things that might happen; when " +
		"unsure how to classify something, capture_to_inbox is the zero-decision default."
	if scope == store.ScopeReadWrite {
		return base + " This token has read_write scope: both read and write tools are available."
	}
	return base + " This token has read_only scope: only the read tools are listed and callable; " +
		"minting a read_write token unlocks the write tools."
}

// ─── Response writers ─────────────────────────────────────────────────────

// writeRPCResult writes a JSON-RPC success response (always HTTP 200).
func (h *Handler) writeRPCResult(w http.ResponseWriter, id json.RawMessage, result any) {
	writeJSON(w, http.StatusOK, rpcResultEnvelope{JSONRPC: "2.0", ID: id, Result: result})
}

// writeRPCError writes a JSON-RPC error response at the given HTTP status:
// 400 for transport-level faults (unparseable / not a valid request) where
// no proper response can be formed, 200 for method-level errors carrying
// the request id.
func (h *Handler) writeRPCError(w http.ResponseWriter, httpStatus int, id json.RawMessage, code int, msg string) {
	writeJSON(w, httpStatus, rpcErrorEnvelope{
		JSONRPC: "2.0",
		ID:      id,
		Error:   rpcError{Code: code, Message: msg},
	})
}

// writeJSON serializes v with the given status code.
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("mcp: encode response: %v", err)
	}
}

// rateLimited applies the per-token tools/call rate limit, reporting
// whether the caller is over budget and, if so, a retry hint in seconds.
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

// ctxNow renders the handler clock as an RFC 3339 UTC timestamp.
func (h *Handler) nowRFC3339() string {
	return h.clock().UTC().Format(time.RFC3339)
}

// today renders the handler clock as a yyyy-MM-dd date.
func (h *Handler) today() string {
	return h.clock().UTC().Format("2006-01-02")
}
