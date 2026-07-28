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

	"github.com/bgrewell/donezo/internal/auth"
	"github.com/bgrewell/donezo/internal/store"
)

// fixedClock keeps MCP tests deterministic.
func fixedClock() time.Time {
	return time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
}

// fixture bundles a handler over real temp stores with a read_write and a
// read_only token for user ben, who owns space "sandbox" (with project
// "loom"). User other owns "private", used to exercise cross-space denial.
type fixture struct {
	h       *Handler
	core    *store.CoreStore
	spaces  *store.SpaceStore
	user    store.User
	other   store.User
	rwToken string
	roToken string
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

func newFixture(t *testing.T) *fixture {
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
	h := NewHandler(core, spaces,
		WithClock(fixedClock),
		WithLogger(log.New(io.Discard, "", 0)),
		WithVersion("test-1.2.3"),
	)
	return &fixture{
		h: h, core: core, spaces: spaces, user: ben, other: other,
		rwToken: mintToken(t, core, ben.ID, "tok-rw", store.ScopeReadWrite),
		roToken: mintToken(t, core, ben.ID, "tok-ro", store.ScopeReadOnly),
	}
}

// call posts body to /mcp with the given bearer token (blank = no header).
func (f *fixture) call(t *testing.T, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	return rec
}

// rpcResp is a decoded JSON-RPC response.
type rpcResp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func parseRPC(t *testing.T, rec *httptest.ResponseRecorder) rpcResp {
	t.Helper()
	var resp rpcResp
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse rpc response %q: %v", rec.Body.String(), err)
	}
	if resp.JSONRPC != "2.0" {
		t.Errorf("jsonrpc = %q, want 2.0", resp.JSONRPC)
	}
	return resp
}

// toolResultBody is the decoded tools/call result.
type toolResultBody struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	IsError bool `json:"isError"`
}

// callTool runs a tools/call and returns the first text block and isError.
func (f *fixture) callTool(t *testing.T, token, name, argsJSON string) (string, bool) {
	t.Helper()
	if argsJSON == "" {
		argsJSON = "{}"
	}
	body := fmt.Sprintf(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":%q,"arguments":%s}}`, name, argsJSON)
	rec := f.call(t, token, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("tools/call %s: HTTP %d (body %s)", name, rec.Code, rec.Body.String())
	}
	resp := parseRPC(t, rec)
	if resp.Error != nil {
		t.Fatalf("tools/call %s: protocol error %+v", name, resp.Error)
	}
	var res toolResultBody
	if err := json.Unmarshal(resp.Result, &res); err != nil {
		t.Fatalf("parse tool result: %v", err)
	}
	if len(res.Content) == 0 || res.Content[0].Type != "text" {
		t.Fatalf("tool result missing text content: %s", resp.Result)
	}
	return res.Content[0].Text, res.IsError
}

// ─── Protocol tests ───────────────────────────────────────────────────────

func TestInitialize(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	tests := []struct {
		name        string
		requested   string
		wantVersion string
	}{
		{name: "echo latest", requested: "2025-06-18", wantVersion: "2025-06-18"},
		{name: "echo supported older", requested: "2024-11-05", wantVersion: "2024-11-05"},
		{name: "negotiate unknown to latest", requested: "1.0", wantVersion: ProtocolVersion},
	}
	for _, tt := range tests {
		tt := tt // capture (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":%q,"capabilities":{},"clientInfo":{"name":"c","version":"1"}}}`, tt.requested)
			rec := f.call(t, f.rwToken, body)
			if rec.Code != http.StatusOK {
				t.Fatalf("HTTP %d", rec.Code)
			}
			resp := parseRPC(t, rec)
			var result struct {
				ProtocolVersion string `json:"protocolVersion"`
				Capabilities    struct {
					Tools *struct{} `json:"tools"`
				} `json:"capabilities"`
				ServerInfo struct {
					Name    string `json:"name"`
					Version string `json:"version"`
				} `json:"serverInfo"`
				Instructions string `json:"instructions"`
			}
			if err := json.Unmarshal(resp.Result, &result); err != nil {
				t.Fatalf("parse: %v", err)
			}
			if result.ProtocolVersion != tt.wantVersion {
				t.Errorf("protocolVersion = %q, want %q", result.ProtocolVersion, tt.wantVersion)
			}
			if result.Capabilities.Tools == nil {
				t.Error("missing tools capability")
			}
			if result.ServerInfo.Name != "donezo" || result.ServerInfo.Version != "test-1.2.3" {
				t.Errorf("serverInfo = %+v", result.ServerInfo)
			}
			if !strings.Contains(result.Instructions, "list_spaces") {
				t.Error("instructions should orient the caller to list_spaces")
			}
		})
	}
}

