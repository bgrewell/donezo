package api

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"testing"

	"github.com/bgrewell/donezo/internal/auth"
	"github.com/bgrewell/donezo/internal/store"
)

// createdToken is the 201 body of POST /api/tokens.
type createdToken struct {
	ID          string `json:"id"`
	Token       string `json:"token"`
	TokenPrefix string `json:"tokenPrefix"`
	Scope       string `json:"scope"`
	Name        string `json:"name"`
	CreatedAt   string `json:"createdAt"`
}

func TestCreateTokenReturnsPlaintextOnce(t *testing.T) {
	t.Parallel()
	h := newTestServer(t).Handler()

	rec := doJSON(t, h, http.MethodPost, "/api/tokens", `{"name":"laptop","scope":"read_write"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d (body %s)", rec.Code, rec.Body.String())
	}
	var created createdToken
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !strings.HasPrefix(created.Token, auth.APITokenScheme) {
		t.Errorf("token %q missing scheme prefix", created.Token)
	}
	if created.TokenPrefix != created.Token[:auth.APITokenPrefixLen] {
		t.Errorf("prefix %q does not match token", created.TokenPrefix)
	}
	if created.Scope != "read_write" || created.Name != "laptop" || created.ID == "" {
		t.Errorf("unexpected created token: %+v", created)
	}

	// The listing shows the token but never the plaintext or hash.
	listRec := doJSON(t, h, http.MethodGet, "/api/tokens", "")
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d", listRec.Code)
	}
	body := listRec.Body.String()
	if strings.Contains(body, created.Token) {
		t.Error("listing leaked the plaintext token")
	}
	if strings.Contains(body, auth.HashToken(created.Token)) {
		t.Error("listing leaked the token hash")
	}
	var list struct {
		Tokens []map[string]any `json:"tokens"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &list); err != nil {
		t.Fatalf("parse list: %v", err)
	}
	if len(list.Tokens) != 1 {
		t.Fatalf("tokens = %d, want 1", len(list.Tokens))
	}
	tok := list.Tokens[0]
	for _, field := range []string{"id", "name", "tokenPrefix", "scope", "createdAt"} {
		if _, ok := tok[field]; !ok {
			t.Errorf("listing missing field %q", field)
		}
	}
	if _, leaked := tok["token"]; leaked {
		t.Error("listing row carries a token field")
	}
	if _, leaked := tok["tokenHash"]; leaked {
		t.Error("listing row carries a tokenHash field")
	}
}

func TestCreateTokenValidation(t *testing.T) {
	t.Parallel()
	h := newTestServer(t).Handler()
	tests := []struct {
		name string
		body string
	}{
		{name: "missing name", body: `{"scope":"read_only"}`},
		{name: "blank name", body: `{"name":"  ","scope":"read_only"}`},
		{name: "missing scope", body: `{"name":"x"}`},
		{name: "bad scope", body: `{"name":"x","scope":"admin"}`},
		{name: "unknown field", body: `{"name":"x","scope":"read_only","extra":1}`},
	}
	for _, tt := range tests {
		tt := tt // capture (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := doJSON(t, h, http.MethodPost, "/api/tokens", tt.body)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestDeleteTokenOwnOnly(t *testing.T) {
	t.Parallel()
	h := newTestServer(t).Handler()
	rec := doJSON(t, h, http.MethodPost, "/api/tokens", `{"name":"laptop","scope":"read_only"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d", rec.Code)
	}
	var created createdToken
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("parse: %v", err)
	}

	// Deleting an unknown id answers 404.
	if got := doJSON(t, h, http.MethodDelete, "/api/tokens/tok-missing", "").Code; got != http.StatusNotFound {
		t.Errorf("delete unknown = %d, want 404", got)
	}

	// Deleting the owner's own token answers 204, and is idempotent.
	if got := doJSON(t, h, http.MethodDelete, "/api/tokens/"+created.ID, "").Code; got != http.StatusNoContent {
		t.Errorf("delete own = %d, want 204", got)
	}
	if got := doJSON(t, h, http.MethodDelete, "/api/tokens/"+created.ID, "").Code; got != http.StatusNoContent {
		t.Errorf("idempotent delete = %d, want 204", got)
	}

	// After revocation the token is still listed, now marked revoked.
	listRec := doJSON(t, h, http.MethodGet, "/api/tokens", "")
	var list struct {
		Tokens []map[string]any `json:"tokens"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &list); err != nil {
		t.Fatalf("parse list: %v", err)
	}
	if len(list.Tokens) != 1 || list.Tokens[0]["revokedAt"] == nil {
		t.Errorf("revoked token listing = %+v", list.Tokens)
	}
}

func TestTokensRequireAuth(t *testing.T) {
	t.Parallel()
	// A server with the default (session) authenticator and no cookie: the
	// token endpoints are under /api/ and require authentication.
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
		method, path string
	}{
		{http.MethodGet, "/api/tokens"},
		{http.MethodPost, "/api/tokens"},
		{http.MethodDelete, "/api/tokens/tok-x"},
	} {
		rec := doJSON(t, h, tc.method, tc.path, "")
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s = %d, want 401", tc.method, tc.path, rec.Code)
		}
	}
}
