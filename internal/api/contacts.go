package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/bgrewell/donezo/internal/auth"
	"github.com/bgrewell/donezo/internal/notify"
	"github.com/bgrewell/donezo/internal/store"
)

// Notification destinations (#52): where a person's reminders are delivered.
//
// Two rules shape every handler here. A destination is never deliverable
// until its owner has proved they receive at it, because otherwise a signed
// in user could aim this instance at a stranger's phone. And every read and
// write is scoped to the authenticated user, so a guessed id reaches a 404
// rather than somebody else's address.

// maxContactLabelRunes bounds the optional label. It exists to name a
// destination ("work", "phone"), not to hold notes.
const maxContactLabelRunes = 40

// newContactID returns a random contact row id ("ctc-" + 16 hex chars).
func newContactID() (string, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("api: generate contact id: %w", err)
	}
	return "ctc-" + hex.EncodeToString(buf[:]), nil
}

// contactCreate is the POST /api/notify/contacts body.
type contactCreate struct {
	Channel string `json:"channel"`
	Address string `json:"address"`
	Label   string `json:"label"`
}

// contactVerify is the POST /api/notify/contacts/{id}/verify body.
type contactVerify struct {
	Code string `json:"code"`
}

// channelStatus reports what one channel can do on this instance.
type channelStatus struct {
	// Channel is "email" or "sms".
	Channel string `json:"channel"`
	// Configured reports whether the operator has set this channel up. The
	// settings UI needs it to explain why adding a number will not deliver
	// anything, rather than letting somebody add one and wait.
	Configured bool `json:"configured"`
	// Provider describes the backing provider, for the admin view. Empty
	// when the channel is not configured, and never a credential.
	Provider string `json:"provider,omitempty"`
}

// handleNotifyStatus reports which channels this instance can deliver on.
//
// Readable by any signed-in user, not just an admin: it is what the settings
// page uses to say "SMS is not set up on this instance" instead of silently
// accepting a number that will never be texted. It carries no secrets — see
// Sender.Describe.
func (s *Server) handleNotifyStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := userFrom(r.Context()); !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	statuses := make([]channelStatus, 0, len(notify.Channels))
	for _, c := range notify.Channels {
		st := channelStatus{Channel: string(c)}
		if sender, ok := s.notifiers.Sender(c); ok {
			st.Configured = true
			st.Provider = sender.Describe()
		}
		statuses = append(statuses, st)
	}
	writeJSON(w, http.StatusOK, map[string]any{"channels": statuses})
}

// handleListContacts returns the authenticated user's destinations.
func (s *Server) handleListContacts(w http.ResponseWriter, r *http.Request) {
	user, ok := userFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	contacts, err := s.core.ListUserContacts(r.Context(), user.ID)
	if err != nil {
		s.logger.Printf("list contacts: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string][]store.UserContact{"contacts": contacts})
}

// handleCreateContact adds a destination and sends it a verification code.
//
// The code goes out as part of creating it because the two are one action to
// the person doing it: adding a number you cannot then verify is a dead end,
// and a separate "send code" button is a step that exists only because of how
// the server is built.
func (s *Server) handleCreateContact(w http.ResponseWriter, r *http.Request) {
	user, ok := userFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req contactCreate
	if !s.decodeBody(w, r, &req) {
		return
	}
	channel := notify.Channel(strings.TrimSpace(req.Channel))
	if !notify.ValidChannel(channel) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("channel must be one of %s", channelList()))
		return
	}
	address := strings.TrimSpace(req.Address)
	if err := notify.ValidateAddress(channel, address); err != nil {
		// The notify package's messages are written for a person typing into
		// the form, so they are surfaced as-is.
		writeError(w, http.StatusBadRequest, strings.TrimPrefix(err.Error(), "notify: "))
		return
	}
	label := strings.TrimSpace(req.Label)
	if len([]rune(label)) > maxContactLabelRunes {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("label must be %d characters or fewer", maxContactLabelRunes))
		return
	}
	// Refusing here rather than after storing keeps the settings page honest:
	// a destination on a channel nobody can send on would sit unverifiable
	// forever, looking like a bug in verification.
	if !s.notifiers.Configured(channel) {
		writeError(w, http.StatusConflict,
			fmt.Sprintf("this instance cannot send %s; ask the operator to configure it", channel))
		return
	}

	id, err := newContactID()
	if err != nil {
		s.logger.Printf("create contact: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	contact, err := s.core.CreateUserContact(r.Context(), store.UserContact{
		ID: id, UserID: user.ID, Channel: string(channel), Address: address, Label: label,
	})
	if err != nil {
		if errors.Is(err, store.ErrDuplicateID) {
			writeError(w, http.StatusConflict, "that destination is already on your list")
			return
		}
		s.logger.Printf("create contact: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := s.sendContactCode(r, user.ID, contact); err != nil {
		// The row exists and can be verified with a resend, so this is
		// reported without undoing it — deleting it would lose the address
		// they just typed for a fault that is probably the relay's.
		s.logger.Printf("create contact: sending code to %s %s: %v",
			contact.Channel, notify.Redact(channel, contact.Address), err)
		writeJSON(w, http.StatusCreated, map[string]any{
			"contact": contact,
			"warning": "added, but the code could not be sent — try Resend in a moment",
		})
		return
	}
	contact.PendingCode = true
	writeJSON(w, http.StatusCreated, map[string]any{"contact": contact})
}