func TestNotificationsInitialized(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	rec := f.call(t, f.rwToken, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if rec.Code != http.StatusAccepted {
		t.Errorf("status = %d, want 202", rec.Code)
	}
	if strings.TrimSpace(rec.Body.String()) != "" {
		t.Errorf("notification should have no body, got %q", rec.Body.String())
	}
}

func TestPing(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	rec := f.call(t, f.rwToken, `{"jsonrpc":"2.0","id":9,"method":"ping"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	resp := parseRPC(t, rec)
	if resp.Error != nil {
		t.Fatalf("ping error: %+v", resp.Error)
	}
	if strings.TrimSpace(string(resp.Result)) != "{}" {
		t.Errorf("ping result = %s, want {}", resp.Result)
	}
}

func TestToolsListScopes(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	listFor := func(token string) []string {
		rec := f.call(t, token, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		resp := parseRPC(t, rec)
		var result struct {
			Tools []struct {
				Name        string         `json:"name"`
				Description string         `json:"description"`
				InputSchema map[string]any `json:"inputSchema"`
			} `json:"tools"`
		}
		if err := json.Unmarshal(resp.Result, &result); err != nil {
			t.Fatalf("parse: %v", err)
		}
		names := make([]string, 0, len(result.Tools))
		for _, tl := range result.Tools {
			if tl.Description == "" {
				t.Errorf("tool %s missing description", tl.Name)
			}
			if tl.InputSchema["type"] != "object" {
				t.Errorf("tool %s inputSchema type = %v", tl.Name, tl.InputSchema["type"])
			}
			names = append(names, tl.Name)
		}
		return names
	}

	rw := listFor(f.rwToken)
	if len(rw) != len(tools) {
		t.Errorf("read_write listed %d tools, want %d (all)", len(rw), len(tools))
	}
	ro := listFor(f.roToken)
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
	text, isErr := f.callTool(t, f.rwToken, "list_spaces", "{}")
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
	text, isErr := f.callTool(t, f.roToken, "capture_to_inbox", `{"space_id":"sandbox","text":"hi"}`)
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
			rec := f.call(t, tt.token, body)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401 (body %s)", rec.Code, rec.Body.String())
			}
			if got := rec.Header().Get("WWW-Authenticate"); !strings.HasPrefix(got, "Bearer") {
				t.Errorf("WWW-Authenticate = %q, want a Bearer challenge", got)
			}
		})
	}
}

// TestAuthSchemeCaseInsensitive proves the Authorization scheme match
// follows RFC 7235 (auth-scheme is case-insensitive): a spec-compliant
// client sending "bearer" or "BEARER" authenticates the same as "Bearer".
func TestAuthSchemeCaseInsensitive(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	for _, scheme := range []string{"Bearer", "bearer", "BEARER", "BeArEr"} {
		scheme := scheme
		t.Run(scheme, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
			req.Header.Set("Authorization", scheme+" "+f.rwToken)
			rec := httptest.NewRecorder()
			f.h.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Errorf("scheme %q: status = %d, want 200 (body %s)", scheme, rec.Code, rec.Body.String())
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
	if got := rec.Header().Get("Allow"); got != http.MethodPost {
		t.Errorf("Allow = %q, want POST", got)
	}
}

func TestMalformedJSONRPC(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	tests := []struct {
		name       string
		body       string
		wantHTTP   int
		wantCode   int
		wantResult bool // expect a JSON-RPC error object
	}{
		{name: "parse error", body: `{not json`, wantHTTP: http.StatusBadRequest, wantCode: codeParseError, wantResult: true},
		{name: "batch rejected", body: `[{"jsonrpc":"2.0","id":1,"method":"ping"}]`, wantHTTP: http.StatusBadRequest, wantCode: codeInvalidRequest, wantResult: true},
		{name: "missing jsonrpc", body: `{"id":1,"method":"ping"}`, wantHTTP: http.StatusBadRequest, wantCode: codeInvalidRequest, wantResult: true},
		{name: "empty body", body: ``, wantHTTP: http.StatusBadRequest, wantCode: codeInvalidRequest, wantResult: true},
		{name: "unknown method", body: `{"jsonrpc":"2.0","id":1,"method":"resources/list"}`, wantHTTP: http.StatusOK, wantCode: codeMethodNotFound, wantResult: true},
		{name: "unknown tool", body: `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"frobnicate","arguments":{}}}`, wantHTTP: http.StatusOK, wantCode: codeInvalidParams, wantResult: true},
		{name: "tools/call missing name", body: `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{}}`, wantHTTP: http.StatusOK, wantCode: codeInvalidParams, wantResult: true},
	}
	for _, tt := range tests {
		tt := tt // capture (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := f.call(t, f.rwToken, tt.body)
			if rec.Code != tt.wantHTTP {
				t.Fatalf("HTTP %d, want %d (body %s)", rec.Code, tt.wantHTTP, rec.Body.String())
			}
			resp := parseRPC(t, rec)
			if resp.Error == nil {
				t.Fatalf("want JSON-RPC error, got %s", rec.Body.String())
			}
			if resp.Error.Code != tt.wantCode {
				t.Errorf("error code = %d, want %d", resp.Error.Code, tt.wantCode)
			}
		})
	}
}

// TestRateLimitAppliesToUnknownToolAndMalformedParams proves the rate-limit
// check runs before the tool-name lookup: an unknown tool name and
// malformed params must still cost budget, or a caller could probe around
// the ceiling for free with cheap invalid calls.
func TestRateLimitAppliesToUnknownToolAndMalformedParams(t *testing.T) {
	t.Parallel()
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
	// A limiter of 1/min on a frozen clock: the second call of any shape
	// must be blocked, even an unknown-tool or malformed-params one.
	limiter := auth.NewRateLimiter(auth.WithLimit(1), auth.WithWindow(time.Minute), auth.WithLimiterClock(fixedClock))
	h := NewHandler(core, spaces, WithClock(fixedClock), WithLogger(log.New(io.Discard, "", 0)), WithRateLimiter(limiter))
	f := &fixture{h: h, core: core, spaces: spaces, user: ben,
		rwToken: mintToken(t, core, ben.ID, "tok-rw", store.ScopeReadWrite)}

	// First call: burns the only unit of budget, regardless of shape.
	f.call(t, f.rwToken, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"not_a_real_tool"}}`)

	unknown := parseRPC(t, f.call(t, f.rwToken,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"still_not_real"}}`))
	if unknown.Error != nil {
		t.Errorf("unknown-tool call after limit hit: got RPC error %+v, want a rate-limited tool result (RPC error means it bypassed the limiter and reached the unknown-tool check)", unknown.Error)
	}

	malformed := parseRPC(t, f.call(t, f.rwToken,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{}}`))
	if malformed.Error != nil {
		t.Errorf("malformed-params call after limit hit: got RPC error %+v, want a rate-limited tool result (RPC error means it bypassed the limiter and reached the params check)", malformed.Error)
	}
}

func TestToolCallRateLimited(t *testing.T) {
	t.Parallel()
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
	// A limiter of 2/min on a frozen clock: the third call is blocked.
	limiter := auth.NewRateLimiter(auth.WithLimit(2), auth.WithWindow(time.Minute), auth.WithLimiterClock(fixedClock))
	h := NewHandler(core, spaces, WithClock(fixedClock), WithLogger(log.New(io.Discard, "", 0)), WithRateLimiter(limiter))
	f := &fixture{h: h, core: core, spaces: spaces, user: ben,
		rwToken: mintToken(t, core, ben.ID, "tok-rw", store.ScopeReadWrite)}

	if _, isErr := f.callTool(t, f.rwToken, "list_spaces", "{}"); isErr {
		t.Fatal("call 1 should pass")
	}
	if _, isErr := f.callTool(t, f.rwToken, "list_spaces", "{}"); isErr {
		t.Fatal("call 2 should pass")
	}
	text, isErr := f.callTool(t, f.rwToken, "list_spaces", "{}")
	if !isErr || !strings.Contains(text, "Rate limit") {
		t.Errorf("call 3 should be rate-limited, got isError=%v text=%s", isErr, text)
	}
}
