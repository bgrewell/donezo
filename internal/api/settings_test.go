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

// A wrong method on a known path must name the right one. Falling through
// to the catch-all 404 reads as "this server has no settings endpoint",
// which sends a client hunting for a version mismatch.
func TestSettingsWrongMethod(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	for _, method := range []string{http.MethodPost, http.MethodDelete, http.MethodPut} {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(method, "/api/settings", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s /api/settings = %d, want 405 (body %s)", method, rec.Code, rec.Body)
		}
		if allow := rec.Header().Get("Allow"); allow != "GET, PATCH" {
			t.Errorf("%s Allow = %q, want %q", method, allow, "GET, PATCH")
		}
	}
}

// Onboarding progress records something that already happened, so a patch may
// only ever move it forward. The scenario this protects: a second browser
// mounts with empty local state and syncs before it has read the server, which
// would otherwise clear the flag everywhere and bring the welcome dialog back.
func TestSettingsOnboardingFlagsAreMonotonic(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		patches      []string
		wantWelcomed bool
		wantTourDone bool
	}{
		{
			name:         "flags set",
			patches:      []string{`{"welcomed":true,"tourDone":true}`},
			wantWelcomed: true,
			wantTourDone: true,
		},
		{
			name: "false cannot clear either set flag",
			patches: []string{
				`{"welcomed":true,"tourDone":true}`,
				`{"welcomed":false,"tourDone":false}`,
			},
			wantWelcomed: true,
			wantTourDone: true,
		},
		{
			name:         "welcomed false cannot clear tourDone either",
			patches:      []string{`{"tourDone":true}`, `{"welcomed":false,"tourDone":false}`},
			wantTourDone: true,
		},
		{
			name:         "false on unset flags leaves them unset",
			patches:      []string{`{"welcomed":false,"tourDone":false}`},
			wantWelcomed: false,
			wantTourDone: false,
		},
		{
			name:         "unrelated patch leaves progress alone",
			patches:      []string{`{"welcomed":true}`, `{"theme":"paper"}`},
			wantWelcomed: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := newTestServer(t).Handler()
			for _, body := range tt.patches {
				if rec := doJSON(t, h, http.MethodPatch, "/api/settings", body); rec.Code != http.StatusOK {
					t.Fatalf("patch %s = %d (body %s)", body, rec.Code, rec.Body)
				}
			}
			// Assert against a fresh read, not the patch response: the stored
			// document is what a new browser will be handed.
			got := settingsBody(t, doJSON(t, h, http.MethodGet, "/api/settings", "").Body.Bytes())
			if welcomed, _ := got["welcomed"].(bool); welcomed != tt.wantWelcomed {
				t.Errorf("welcomed = %v, want %v (stored %+v)", welcomed, tt.wantWelcomed, got)
			}
			if tourDone, _ := got["tourDone"].(bool); tourDone != tt.wantTourDone {
				t.Errorf("tourDone = %v, want %v (stored %+v)", tourDone, tt.wantTourDone, got)
			}
		})
	}
}

func TestSettingsDismissedHintsAccumulate(t *testing.T) {
	t.Parallel()
	h := newTestServer(t).Handler()
	for _, body := range []string{
		`{"dismissedHints":["capture"]}`,
		`{"dismissedHints":["timeline","capture"]}`,
	} {
		if rec := doJSON(t, h, http.MethodPatch, "/api/settings", body); rec.Code != http.StatusOK {
			t.Fatalf("patch %s = %d (body %s)", body, rec.Code, rec.Body)
		}
	}
	got := settingsBody(t, doJSON(t, h, http.MethodGet, "/api/settings", "").Body.Bytes())
	hints, _ := got["dismissedHints"].([]any)
	if len(hints) != 2 {
		t.Fatalf("dismissedHints = %v, want the union of both patches with no duplicate", hints)
	}
	if hints[0] != "capture" || hints[1] != "timeline" {
		t.Errorf("dismissedHints = %v, want first-seen order [capture timeline]", hints)
	}
}

// A hint list a client can grow without limit is a way to inflate one user's
// settings document, so both the per-patch size and the stored total are
// bounded.
func TestSettingsDismissedHintsAreBounded(t *testing.T) {
	t.Parallel()
	h := newTestServer(t).Handler()

	oversized, err := json.Marshal(map[string]any{
		"dismissedHints": make([]string, maxDismissedHints+1),
	})
	if err != nil {
		t.Fatal(err)
	}
	if rec := doJSON(t, h, http.MethodPatch, "/api/settings", string(oversized)); rec.Code != http.StatusBadRequest {
		t.Errorf("oversized list = %d, want 400 (body %s)", rec.Code, rec.Body)
	}

	long := strings.Repeat("x", maxHintIDRunes+1)
	body, err := json.Marshal(map[string]any{"dismissedHints": []string{long}})
	if err != nil {
		t.Fatal(err)
	}
	if rec := doJSON(t, h, http.MethodPatch, "/api/settings", string(body)); rec.Code != http.StatusBadRequest {
		t.Errorf("over-long hint id = %d, want 400 (body %s)", rec.Code, rec.Body)
	}

	if rec := doJSON(t, h, http.MethodPatch, "/api/settings", `{"dismissedHints":[""]}`); rec.Code != http.StatusBadRequest {
		t.Errorf("empty hint id = %d, want 400 (body %s)", rec.Code, rec.Body)
	}
}

// Resetting is the one way progress moves backwards, and it has to survive
// being sent alongside the very flags it clears — the client sends its whole
// onboarding state, so field order must not decide the outcome.
func TestSettingsResetOnboarding(t *testing.T) {
	t.Parallel()
	h := newTestServer(t).Handler()
	seed := `{"welcomed":true,"tourDone":true,"dismissedHints":["capture"],"theme":"paper"}`
	if rec := doJSON(t, h, http.MethodPatch, "/api/settings", seed); rec.Code != http.StatusOK {
		t.Fatalf("seed = %d (body %s)", rec.Code, rec.Body)
	}
	reset := `{"resetOnboarding":true,"welcomed":true,"tourDone":true,"dismissedHints":["capture"]}`
	if rec := doJSON(t, h, http.MethodPatch, "/api/settings", reset); rec.Code != http.StatusOK {
		t.Fatalf("reset = %d (body %s)", rec.Code, rec.Body)
	}

	got := settingsBody(t, doJSON(t, h, http.MethodGet, "/api/settings", "").Body.Bytes())
	if welcomed, _ := got["welcomed"].(bool); welcomed {
		t.Error("welcomed survived a reset")
	}
	if tourDone, _ := got["tourDone"].(bool); tourDone {
		t.Error("tourDone survived a reset")
	}
	if hints, _ := got["dismissedHints"].([]any); len(hints) != 0 {
		t.Errorf("dismissedHints = %v, want empty after a reset", hints)
	}
	// Appearance is not onboarding state and must be untouched by the reset.
	if theme, _ := got["theme"].(string); theme != "paper" {
		t.Errorf("theme = %q, want it left alone by an onboarding reset", theme)
	}
}
