package api

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bgrewell/donezo/internal/store"
)

func TestInstanceReportsVersion(t *testing.T) {
	t.Parallel()
	h := newTestServer(t, WithServerVersion("v9.9.9")).Handler()
	rec := doJSON(t, h, http.MethodGet, "/api/instance", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (body %s)", rec.Code, rec.Body)
	}
	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got["version"] != "v9.9.9" {
		t.Errorf("version = %q, want v9.9.9", got["version"])
	}
}

// The operator's switch: once an instance is public, the exact build is of
// more use to somebody probing it than to the people using it. Hiding must
// omit the field entirely rather than blanking it, so a client cannot tell a
// hidden version from an old server that never had the field.
func TestInstanceHidesVersionWhenAsked(t *testing.T) {
	t.Parallel()
	h := newTestServer(t, WithServerVersion("v9.9.9"), WithHideVersion(true)).Handler()
	rec := doJSON(t, h, http.MethodGet, "/api/instance", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (body %s)", rec.Code, rec.Body)
	}
	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, present := got["version"]; present {
		t.Errorf("version present despite --hide-version: %v", got)
	}
	if len(rec.Body.String()) > 40 {
		t.Errorf("hidden response should be an empty object, got %s", rec.Body)
	}
}

// It sits behind auth: an unauthenticated caller learning the build is the
// thing --hide-version exists to prevent, so it must not be free either way.
func TestInstanceRequiresAuth(t *testing.T) {
	t.Parallel()
	// newTestServer installs an auth bypass, so this builds a plain server
	// the way the settings suite does — otherwise every request is signed in
	// and the check passes for the wrong reason.
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
	h := NewServer(core, spaces,
		WithLogger(log.New(io.Discard, "", 0)),
		WithServerVersion("v9.9.9"),
	).Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/instance", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous = %d, want 401 (body %s)", rec.Code, rec.Body)
	}
}
