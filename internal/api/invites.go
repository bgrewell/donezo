package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bgrewell/donezo/internal/auth"
	"github.com/bgrewell/donezo/internal/store"
)

// This file implements roles: the admin-only invite lifecycle (mint,
// list, revoke) and the public invite-code registration endpoint.

// inviteFailedMessage is deliberately identical for unknown, used,
// expired, and revoked codes, so registration responses never reveal
// the state of any code.
const inviteFailedMessage = "invalid or expired invite code"

// Invite lifetime bounds, in days: absent defaults to a week, and
// requests beyond the cap are clamped to it.
const (
	defaultInviteDays = 7
	maxInviteDays     = 90
)

// Every member's first space, created with their account.
const (
	registerSpaceName  = "main"
	registerSpaceColor = "blue"
)

// requireAdmin resolves the authenticated user and requires the admin
// role. On failure it writes the response — 401 without an identity,
// 403 for members — and reports false.
func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) (store.User, bool) {
	user, ok := userFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return store.User{}, false
	}
	if user.Role != store.RoleAdmin {
		writeError(w, http.StatusForbidden, "admin required")
		return store.User{}, false
	}
	return user, true
}

// inviteView is the wire shape of one invite in the admin list. The
// plaintext code appears only in handleCreateInvite's 201; the list
// carries the prefix and derived status, never the code or its hash.
type inviteView struct {
	ID         string  `json:"id"`
	CodePrefix string  `json:"codePrefix"`
	Status     string  `json:"status"`
	CreatedBy  string  `json:"createdBy"`
	CreatedAt  string  `json:"createdAt"`
	ExpiresAt  string  `json:"expiresAt"`
	UsedBy     *string `json:"usedBy,omitempty"`
	UsedAt     *string `json:"usedAt,omitempty"`
	RevokedAt  *string `json:"revokedAt,omitempty"`
}

// inviteStatus derives an invite's lifecycle state at now (RFC 3339
// UTC, which orders lexicographically). A used invite stays "used" even
// if later revoked or past its expiry — the claim happened — and
// "revoked" likewise wins over "expired".
func inviteStatus(l store.InviteListing, now string) string {
	switch {
	case l.UsedAt != nil:
		return "used"
	case l.RevokedAt != nil:
		return "revoked"
	case l.ExpiresAt <= now:
		return "expired"
	default:
		return "active"
	}
}

// newInviteID returns a random invite row id ("inv-" + 16 hex chars).
// Invite ids are identifiers, not secrets — the secret is the code.
func newInviteID() (string, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("api: generate invite id: %w", err)
	}
	return "inv-" + hex.EncodeToString(buf[:]), nil
}

// handleCreateInvite mints an invite code: {expiresInDays?} in (default
// 7, capped at 90), 201 {invite: {id, code, codePrefix, expiresAt}}
// out. The plaintext code exists only in this response — the database
// holds its hash — so it can never be retrieved again; mint a new
// invite instead.
func (s *Server) handleCreateInvite(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	var req struct {
		ExpiresInDays *int `json:"expiresInDays"`
	}
	// Strict decoding, but an absent body means "all defaults".
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxAuthBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, decodeMessage(err))
		return
	}
	days := defaultInviteDays
	if req.ExpiresInDays != nil {
		days = *req.ExpiresInDays
		if days < 1 {
			writeError(w, http.StatusBadRequest, "expiresInDays must be at least 1")
			return
		}
		if days > maxInviteDays {
			days = maxInviteDays
		}
	}
	code, codeHash, err := auth.NewInviteCode()
	if err != nil {
		s.logger.Printf("create invite: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	id, err := newInviteID()
	if err != nil {
		s.logger.Printf("create invite: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	inv, err := s.core.CreateInvite(r.Context(), store.Invite{
		ID:         id,
		CodeHash:   codeHash,
		CodePrefix: code[:auth.InviteCodePrefixLen],
		CreatedBy:  user.ID,
	}, time.Duration(days)*24*time.Hour)
	if err != nil {
		s.logger.Printf("create invite: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]map[string]string{"invite": {
		"id":         inv.ID,
		"code":       code,
		"codePrefix": inv.CodePrefix,
		"expiresAt":  inv.ExpiresAt,
	}})
}

