package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/bgrewell/donezo/internal/auth"
	"github.com/bgrewell/donezo/internal/store"
)

// These tests prove the same behavioral contract the hand-rolled server
// established, now against the official MCP Go SDK. The protocol-level
// behaviors (initialize, tools/list scope filtering, tools/call, scope
// enforcement, cross-space denial, rate limiting) are exercised through the
// SDK's own client over a real HTTP server — two independent implementations
// agreeing on the wire — while the auth/transport edge cases that need raw
// HTTP control (missing/malformed headers, case-insensitive scheme, revoked
// tokens, GET, oversized bodies) drive the handler directly with httptest.

// fixedClock keeps MCP tests deterministic.
func fixedClock() time.Time {
	return time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
}

// fixture bundles a handler over real temp stores, served over a live HTTP
// test server, with a read_write and a read_only token for user ben, who
// owns space "sandbox" (with project "loom"). User other owns "private",
// used to exercise cross-space denial. rw and ro are connected SDK client
// sessions for those tokens.
type fixture struct {
	h       *Handler
	core    *store.CoreStore
	spaces  *store.SpaceStore
	user    store.User
	other   store.User
	rwToken string
	roToken string
	server  *httptest.Server
	rw      *mcpsdk.ClientSession
	ro      *mcpsdk.ClientSession
}

// mintToken generates an API token, stores it under the given id/scope, and
// returns the plaintext bearer value.
func mintToken(t *testing.T, core *store.CoreStore, userID int64, id, scope string) string {
	t.Helper()
	token, hash, prefix, err := auth.NewAPIToken()
	if err != nil {
		t.Fatalf("NewAPIToken: %v", err)
	}
	if _, err := core.CreateAPIToken(context.Background(), store.APIToken{
		ID: id, UserID: userID, Name: id, TokenHash: hash, TokenPrefix: prefix, Scope: scope,
	}); err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}
	return token
}

