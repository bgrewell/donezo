package api

import (
	"bytes"
	"context"
	"encoding/json"
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

// fixedClock keeps API tests deterministic.
func fixedClock() time.Time {
	return time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
}

// newTestServer builds a Server over temp stores seeded with: user ben
// (the static test identity) owning space "sandbox" with one project,
// and user other owning space "private". The static authenticator keeps
// these endpoint tests independent of session plumbing, which has its
// own tests in auth_test.go. Extra options are applied after the
// defaults, so callers can layer on more (e.g. WithWebUI fixtures).
func newTestServer(t *testing.T, extra ...ServerOption) *Server {
	t.Helper()
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

	ctx := context.Background()
	ben, err := core.CreateUser(ctx, "ben", "Ben")
	if err != nil {
		t.Fatalf("create user ben: %v", err)
	}
	other, err := core.CreateUser(ctx, "other", "Other")
	if err != nil {
		t.Fatalf("create user other: %v", err)
	}
	if _, err := core.CreateSpace(ctx, store.Space{ID: "sandbox", UserID: ben.ID, Name: "Sandbox", Color: "blue"}); err != nil {
		t.Fatalf("create space sandbox: %v", err)
	}
	if _, err := core.CreateSpace(ctx, store.Space{ID: "private", UserID: other.ID, Name: "Private", Color: "rose", Position: 1}); err != nil {
		t.Fatalf("create space private: %v", err)
	}
	if _, err := spaces.CreateProject(ctx, "sandbox", store.Project{
		ID: "loom", Name: "Loom", Color: "blue", Purpose: "p", Outcome: "o",
		CurrentFocus: "cf", NextAction: "na", Status: "active", ResumeContext: "rc",
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}

	quiet := log.New(io.Discard, "", 0)
	opts := append([]ServerOption{WithLogger(quiet), WithAuthenticator(StaticAuthenticator{User: ben})}, extra...)
	return NewServer(core, spaces, opts...)
}

func TestAPIEndpoints(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		check      func(t *testing.T, body []byte)
	}{
		{
			name: "healthz", method: http.MethodGet, path: "/api/healthz", wantStatus: http.StatusOK,
			check: func(t *testing.T, body []byte) {
				t.Helper()
				if got := strings.TrimSpace(string(body)); got != `{"status":"ok"}` {
					t.Errorf("body = %s", got)
				}
			},
		},
		{
			name:   "spaces lists only own spaces with camelCase fields",
			method: http.MethodGet, path: "/api/spaces", wantStatus: http.StatusOK,
			check: func(t *testing.T, body []byte) {
				t.Helper()
				var resp struct {
					Spaces []map[string]any `json:"spaces"`
				}
				if err := json.Unmarshal(body, &resp); err != nil {
					t.Fatalf("parse: %v", err)
				}
				if len(resp.Spaces) != 1 {
					t.Fatalf("spaces = %d, want 1 (ownership filter)", len(resp.Spaces))
				}
				sp := resp.Spaces[0]
				if sp["id"] != "sandbox" {
					t.Errorf("space id = %v", sp["id"])
				}
				if _, ok := sp["userId"]; !ok {
					t.Error("missing camelCase userId field")
				}
				if _, ok := sp["createdAt"]; !ok {
					t.Error("missing camelCase createdAt field")
				}
			},
		},
		{
			name: "space state", method: http.MethodGet, path: "/api/spaces/sandbox/state", wantStatus: http.StatusOK,
			check: func(t *testing.T, body []byte) {
				t.Helper()
				var state struct {
					Projects []map[string]any `json:"projects"`
					Inbox    []any            `json:"inbox"`
				}
				if err := json.Unmarshal(body, &state); err != nil {
					t.Fatalf("parse: %v", err)
				}
				if len(state.Projects) != 1 {
					t.Fatalf("projects = %d, want 1", len(state.Projects))
				}
				p := state.Projects[0]
				for _, field := range []string{"currentFocus", "nextAction", "altNextActions", "resumeContext"} {
					if _, ok := p[field]; !ok {
						t.Errorf("project missing camelCase field %q", field)
					}
				}
				if state.Inbox == nil {
					t.Error("inbox is null, want []")
				}
			},
		},
		{
			name: "unknown space is 404", method: http.MethodGet, path: "/api/spaces/ghost/state",
			wantStatus: http.StatusNotFound,
			check:      checkErrorEnvelope,
		},
		{
			name: "another user's space reads as 404", method: http.MethodGet, path: "/api/spaces/private/state",
			wantStatus: http.StatusNotFound,
			check:      checkErrorEnvelope,
		},
		{
			name: "unknown route is 404 JSON", method: http.MethodGet, path: "/api/nope",
			wantStatus: http.StatusNotFound,
			check:      checkErrorEnvelope,
		},
		{
			name: "wrong method is rejected", method: http.MethodPost, path: "/api/healthz",
			wantStatus: http.StatusMethodNotAllowed,
		},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := newTestServer(t)
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.check != nil {
				tt.check(t, rec.Body.Bytes())
			}
		})
	}
}

// TestWithMCPRateLimiterAppliesToMountedHandler proves the Server actually
// threads a WithMCPRateLimiter option down into the /mcp handler it mounts
// (internal/api/server.go builds that handler internally, so this is the
// only place a wiring regression there would be caught): a limiter capped
// at one call per minute blocks the second tools/call over real HTTP.
func TestWithMCPRateLimiterAppliesToMountedHandler(t *testing.T) {
	t.Parallel()
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
		_ = core.Close()
		_ = spaces.Close()
	})

	ctx := context.Background()
	ben, err := core.CreateUser(ctx, "ben", "Ben")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := core.CreateSpace(ctx, store.Space{ID: "sandbox", UserID: ben.ID, Name: "Sandbox", Color: "blue"}); err != nil {
		t.Fatalf("create space: %v", err)
	}
	token, hash, prefix, err := auth.NewAPIToken()
	if err != nil {
		t.Fatalf("NewAPIToken: %v", err)
	}
	if _, err := core.CreateAPIToken(ctx, store.APIToken{
		ID: "tok-1", UserID: ben.ID, Name: "tok-1", TokenHash: hash, TokenPrefix: prefix,
		Scope: store.ScopeReadWrite,
	}); err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}

	limiter := auth.NewRateLimiter(auth.WithLimit(1), auth.WithWindow(time.Minute), auth.WithLimiterClock(fixedClock))
	quiet := log.New(io.Discard, "", 0)
	srv := NewServer(core, spaces, WithLogger(quiet), WithMCPRateLimiter(limiter))

	call := func() (status int, isError bool) {
		body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_spaces","arguments":{}}}`
		req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		var resp struct {
			Result struct {
				IsError bool `json:"isError"`
			} `json:"result"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		return rec.Code, resp.Result.IsError
	}

	if status, isErr := call(); status != http.StatusOK || isErr {
		t.Fatalf("call 1: status=%d isError=%v, want 200 and not rate-limited", status, isErr)
	}
	if status, isErr := call(); status != http.StatusOK || !isErr {
		t.Errorf("call 2: status=%d isError=%v, want 200 and rate-limited (the injected 1/min limiter must govern the mounted handler)", status, isErr)
	}
}

// checkErrorEnvelope asserts the {"error": string} JSON error shape.
func checkErrorEnvelope(t *testing.T, body []byte) {
	t.Helper()
	var e struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &e); err != nil {
		t.Fatalf("error envelope parse: %v (body %s)", err, body)
	}
	if e.Error == "" {
		t.Errorf("error envelope empty: %s", body)
	}
}