// handleSendContactCode sends a fresh verification code.
func (s *Server) handleSendContactCode(w http.ResponseWriter, r *http.Request) {
	user, ok := userFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	contact, err := s.core.GetUserContact(r.Context(), user.ID, r.PathValue("id"))
	if err != nil {
		s.writeStoreError(w, "contact", err)
		return
	}
	if contact.Verified() {
		writeError(w, http.StatusConflict, "that destination is already verified")
		return
	}
	if err := s.sendContactCode(r, user.ID, contact); err != nil {
		if errors.Is(err, store.ErrResendTooSoon) {
			writeError(w, http.StatusTooManyRequests, store.ErrResendTooSoon.Error())
			return
		}
		s.logger.Printf("send contact code: %v", err)
		writeError(w, http.StatusBadGateway, "the code could not be sent; try again in a moment")
		return
	}
	contact.PendingCode = true
	writeJSON(w, http.StatusOK, map[string]any{"contact": contact})
}

// sendContactCode mints a code, records its hash, and delivers it.
//
// Order matters: the hash is stored first, so a code that arrives always has
// something to check it against. A send that fails leaves a live challenge,
// which is harmless — it expires — and is what lets Resend work without
// re-adding the destination.
func (s *Server) sendContactCode(r *http.Request, userID int64, contact store.UserContact) error {
	code, hash, err := auth.NewVerificationCode()
	if err != nil {
		return err
	}
	if _, err := s.core.StartContactChallenge(r.Context(), userID, contact.ID, hash); err != nil {
		return err
	}
	channel := notify.Channel(contact.Channel)
	ctx, cancel := context.WithTimeout(r.Context(), notify.DefaultTimeout)
	defer cancel()
	return s.notifiers.Send(ctx, channel, contact.Address, notify.Message{
		Subject: fmt.Sprintf("donezo verification code: %s", code),
		Body: strings.Join([]string{
			fmt.Sprintf("Enter %s in donezo to confirm this is where your reminders should go.", code),
			"",
			"It expires in 15 minutes. If you did not ask for this, ignore it — nothing is sent here until it is confirmed.",
		}, "\n"),
	})
}

// handleVerifyContact checks a code and marks the destination usable.
//
// Every failure answers 400 with the store's own wording rather than
// distinguishing "wrong" from "expired" by status code: the next step is the
// same either way, and the person needs the sentence, not the number.
func (s *Server) handleVerifyContact(w http.ResponseWriter, r *http.Request) {
	user, ok := userFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req contactVerify
	if !s.decodeBody(w, r, &req) {
		return
	}
	code := strings.TrimSpace(req.Code)
	if code == "" {
		writeError(w, http.StatusBadRequest, "code is required")
		return
	}
	contact, err := s.core.VerifyUserContact(r.Context(), user.ID, r.PathValue("id"), auth.HashToken(code))
	switch {
	case errors.Is(err, store.ErrCodeIncorrect):
		writeError(w, http.StatusBadRequest, "that code is not right")
		return
	case errors.Is(err, store.ErrCodeExpired):
		writeError(w, http.StatusBadRequest, "that code has expired; send a new one")
		return
	case errors.Is(err, store.ErrTooManyAttempts):
		writeError(w, http.StatusTooManyRequests, "too many attempts; send a new code")
		return
	case err != nil:
		s.writeStoreError(w, "contact", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"contact": contact})
}

// handleDeleteContact removes a destination.
func (s *Server) handleDeleteContact(w http.ResponseWriter, r *http.Request) {
	user, ok := userFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if err := s.core.DeleteUserContact(r.Context(), user.ID, r.PathValue("id")); err != nil {
		s.writeStoreError(w, "contact", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// channelList renders the supported channels for an error message.
func channelList() string {
	names := make([]string, 0, len(notify.Channels))
	for _, c := range notify.Channels {
		names = append(names, string(c))
	}
	return strings.Join(names, ", ")
}