// newFixture builds the standard fixture. extra options are appended after
// the defaults, so a test can override the clock or the instance zone.
func newFixture(t *testing.T, extra ...Option) *fixture {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	core, err := store.NewCoreStore(store.WithDataDir(dir), store.WithClock(fixedClock))
	if err != nil {
		t.Fatalf("NewCoreStore: %v", err)
	}
	spaces, err := store.NewSpaceStore(store.WithDataDir(dir), store.WithClock(fixedClock))
	if err != nil {
		t.Fatalf("NewSpaceStore: %v", err)
	}
	t.Cleanup(func() {
		if err := core.Close(); err != nil {
			t.Errorf("close core: %v", err)
		}
		if err := spaces.Close(); err != nil {
			t.Errorf("close spaces: %v", err)
		}
	})
	ben, err := core.CreateUser(ctx, "ben", "Ben")
	if err != nil {
		t.Fatalf("create ben: %v", err)
	}
	other, err := core.CreateUser(ctx, "other", "Other")
	if err != nil {
		t.Fatalf("create other: %v", err)
	}
	if _, err := core.CreateSpace(ctx, store.Space{ID: "sandbox", UserID: ben.ID, Name: "Sandbox", Color: "blue"}); err != nil {
		t.Fatalf("create sandbox: %v", err)
	}
	if _, err := core.CreateSpace(ctx, store.Space{ID: "private", UserID: other.ID, Name: "Private", Color: "rose", Position: 1}); err != nil {
		t.Fatalf("create private: %v", err)
	}
	if _, err := spaces.CreateProject(ctx, "sandbox", store.Project{
		ID: "loom", Name: "Loom", Color: "blue", Purpose: "p", Outcome: "o",
		CurrentFocus: "cf", NextAction: "na", Status: "active", ResumeContext: "rc",
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	h := NewHandler(core, spaces, append([]Option{
		WithClock(fixedClock),
		// UTC by default so a test that says nothing about zones is not
		// silently steered by the machine it runs on.
		WithLocation(time.UTC),
		WithLogger(log.New(io.Discard, "", 0)),
		WithVersion("test-1.2.3"),
	}, extra...)...)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	f := &fixture{
		h: h, core: core, spaces: spaces, user: ben, other: other, server: srv,
		rwToken: mintToken(t, core, ben.ID, "tok-rw", store.ScopeReadWrite),
		roToken: mintToken(t, core, ben.ID, "tok-ro", store.ScopeReadOnly),
	}
	f.rw = f.connect(t, f.rwToken)
	f.ro = f.connect(t, f.roToken)
	return f
}

// bearerRT injects an Authorization: Bearer header onto every request, so a
// real SDK client authenticates the way an MCP client configured with a
// token does.
type bearerRT struct {
	token string
	base  http.RoundTripper
}

func (b *bearerRT) RoundTrip(r *http.Request) (*http.Response, error) {
	r2 := r.Clone(r.Context())
	if b.token != "" {
		r2.Header.Set("Authorization", "Bearer "+b.token)
	}
	base := b.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(r2)
}

// connect opens an SDK client session to the fixture server using token.
func (f *fixture) connect(t *testing.T, token string) *mcpsdk.ClientSession {
	t.Helper()
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "donezo-test-client", Version: "1"}, nil)
	sess, err := client.Connect(context.Background(), &mcpsdk.StreamableClientTransport{
		Endpoint:             f.server.URL,
		HTTPClient:           &http.Client{Transport: &bearerRT{token: token}},
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatalf("connect (token scope): %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	return sess
}

// callTool runs a tools/call over the given session and returns the first
// text block and isError. A protocol-level error (e.g. unknown tool) fails
// the test — handler-level validation surfaces as an isError result instead.
func (f *fixture) callTool(t *testing.T, sess *mcpsdk.ClientSession, name, argsJSON string) (string, bool) {
	t.Helper()
	if argsJSON == "" {
		argsJSON = "{}"
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		t.Fatalf("decode args %s: %v", argsJSON, err)
	}
	res, err := sess.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("tools/call %s: protocol error %v", name, err)
	}
	if len(res.Content) == 0 {
		t.Fatalf("tools/call %s: no content", name)
	}
	tc, ok := res.Content[0].(*mcpsdk.TextContent)
	if !ok {
		t.Fatalf("tools/call %s: content[0] is %T, want *TextContent", name, res.Content[0])
	}
	return tc.Text, res.IsError
}

// listToolNames returns the tool names visible over the given session.
func listToolNames(t *testing.T, sess *mcpsdk.ClientSession) []string {
	t.Helper()
	res, err := sess.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	names := make([]string, 0, len(res.Tools))
	for _, tl := range res.Tools {
		if tl.Description == "" {
			t.Errorf("tool %s missing description", tl.Name)
		}
		if m, ok := tl.InputSchema.(map[string]any); !ok || m["type"] != "object" {
			t.Errorf("tool %s inputSchema type = %v", tl.Name, tl.InputSchema)
		}
		names = append(names, tl.Name)
	}
	return names
}

// ─── Raw HTTP helpers (auth/transport edge cases) ──────────────────────────

// rpcResp is a decoded single JSON-RPC response body.
type rpcResp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// rawPost drives the handler directly (no SDK client), setting the headers a
// stateless Streamable HTTP request requires. A blank token sends no
// Authorization header.
func (f *fixture) rawPost(t *testing.T, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	return rec
}

// ─── Protocol tests (SDK client interop) ───────────────────────────────────

func TestInitialize(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	rw := f.rw.InitializeResult()
	if rw == nil {
		t.Fatal("nil initialize result")
	}
	if rw.ServerInfo == nil || rw.ServerInfo.Name != "donezo" || rw.ServerInfo.Version != "test-1.2.3" {
		t.Errorf("serverInfo = %+v", rw.ServerInfo)
	}
	if rw.ProtocolVersion == "" {
		t.Error("empty negotiated protocol version")
	}
	if !strings.Contains(rw.Instructions, "list_spaces") || !strings.Contains(rw.Instructions, "read_write") {
		t.Errorf("rw instructions not scope-tailored: %q", rw.Instructions)
	}
	ro := f.ro.InitializeResult()
	if !strings.Contains(ro.Instructions, "read_only") {
		t.Errorf("ro instructions not scope-tailored: %q", ro.Instructions)
	}
	// The tools capability is proven usable by a successful list.
	if _, err := f.rw.ListTools(context.Background(), nil); err != nil {
		t.Errorf("tools capability not usable: %v", err)
	}
}

// TestPing proves ping's actual, intentional shape: it is entirely SDK/
// protocol-defined behavior donezo does not implement or control. On
// protocol revisions that still carry ping (<= 2025-11-25) it answers {};
// the SDK's default client negotiates the current 2026-07-28 protocol,
// where the sessionless redesign removed ping outright, so a real default
// client's Ping() is EXPECTED to fail. Pinning that failure here (rather
// than only exercising the legacy path) stops a future reader from
// assuming ping works for production clients.
func TestPing(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	t.Run("answered on a protocol revision that still carries it", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":9,"method":"ping"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("Mcp-Protocol-Version", "2025-06-18")
		req.Header.Set("Authorization", "Bearer "+f.rwToken)
		rec := httptest.NewRecorder()
		f.h.ServeHTTP(rec, req)
		resp := parseRawRPC(t, rec)
		if resp.Error != nil {
			t.Fatalf("ping error: %+v", resp.Error)
		}
		if strings.TrimSpace(string(resp.Result)) != "{}" {
			t.Errorf("ping result = %s, want {}", resp.Result)
		}
	})

	t.Run("unsupported on the default negotiated protocol, by design", func(t *testing.T) {
		t.Parallel()
		if err := f.rw.Ping(context.Background(), nil); err == nil {
			t.Fatal("Ping() succeeded on the default-negotiated protocol; ping is expected to be unsupported there (2026-07-28's sessionless redesign removed it) — if the SDK now supports it, update this test AND the docs claiming ping is a legacy-only keepalive")
		}
	})
}

func TestToolsListScopes(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	rw := listToolNames(t, f.rw)
	if len(rw) != len(tools) {
		t.Errorf("read_write listed %d tools, want %d (all)", len(rw), len(tools))
	}
	ro := listToolNames(t, f.ro)
	wantRead := 0
	for _, tl := range tools {
		if !tl.write {
			wantRead++
		}
	}
	if len(ro) != wantRead {
		t.Errorf("read_only listed %d tools, want %d (read only)", len(ro), wantRead)
	}
	for _, name := range ro {
		if toolByName[name].write {
			t.Errorf("read_only listing includes write tool %q", name)
		}
	}
}

func TestToolsCallHappy(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	text, isErr := f.callTool(t, f.rw, "list_spaces", "{}")
	if isErr {
		t.Fatalf("list_spaces isError: %s", text)
	}
	if !strings.Contains(text, "sandbox") {
		t.Errorf("list_spaces should include sandbox, got %s", text)
	}
	if strings.Contains(text, "private") {
		t.Error("list_spaces leaked another user's space")
	}
}

func TestReadOnlyBlocksWrite(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	text, isErr := f.callTool(t, f.ro, "capture_to_inbox", `{"space_id":"sandbox","text":"hi"}`)
	if !isErr {
		t.Fatal("read_only write call should be an isError result")
	}
	if !strings.Contains(text, "read_write") {
		t.Errorf("rejection should name the required scope, got %s", text)
	}
	// And it really did not write.
	items, err := f.spaces.ListInboxItems(context.Background(), "sandbox")
	if err != nil {
		t.Fatalf("list inbox: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("read_only call wrote %d inbox items", len(items))
	}
}

// ─── Auth / transport edge cases (raw HTTP) ────────────────────────────────

func TestAuthFailures(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	revoked := mintToken(t, f.core, f.user.ID, "tok-revoked", store.ScopeReadOnly)
	if err := f.core.RevokeAPIToken(context.Background(), f.user.ID, "tok-revoked"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	tests := []struct {
		name  string
		token string
	}{
		{name: "no token", token: ""},
		{name: "not a donezo token", token: "sk-whatever"},
		{name: "unknown token", token: auth.APITokenScheme + "AAAAAAAAAAAAAAAAAAAAAAAAAA"},
		{name: "revoked token", token: revoked},
	}
	for _, tt := range tests {
		tt := tt // capture (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := f.rawPost(t, tt.token, body)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401 (body %s)", rec.Code, rec.Body.String())
			}
			if got := rec.Header().Get("WWW-Authenticate"); !strings.HasPrefix(got, "Bearer") {
				t.Errorf("WWW-Authenticate = %q, want a Bearer challenge", got)
			}
		})
	}
}

