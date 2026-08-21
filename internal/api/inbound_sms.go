package api

import (
	"encoding/xml"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/bgrewell/donezo/internal/notify"
	"github.com/bgrewell/donezo/internal/store"
)

// Inbound SMS limits: a person texts a handful of things a minute at most, and
// each inbound message will (from phase 2) spend a model call, so a stuck or
// hostile sender is capped per number. Only signature-valid Twilio requests are
// counted, so this bounds a real sender, not an attacker probing the endpoint.
const (
	defaultInboundSMSLimit  = 12
	defaultInboundSMSWindow = time.Minute
)

// maxInboundSMSBytes caps the one unauthenticated POST body. A real SMS is at
// most ~1600 characters; anything near this is a mistake or an attempt to make
// us buffer megabytes on a public endpoint.
const maxInboundSMSBytes = 16 << 10

// WithTwilioAuthToken supplies the Twilio account auth token used to validate
// the X-Twilio-Signature on inbound SMS. Without it (or without a public URL)
// the inbound webhook is not registered — inbound texting stays off.
func WithTwilioAuthToken(token string) ServerOption {
	return func(s *Server) { s.twilioAuthToken = token }
}

// handleInboundSMS is Twilio's incoming-message webhook. It is a public path
// (no session cookie) that authenticates the request by its X-Twilio-Signature,
// attributes it to the user who owns the verified sending number, and — for now
// (phase 1) — captures the message into that user's inbox, replying over TwiML.
//
// Everything an unauthenticated or unattributable request could learn is
// withheld: a bad signature is a flat 403, and an unknown or shared number gets
// an empty (but 200) TwiML so Twilio does not retry and nothing is disclosed.
func (s *Server) handleInboundSMS(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxInboundSMSBytes)
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}

	// Validate the signature FIRST, before any lookup or write, so an
	// unauthenticated request never reaches the database. Twilio signs the
	// exact URL it was configured to call — the public URL plus this path.
	signedURL := strings.TrimRight(s.publicURL, "/") + r.URL.Path
	if r.URL.RawQuery != "" {
		signedURL += "?" + r.URL.RawQuery
	}
	sig := r.Header.Get("X-Twilio-Signature")
	if !notify.ValidateTwilioSignature(s.twilioAuthToken, signedURL, r.PostForm, sig) {
		writeError(w, http.StatusForbidden, "invalid signature")
		return
	}

	from := strings.TrimSpace(r.PostFormValue("From"))
	body := strings.TrimSpace(r.PostFormValue("Body"))

	// Rate-limit per sending number (post-signature, so only real senders count).
	if from != "" {
		if ok, _ := s.smsLimiter.Allow("sms:" + from); !ok {
			writeTwiML(w, "") // drop quietly; do not tell an over-eager sender why
			return
		}
	}

	user, err := s.core.UserForVerifiedContact(r.Context(), string(notify.ChannelSMS), from)
	if err != nil {
		// Unknown or shared number, or a lookup fault: say nothing useful. An
		// empty 200 keeps Twilio from retrying and reveals no account state.
		if !errors.Is(err, store.ErrNotFound) && !errors.Is(err, store.ErrAmbiguousContact) {
			s.logger.Printf("inbound sms: contact lookup: %v", err)
		}
		writeTwiML(w, "")
		return
	}

	// Every path that does not actually capture something replies with the same
	// silent empty-200 as an unknown number, so a signed probe — an empty body,
	// or a verified-but-spaceless account — cannot be told apart from an
	// unrecognized one. Only a real capture, a side-effecting action rather than
	// a free oracle, gets a confirmation.
	if body == "" {
		writeTwiML(w, "")
		return
	}

	// Try to decode the message into a reminder or task. On any doubt it
	// returns false and we fall back to capturing the raw text to the inbox.
	if reply, done := s.decodeInboundSMS(r.Context(), user, body); done {
		writeTwiML(w, reply)
		return
	}

	space, err := s.core.FirstLiveSpace(r.Context(), user.ID)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			s.logger.Printf("inbound sms: default space for user %d: %v", user.ID, err)
		}
		writeTwiML(w, "")
		return
	}

	id, err := store.NewID("inb")
	if err != nil {
		s.logger.Printf("inbound sms: id: %v", err)
		writeTwiML(w, "")
		return
	}
	capturedAt := s.clock().In(s.location).Format("2006-01-02T15:04:05")
	if _, err := s.spaces.CreateInboxItem(r.Context(), space.ID, store.InboxItem{
		ID:         id,
		Raw:        body,
		CapturedAt: capturedAt,
		Status:     "pending",
	}); err != nil {
		s.logger.Printf("inbound sms: capture to inbox: %v", err)
		writeTwiML(w, "")
		return
	}

	writeTwiML(w, "Saved to your donezo inbox ("+space.Name+"). Open the app to file it.")
}

// twiml is the minimal TwiML the webhook replies with. An empty Message field
// marshals to a bare <Response></Response>, i.e. "reply with nothing".
type twiml struct {
	XMLName xml.Name `xml:"Response"`
	Message string   `xml:"Message,omitempty"`
}

// writeTwiML writes a TwiML SMS reply. A blank message sends no reply.
func writeTwiML(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(xml.Header))
	// Marshal handles XML-escaping of the message body.
	body, err := xml.Marshal(twiml{Message: message})
	if err != nil {
		body = []byte("<Response></Response>")
	}
	_, _ = w.Write(body)
}
