package api

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/bgrewell/donezo/internal/auth"
	"github.com/bgrewell/donezo/internal/store"
)

// This file implements per-user API token management for the MCP endpoint.
// Unlike invites, these are not admin-gated: any authenticated user mints,
// lists, and revokes their own tokens over the session-cookie API. The
// plaintext token is returned exactly once, at creation; everything else
// sees only the prefix.

// tokenScopes mirrors store's API token scopes for request validation.
var tokenScopes = []string{store.ScopeReadOnly, store.ScopeReadWrite}

// maxTokenNameLen bounds the owner-supplied label; a name only has to be
// recognizable in the listing.
const maxTokenNameLen = 100

// tokenView is the wire shape of one token in the owner's list. It carries
// the prefix and status, never the plaintext token or its hash, and
// matches the ApiToken interface the frontend consumes.
type tokenView struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	TokenPrefix string  `json:"tokenPrefix"`
	Scope       string  `json:"scope"`
	CreatedAt   string  `json:"createdAt"`
	LastUsedAt  *string `json:"lastUsedAt,omitempty"`
	RevokedAt   *string `json:"revokedAt,omitempty"`
}

// newTokenID returns a random token row id ("tok-" + 16 hex chars). Token
// ids are identifiers, not secrets — the secret is the token itself.
func newTokenID() (string, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("api: generate token id: %w", err)
	}
	return "tok-" + hex.EncodeToString(buf[:]), nil
}

// handleCreateToken mints an API token for the authenticated user: {name,
// scope} in, 201 {id, token, tokenPrefix, scope, name, createdAt} out. The
// plaintext token exists only in this response — the database holds its
// hash — so it can never be retrieved again; mint a new token instead.
func (s *Server) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	user, ok := userFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req struct {
		Name  string `json:"name"`
		Scope string `json:"scope"`
	}
	if !s.decodeBody(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if err := firstError(
		required("name", req.Name),
		oneOf("scope", req.Scope, tokenScopes),
	); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(req.Name) > maxTokenNameLen {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("name must be at most %d characters", maxTokenNameLen))
		return
	}
	token, hash, prefix, err := auth.NewAPIToken()
	if err != nil {
		s.logger.Printf("create token: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	id, err := newTokenID()
	if err != nil {
		s.logger.Printf("create token: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	stored, err := s.core.CreateAPIToken(r.Context(), store.APIToken{
		ID: id, UserID: user.ID, Name: req.Name, TokenHash: hash, TokenPrefix: prefix, Scope: req.Scope,
	})
	if err != nil {
		s.logger.Printf("create token: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{
		"id":          stored.ID,
		"token":       token,
		"tokenPrefix": stored.TokenPrefix,
		"scope":       stored.Scope,
		"name":        stored.Name,
		"createdAt":   stored.CreatedAt,
	})
}

// handleListTokens returns the authenticated user's tokens, newest first —
// prefix, scope, timestamps, and revoked status, never the token or hash.
func (s *Server) handleListTokens(w http.ResponseWriter, r *http.Request) {
	user, ok := userFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	tokens, err := s.core.ListAPITokens(r.Context(), user.ID)
	if err != nil {
		s.logger.Printf("list tokens: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	views := make([]tokenView, 0, len(tokens))
	for _, t := range tokens {
		views = append(views, tokenView{
			ID:          t.ID,
			Name:        t.Name,
			TokenPrefix: t.TokenPrefix,
			Scope:       t.Scope,
			CreatedAt:   t.CreatedAt,
			LastUsedAt:  t.LastUsedAt,
			RevokedAt:   t.RevokedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string][]tokenView{"tokens": views})
}

// handleDeleteToken revokes one of the authenticated user's tokens.
// Revocation is idempotent — revoking twice answers 204 both times — and a
// token belonging to another user (or an unknown id) answers 404, so token
// ids are not probeable across accounts.
func (s *Server) handleDeleteToken(w http.ResponseWriter, r *http.Request) {
	user, ok := userFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	err := s.core.RevokeAPIToken(r.Context(), user.ID, r.PathValue("id"))
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "token not found")
	case err != nil:
		s.logger.Printf("revoke token: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