// TestAuthSchemeCaseInsensitive proves the Authorization scheme match follows
// RFC 7235 (auth-scheme is case-insensitive): a spec-compliant client sending
// "bearer" or "BEARER" authenticates the same as "Bearer".
func TestAuthSchemeCaseInsensitive(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	for _, scheme := range []string{"Bearer", "bearer", "BEARER", "BeArEr"} {
		scheme := scheme
		t.Run(scheme, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			req.Header.Set("Authorization", scheme+" "+f.rwToken)
			rec := httptest.NewRecorder()
			f.h.ServeHTTP(rec, req)
			// Reaching the SDK (not the 401 path) is the point: any status
			// other than 401 means authentication succeeded.
			if rec.Code == http.StatusUnauthorized {
				t.Errorf("scheme %q: got 401, want authenticated (body %s)", scheme, rec.Body.String())
			}
		})
	}
}

func TestGETNotAllowed(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+f.rwToken)
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET status = %d, want 405", rec.Code)
	}
	if got := rec.Header().Get("Allow"); !strings.Contains(got, http.MethodPost) {
		t.Errorf("Allow = %q, want to contain POST", got)
	}
}

func TestBodyTooLarge(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	// A JSON body past the 1 MiB cap: the SDK transport rejects it with 413.
	big := strings.Repeat("a", maxMCPBodyBytes+1024)
	body := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"capture_to_inbox","arguments":{"space_id":"sandbox","text":%q}}}`, big)
	rec := f.rawPost(t, f.rwToken, body)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized body status = %d, want 413 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestMalformedJSONRejected(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	// Malformed input and unknown methods are rejected before any tool runs,
	// with a 4xx and never a successful result: an unparseable body, an empty
	// body, and a genuinely unknown JSON-RPC method.
	for _, tt := range []struct {
		name, body string
	}{
		{"parse error", `{not json`},
		{"empty body", ``},
		{"unknown method", `{"jsonrpc":"2.0","id":1,"method":"donezo/nope"}`},
	} {
		tt := tt // capture (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := f.rawPost(t, f.rwToken, tt.body)
			if rec.Code < 400 || rec.Code >= 500 {
				t.Errorf("%s: status = %d, want 4xx (body %s)", tt.name, rec.Code, rec.Body.String())
			}
		})
	}
}

// TestNotificationsInitialized proves a fire-and-forget notification (no id)
// is accepted without a JSON-RPC error — the SDK client sends this on every
// connect, so the whole interop suite depends on it, but assert it directly.
func TestNotificationsInitialized(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	rec := f.rawPost(t, f.rwToken, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if rec.Code >= 300 {
		t.Errorf("notification status = %d, want 2xx (body %s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"error"`) {
		t.Errorf("notification should not error, got %s", rec.Body.String())
	}
}