// handleListInvites answers the admin invite table: every invite with
// its derived status and the creator's / claimant's usernames.
func (s *Server) handleListInvites(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	listings, err := s.core.ListInvites(r.Context())
	if err != nil {
		s.logger.Printf("list invites: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	now := s.clock().UTC().Format(time.RFC3339)
	views := make([]inviteView, 0, len(listings))
	for _, l := range listings {
		views = append(views, inviteView{
			ID:         l.ID,
			CodePrefix: l.CodePrefix,
			Status:     inviteStatus(l, now),
			CreatedBy:  l.CreatorUsername,
			CreatedAt:  l.CreatedAt,
			ExpiresAt:  l.ExpiresAt,
			UsedBy:     l.UsedByUsername,
			UsedAt:     l.UsedAt,
			RevokedAt:  l.RevokedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string][]inviteView{"invites": views})
}

// handleRevokeInvite marks an invite unusable. Revocation is idempotent
// — revoking twice answers 204 both times, keeping the original stamp —
// and an unknown id answers 404.
func (s *Server) handleRevokeInvite(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	err := s.core.RevokeInvite(r.Context(), r.PathValue("id"))
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "invite not found")
	case err != nil:
		s.logger.Printf("revoke invite: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleAuthRegister redeems an invite code: {code, username,
// displayName?, password} in; a member account, their first space
// ("main"), and a session out. It shares the credential rate limiter
// with login and setup. Every unusable code — unknown, used, expired,
// revoked — answers the same 403, so registration is not an
// invite-state oracle; a taken username is its own 409, disclosed only
// after the code has been validated.
//
// That 409 is a deliberate, bounded disclosure: someone holding one
// valid unclaimed code can probe usernames without burning it (the 409
// rolls the claim back so the invitee can pick another name). Accepted
// because the precondition is real trust — the admin handed that person
// a code, i.e. approved them for an account, which anonymous clients
// (who get login's uniform 401) never have — and because a uniform
// error here would misdirect legitimate invitees into blaming a code
// their admin just minted. The probe rate is bounded: allowAttempt runs
// before anything else, so every guess, including 409s, spends the
// shared 10-per-5-minutes-per-IP credential budget
// (TestAuthRegisterUsernameProbingRateLimited pins this).
func (s *Server) handleAuthRegister(w http.ResponseWriter, r *http.Request) {
	if !s.allowAttempt(w, r) {
		return
	}
	var req struct {
		Code        string `json:"code"`
		Username    string `json:"username"`
		DisplayName string `json:"displayName"`
		Password    string `json:"password"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	code := strings.TrimSpace(req.Code)
	if code == "" {
		writeError(w, http.StatusBadRequest, "an invite code is required")
		return
	}
	if err := validateUsername(req.Username); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := auth.ValidatePassword(req.Password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.DisplayName == "" {
		req.DisplayName = req.Username
	}
	hash, err := s.passwords.Hash(req.Password)
	if err != nil {
		s.logger.Printf("register: hash password: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	codeHash := auth.HashInviteCode(code)
	// The space-id retry mirrors handleCreateSpace. Every attempt is a
	// fresh all-or-nothing transaction, so a retried collision leaves no
	// residue and cannot burn the invite.
	for attempt := 0; attempt < 3; attempt++ {
		spaceID, err := newSpaceID(registerSpaceName)
		if err != nil {
			s.logger.Printf("register: %v", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		user, sp, err := s.core.RegisterInvitedUser(r.Context(), codeHash,
			store.User{Username: req.Username, DisplayName: req.DisplayName, PasswordHash: hash},
			store.Space{ID: spaceID, Name: registerSpaceName, Color: registerSpaceColor})
		switch {
		case errors.Is(err, store.ErrInviteInvalid):
			writeError(w, http.StatusForbidden, inviteFailedMessage)
			return
		case errors.Is(err, store.ErrUsernameTaken):
			writeError(w, http.StatusConflict, "username is already taken")
			return
		case errors.Is(err, store.ErrDuplicateID):
			continue // space id collision: try a fresh suffix
		case err != nil:
			s.logger.Printf("register: %v", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		// Create the space's content database now, like POST /api/spaces
		// does, so the member's first space is usable the moment they see
		// it. On failure, compensate: an account must not exist half-made,
		// and a server-side fault must not burn the invite. The unwind is
		// one transaction — releasing the claim and deleting the account
		// cannot come apart, so one code can never mint two accounts. If
		// even the unwind fails, the claim stays burned (the safe
		// direction) and the fault is logged for the operator.
		if err := s.spaces.EnsureSpace(r.Context(), sp.ID); err != nil {
			s.logger.Printf("register %q: ensure space database: %v", user.Username, err)
			if err := s.core.UnregisterInvitedUser(r.Context(), codeHash, user.ID, sp.ID); err != nil {
				s.logger.Printf("register compensation: %v", err)
			}
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		s.issueSession(w, r, user)
		return
	}
	s.logger.Printf("register: could not allocate a space id after 3 attempts")
	writeError(w, http.StatusInternalServerError, "internal error")
}
