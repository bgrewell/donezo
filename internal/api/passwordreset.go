package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/bgrewell/donezo/internal/auth"
	"github.com/bgrewell/donezo/internal/notify"
	"github.com/bgrewell/donezo/internal/store"
)

// Password reset: a signed-out user who has forgotten their password asks for
// a link by email, and the link lets them set a new one. Two rules shape it.
// The request endpoint is not an account-existence oracle — it answers the
// same whether or not the address is on file — and the reset link is a
// bearer token (single-use, expiring, hashed at rest, carried in the URL
// fragment) that also invalidates every existing session when spent.

// resetTokenTTL is how long an emailed reset link stays valid.
const resetTokenTTL = time.Hour

// forgotPasswordMessage is the uniform answer to a reset request — returned
// whether or not the address matched an account, so the endpoint reveals
// nothing about which accounts exist.
const forgotPasswordMessage = "if an account uses that address, a reset link is on its way"

// handleForgotPassword accepts an email and, if it resolves to exactly one
// account, emails a reset link. It shares the credential rate limiter with
// login/setup/register, and always answers 200 with the uniform message once
// the address is syntactically valid.
func (s *Server) handleForgotPassword(w http.ResponseWriter, r *http.Request) {
	if !s.allowAttempt(w, r) {
		return
	}
	var req struct {
		Email string `json:"email"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	email := strings.TrimSpace(req.Email)
	if email == "" {
		writeError(w, http.StatusBadRequest, "an email address is required")
		return
	}
	// Syntax is validated (a malformed address can match nothing), but from
	// here every path answers identically — existence is never disclosed.
	if err := notify.ValidateAddress(notify.ChannelEmail, email); err != nil {
		writeError(w, http.StatusBadRequest, "enter a valid email address")
		return
	}
	// The lookup, token write and email send run off the request path so the
	// response time is constant: a matching address (which does DB writes and
	// an SMTP round trip) must not answer measurably slower than a
	// non-matching one, or the latency itself becomes the account-existence
	// oracle the uniform body was written to avoid.
	s.runAsync(func() { s.dispatchPasswordReset(email) })
	writeJSON(w, http.StatusOK, map[string]string{"message": forgotPasswordMessage})
}

// dispatchPasswordReset resolves the address to at most one account and emails
// it a reset link. It runs detached from the request (its own context), and
// every failure degrades to a log line: the response is already sent, and it
// was the same regardless, so nothing here may alter it.
func (s *Server) dispatchPasswordReset(email string) {
	if !s.notifiers.Configured(notify.ChannelEmail) {
		// Nothing to send on. The UI hides the option when email is
		// unconfigured; a direct caller still gets the uniform answer.
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), notify.DefaultTimeout)
	defer cancel()

	userID, ok, err := s.core.FindUserIDForReset(ctx, email)
	if err != nil {
		s.logger.Printf("forgot password: lookup: %v", err)
		return
	}
	if !ok {
		return
	}
	token, tokenHash, err := auth.NewToken()
	if err != nil {
		s.logger.Printf("forgot password: token: %v", err)
		return
	}
	issued, err := s.core.CreatePasswordReset(ctx, userID, tokenHash, resetTokenTTL)
	if err != nil {
		s.logger.Printf("forgot password: store token: %v", err)
		return
	}
	if !issued {
		// Throttled — a reset link was sent to this account very recently and
		// still stands. Not resending is the whole point of the cooldown.
		return
	}
	if err := s.notifiers.Send(ctx, notify.ChannelEmail, email, notify.Message{
		Subject: "Reset your donezo password",
		Body:    passwordResetEmailBody(token, s.publicURL),
	}); err != nil {
		s.logger.Printf("forgot password: send to %s: %v", notify.Redact(notify.ChannelEmail, email), err)
	}
}

// passwordResetEmailBody composes the plain-text reset email. The token rides
// in the URL fragment (#/reset/…), which browsers do not send to servers, so
// it stays out of access logs and referrers. When the instance does not know
// its own URL the token is given on its own.
func passwordResetEmailBody(token, publicURL string) string {
	base := strings.TrimSuffix(publicURL, "/")
	var b strings.Builder
	b.WriteString("Someone asked to reset the password on your donezo account.\n\n")
	if base != "" {
		fmt.Fprintf(&b, "Open this link to choose a new password:\n%s/#/reset/%s\n\n", base, token)
	} else {
		fmt.Fprintf(&b, "Use this reset token in donezo to choose a new password:\n%s\n\n", token)
	}
	b.WriteString("The link is good for one hour and can be used once.\n\n")
	b.WriteString("If you didn't ask for this, you can ignore this email — your password has not changed.")
	return b.String()
}

// handleResetPassword spends a reset token and sets a new password. It shares
// the credential rate limiter, answers a uniform 400 for any unusable token
// (unknown, spent, or expired — not a token-existence oracle), clears every
// existing session for the account, and logs the owner straight in.
func (s *Server) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	if !s.allowAttempt(w, r) {
		return
	}
	var req struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	token := strings.TrimSpace(req.Token)
	if token == "" {
		writeError(w, http.StatusBadRequest, "a reset token is required")
		return
	}
	if err := auth.ValidatePassword(req.Password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	userID, err := s.core.ConsumePasswordReset(r.Context(), auth.HashToken(token))
	if errors.Is(err, store.ErrResetInvalid) {
		writeError(w, http.StatusBadRequest, "this reset link is invalid or has expired; request a new one")
		return
	}
	if err != nil {
		s.logger.Printf("reset password: consume: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	hash, err := s.passwords.Hash(req.Password)
	if err != nil {
		s.logger.Printf("reset password: hash: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := s.core.SetUserPassword(r.Context(), userID, hash); err != nil {
		s.logger.Printf("reset password: set: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// The old password is now suspect — whoever phished or guessed it (and any
	// live session they hold) is cut off the moment the true owner resets.
	if _, err := s.core.DeleteUserSessions(r.Context(), userID); err != nil {
		// Not fatal: the password is already changed. Log and continue to
		// issue this browser a fresh session.
		s.logger.Printf("reset password: clear sessions: %v", err)
	}
	user, err := s.core.GetUserByID(r.Context(), userID)
	if err != nil {
		s.logger.Printf("reset password: load user: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// Spending the token proved control of the account's email, so sign them
	// in rather than bouncing them back to a login they just forgot.
	s.issueSession(w, r, user)
}