// ─── Rate limiting ─────────────────────────────────────────────────────────

// rateLimitFixture builds a handler with a custom-limit tools/call limiter on
// the frozen clock, plus a live server. It seeds ben + sandbox and returns a
// fixture with only the rw token/session populated.
func rateLimitFixture(t *testing.T, limit int) *fixture {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	core, err := store.NewCoreStore(store.WithDataDir(dir), store.WithClock(fixedClock))
	if err != nil {
		t.Fatalf("core: %v", err)
	}
	spaces, err := store.NewSpaceStore(store.WithDataDir(dir), store.WithClock(fixedClock))
	if err != nil {
		t.Fatalf("spaces: %v", err)
	}
	t.Cleanup(func() {
		_ = core.Close()
		_ = spaces.Close()
	})
	ben, err := core.CreateUser(ctx, "ben", "Ben")
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	if _, err := core.CreateSpace(ctx, store.Space{ID: "sandbox", UserID: ben.ID, Name: "S", Color: "blue"}); err != nil {
		t.Fatalf("space: %v", err)
	}
	limiter := auth.NewRateLimiter(auth.WithLimit(limit), auth.WithWindow(time.Minute), auth.WithLimiterClock(fixedClock))
	h := NewHandler(core, spaces, WithClock(fixedClock), WithLogger(log.New(io.Discard, "", 0)), WithRateLimiter(limiter))
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	f := &fixture{h: h, core: core, spaces: spaces, user: ben, server: srv,
		rwToken: mintToken(t, core, ben.ID, "tok-rw", store.ScopeReadWrite)}
	return f
}

