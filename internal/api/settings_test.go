package api

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bgrewell/donezo/internal/store"
)

// settingsBody unmarshals a settings response envelope.
func settingsBody(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var resp struct {
		Settings map[string]any `json:"settings"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("parse settings response: %v (body %s)", err, body)
	}
	return resp.Settings
}

func TestSettingsRoundTrip(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)

	// A user who has never saved a preference gets an empty object, not a
	// 404 — no stored preferences is a normal state.
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET settings = %d, want 200 (body %s)", rec.Code, rec.Body)
	}
	if got := settingsBody(t, rec.Body.Bytes()); len(got) != 0 {
		t.Errorf("unset settings = %v, want empty object", got)
	}

	// A patch returns the full stored set, so a caller never merges itself.
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/settings",
		bytes.NewBufferString(`{"theme":"paper","font":"inter"}`))
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH settings = %d, want 200 (body %s)", rec.Code, rec.Body)
	}
	got := settingsBody(t, rec.Body.Bytes())
	if got["theme"] != "paper" || got["font"] != "inter" {
		t.Errorf("patched settings = %v", got)
	}

	// A second patch leaves untouched fields alone.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/api/settings",
		bytes.NewBufferString(`{"fontSize":"large"}`))
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("second PATCH = %d, want 200 (body %s)", rec.Code, rec.Body)
	}
	got = settingsBody(t, rec.Body.Bytes())
	if got["theme"] != "paper" || got["font"] != "inter" || got["fontSize"] != "large" {
		t.Errorf("merged settings = %v", got)
	}

	// An empty string clears a preference so it follows the default again.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/api/settings",
		bytes.NewBufferString(`{"theme":""}`))
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("clearing PATCH = %d, want 200 (body %s)", rec.Code, rec.Body)
	}
	got = settingsBody(t, rec.Body.Bytes())
	if _, present := got["theme"]; present {
		t.Errorf("cleared theme should be omitted, got %v", got)
	}
	if got["font"] != "inter" {
		t.Errorf("clearing one preference disturbed another: %v", got)
	}

	// And the clear is durable, not just echoed.
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	got = settingsBody(t, rec.Body.Bytes())
	if _, present := got["theme"]; present {
		t.Errorf("cleared theme reappeared on read: %v", got)
	}
}

func TestSettingsPatchRejectsBadInput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		want string
	}{
		{"unknown theme", `{"theme":"neon"}`, "theme must be one of"},
		{"unknown font", `{"font":"comic"}`, "font must be one of"},
		{"unknown font size", `{"fontSize":"enormous"}`, "fontSize must be one of"},
		{"unknown field", `{"colour":"paper"}`, ""},
		{"not an object", `["paper"]`, ""},
		{"trailing content", `{"theme":"paper"}{}`, ""},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := newTestServer(t)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPatch, "/api/settings", bytes.NewBufferString(tt.body))
			srv.Handler().ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body)
			}
			if tt.want != "" && !strings.Contains(rec.Body.String(), tt.want) {
				t.Errorf("body = %s, want it to mention %q", rec.Body, tt.want)
			}
		})
	}
}

// Settings are per-user and carry no user id in the path, so an
// unauthenticated caller must be refused rather than served a default set.
// The server is built without WithAuthenticator so the real session-backed
// authenticator runs and a request with no cookie is genuinely anonymous —
// StaticAuthenticator{} would still authenticate, as a zero-value user.
func TestSettingsRequireAuth(t *testing.T) {
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
		if err := core.Close(); err != nil {
			t.Errorf("close core: %v", err)
		}
		if err := spaces.Close(); err != nil {
			t.Errorf("close spaces: %v", err)
		}
	})
	h := NewServer(core, spaces, WithLogger(log.New(io.Discard, "", 0))).Handler()

	for _, tc := range []struct {
		method, body string
	}{
		{http.MethodGet, ""},
		{http.MethodPatch, `{"theme":"paper"}`},
	} {
		rec := httptest.NewRecorder()
		var req *http.Request
		if tc.body == "" {
			req = httptest.NewRequest(tc.method, "/api/settings", nil)
		} else {
			req = httptest.NewRequest(tc.method, "/api/settings", bytes.NewBufferString(tc.body))
		}
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s unauthenticated = %d, want 401 (body %s)", tc.method, rec.Code, rec.Body)
		}
	}
}
