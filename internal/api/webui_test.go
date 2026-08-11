package api

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newUIFixture writes a tiny fake Vite dist into a temp dir and returns
// it as an fs.FS: the same shape a real build has (index.html at the
// root, hashed files under assets/), injected through the WithWebUI
// seam so these tests never need the embedui build tag.
func newUIFixture(t *testing.T) fs.FS {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"index.html":              "<!doctype html><title>donezo</title><body><div id=\"root\">donezo fixture index</div></body></html>",
		"favicon.svg":             "<svg xmlns=\"http://www.w3.org/2000/svg\"></svg>",
		"assets/app-abc123.js":    "console.log(\"donezo fixture js\");",
		"assets/style-def456.css": ":root{--donezo:1}",
	}
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", name, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return os.DirFS(dir)
}

func TestWebUIServing(t *testing.T) {
	t.Parallel()
	const (
		cacheImmutable = "public, max-age=31536000, immutable"
		cacheNoCache   = "no-cache"
	)
	tests := []struct {
		name       string
		ui         bool // wire the fixture bundle via WithWebUI
		method     string
		path       string
		wantStatus int
		wantType   string // substring of Content-Type; "" = don't check
		wantCache  string // exact Cache-Control; "" = header must be absent
		wantBody   string // substring of the body; "" = don't check
	}{
		{
			// /api/* wins over the "/" catch-all purely by mux pattern
			// specificity — this is the registration-order guarantee.
			name: "api takes precedence over static", ui: true,
			method: http.MethodGet, path: "/api/healthz",
			wantStatus: http.StatusOK, wantType: "application/json", wantBody: `{"status":"ok"}`,
		},
		{
			name: "authenticated api route still served", ui: true,
			method: http.MethodGet, path: "/api/spaces",
			wantStatus: http.StatusOK, wantType: "application/json", wantBody: `"sandbox"`,
		},
		{
			name: "unknown api path stays JSON 404, not SPA", ui: true,
			method: http.MethodGet, path: "/api/nope",
			wantStatus: http.StatusNotFound, wantType: "application/json", wantBody: "not found",
		},
		{
			name: "root serves index.html uncached", ui: true,
			method: http.MethodGet, path: "/",
			wantStatus: http.StatusOK, wantType: "text/html",
			wantCache: cacheNoCache, wantBody: "donezo fixture index",
		},
		{
			name: "index.html by name", ui: true,
			method: http.MethodGet, path: "/index.html",
			wantStatus: http.StatusOK, wantType: "text/html",
			wantCache: cacheNoCache, wantBody: "donezo fixture index",
		},
		{
			name: "hashed js asset is immutable with script content type", ui: true,
			method: http.MethodGet, path: "/assets/app-abc123.js",
			wantStatus: http.StatusOK, wantType: "javascript",
			wantCache: cacheImmutable, wantBody: "donezo fixture js",
		},
		{
			name: "hashed css asset is immutable with css content type", ui: true,
			method: http.MethodGet, path: "/assets/style-def456.css",
			wantStatus: http.StatusOK, wantType: "text/css",
			wantCache: cacheImmutable, wantBody: "--donezo",
		},
		{
			name: "root-level non-hashed file is uncached", ui: true,
			method: http.MethodGet, path: "/favicon.svg",
			wantStatus: http.StatusOK, wantCache: cacheNoCache,
		},
		{
			name: "unknown path falls back to index.html with 200", ui: true,
			method: http.MethodGet, path: "/spaces/sandbox/anything",
			wantStatus: http.StatusOK, wantType: "text/html",
			wantCache: cacheNoCache, wantBody: "donezo fixture index",
		},
		{
			name: "missing asset also falls back to index.html", ui: true,
			method: http.MethodGet, path: "/assets/gone-000000.js",
			wantStatus: http.StatusOK, wantType: "text/html",
			wantCache: cacheNoCache, wantBody: "donezo fixture index",
		},
		{
			name: "non-GET on static paths is rejected", ui: true,
			method: http.MethodPost, path: "/spaces",
			wantStatus: http.StatusMethodNotAllowed, wantType: "application/json",
		},
		{
			name: "API-only mode 404s the root", ui: false,
			method: http.MethodGet, path: "/",
			wantStatus: http.StatusNotFound, wantType: "application/json", wantBody: "not found",
		},
		{
			name: "API-only mode 404s arbitrary paths", ui: false,
			method: http.MethodGet, path: "/spaces/sandbox/anything",
			wantStatus: http.StatusNotFound, wantType: "application/json", wantBody: "not found",
		},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var extra []ServerOption
			if tt.ui {
				extra = append(extra, WithWebUI(newUIFixture(t)))
			}
			srv := newTestServer(t, extra...)
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.wantType != "" {
				if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, tt.wantType) {
					t.Errorf("Content-Type = %q, want substring %q", ct, tt.wantType)
				}
			}
			if got := rec.Header().Get("Cache-Control"); got != tt.wantCache {
				t.Errorf("Cache-Control = %q, want %q", got, tt.wantCache)
			}
			if tt.wantBody != "" && !strings.Contains(rec.Body.String(), tt.wantBody) {
				t.Errorf("body = %q, want substring %q", rec.Body.String(), tt.wantBody)
			}
		})
	}
}

// The crawlable-root fix for A2P vetting: a non-JS verifier fetching "/"
// must find plain-HTML links to the policy pages, because the app renders
// them with React and a crawler that does not run JS would otherwise see
// none. Injected only when the instance publishes the pages.
func TestCrawlableRootLinks(t *testing.T) {
	t.Run("with an operator, root carries plain links", func(t *testing.T) {
		srv := newTestServer(t, WithWebUI(newUIFixture(t)),
			WithOperator("Grewell Tech", "ben@grewelltech.com"))
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		body := rec.Body.String()
		for _, want := range []string{
			`href="/privacy"`, `href="/terms"`, "Grewell Tech", "donezo Reminders",
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("root HTML missing %q — a non-JS crawler cannot find the policy:\n%s", want, body)
			}
		}
		// The SPA mount point must survive, injected before it.
		if !strings.Contains(body, `id="root"`) {
			t.Fatal("the SPA root div was lost")
		}
	})

	t.Run("without an operator, root is untouched", func(t *testing.T) {
		srv := newTestServer(t, WithWebUI(newUIFixture(t)))
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if strings.Contains(rec.Body.String(), `href="/privacy"`) {
			t.Fatal("policy links injected on an instance that does not publish them")
		}
	})
}