// TestRateLimitAppliesToUnknownToolAndMalformedParams proves the rate-limit
// check runs before the tool-name lookup: once budget is spent, an
// unknown-tool call and a missing-name call must both come back as a
// rate-limited tool result (isError), NOT as a protocol error — a protocol
// error would mean the limiter was bypassed and the tool-name/params check was
// reached.
func TestRateLimitAppliesToUnknownToolAndMalformedParams(t *testing.T) {
	t.Parallel()
	f := rateLimitFixture(t, 1) // one tools/call of any shape, then blocked

	// First call spends the only unit of budget (its own shape is irrelevant).
	f.rawPost(t, f.rwToken, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"not_a_real_tool","arguments":{}}}`)

	unknown := parseRawRPC(t, f.rawPost(t, f.rwToken,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"still_not_real","arguments":{}}}`))
	assertRateLimited(t, "unknown-tool after limit", unknown)

	missing := parseRawRPC(t, f.rawPost(t, f.rwToken,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"","arguments":{}}}`))
	assertRateLimited(t, "missing-name after limit", missing)
}

// assertRateLimited fails unless resp is a successful tool result flagged
// isError with a "Rate limit" message (never a JSON-RPC error object).
func assertRateLimited(t *testing.T, label string, resp rpcResp) {
	t.Helper()
	if resp.Error != nil {
		t.Errorf("%s: got RPC error %+v, want a rate-limited tool result (RPC error means the limiter was bypassed)", label, resp.Error)
		return
	}
	var res toolResultBody
	if err := json.Unmarshal(resp.Result, &res); err != nil {
		t.Fatalf("%s: parse result %s: %v", label, resp.Result, err)
	}
	if !res.IsError || len(res.Content) == 0 || !strings.Contains(res.Content[0].Text, "Rate limit") {
		t.Errorf("%s: result = %+v, want isError with Rate limit", label, res)
	}
}

// toolResultBody is the decoded tools/call result payload.
type toolResultBody struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	IsError bool `json:"isError"`
}

// parseRawRPC decodes a single JSON-RPC response body from a raw POST. The
// stateless server with JSONResponse returns one application/json object.
func parseRawRPC(t *testing.T, rec *httptest.ResponseRecorder) rpcResp {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("HTTP %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var resp rpcResp
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse rpc response %q: %v", rec.Body.String(), err)
	}
	return resp
}

func TestToolCallRateLimited(t *testing.T) {
	t.Parallel()
	f := rateLimitFixture(t, 2) // third call blocked
	sess := f.connect(t, f.rwToken)

	if _, isErr := f.callTool(t, sess, "list_spaces", "{}"); isErr {
		t.Fatal("call 1 should pass")
	}
	if _, isErr := f.callTool(t, sess, "list_spaces", "{}"); isErr {
		t.Fatal("call 2 should pass")
	}
	text, isErr := f.callTool(t, sess, "list_spaces", "{}")
	if !isErr || !strings.Contains(text, "Rate limit") {
		t.Errorf("call 3 should be rate-limited, got isError=%v text=%s", isErr, text)
	}
}
