// Package notify delivers donezo's reminders somewhere other than the app.
//
// A reminder that only exists inside donezo is a reminder you have to
// already be looking at donezo to receive, which is the opposite of what a
// reminder is for. This package is the way out: one small interface per
// channel, configured instance-wide (see internal/config), with the actual
// scheduling left to whoever owns the clock.
//
// Configuration is instance-wide for the same reason internal/llm is —
// credentials stay in the environment and out of the database. What is
// per-user is the *destination*: which address or number a given person's
// reminders go to, which lives in core.db and must be verified before
// anything is sent to it (see store.UserContact).
//
// Every feature here is optional. An instance with nothing configured is a
// fully supported instance: reminders keep working in the app exactly as
// they did before, and the dispatcher simply has nowhere to send them.
package notify

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode"
)

// ErrNotConfigured is returned when a channel has no provider configured.
// Like llm.ErrNotConfigured it means "this is switched off", not "this
// broke" — callers should skip the channel rather than treat it as failure.
var ErrNotConfigured = errors.New("notify: channel not configured")

// DefaultTimeout bounds one delivery attempt.
//
// Deliberately short. The dispatcher works through every due reminder in one
// pass, so a provider that has stopped answering must not be able to hold
// the whole pass open — the next tick comes round in a minute and an
// unsent reminder is retried then.
const DefaultTimeout = 20 * time.Second

// Channel is one way of reaching somebody.
type Channel string

const (
	// ChannelEmail is email, delivered over SMTP.
	ChannelEmail Channel = "email"
	// ChannelSMS is a text message, delivered through Twilio.
	ChannelSMS Channel = "sms"
)

// Channels lists every channel donezo knows how to deliver on, in the order
// a settings UI should offer them.
var Channels = []Channel{ChannelEmail, ChannelSMS}

// ValidChannel reports whether c names a channel donezo supports.
func ValidChannel(c Channel) bool {
	for _, known := range Channels {
		if c == known {
			return true
		}
	}
	return false
}

// Message is one notification, in the two parts every channel can express.
//
// Subject is email's alone: SMS has no such field and senders that lack one
// fold it into the text rather than dropping it, because the subject carries
// the reminder itself and the body carries the trimmings.
type Message struct {
	// Subject is the one-line summary. Required.
	Subject string
	// Body is the longer form. May be empty.
	Body string
}

// Sender delivers a message on one channel. Implementations are safe for
// concurrent use and must respect the context's deadline.
type Sender interface {
	// Channel names the channel this sender delivers on.
	Channel() Channel
	// Send delivers msg to one already-validated, already-verified address.
	Send(ctx context.Context, to string, msg Message) error
	// Describe names the backing provider and its notable settings, for the
	// admin status view and for logs. It must never include a credential.
	Describe() string
}

// Registry is the set of configured senders, one per channel at most.
//
// The zero Registry is valid and configured for nothing, which is the
// resting state of an instance that has not opted in.
type Registry struct {
	senders map[Channel]Sender
}

// NewRegistry builds a registry from the senders given. A nil sender is
// skipped, so a caller can pass the result of a constructor that returns nil
// when its channel is not configured, without checking first.
func NewRegistry(senders ...Sender) *Registry {
	r := &Registry{senders: make(map[Channel]Sender, len(senders))}
	for _, s := range senders {
		if s == nil {
			continue
		}
		r.senders[s.Channel()] = s
	}
	return r
}

// Sender returns the sender for a channel, or false when it is unconfigured.
func (r *Registry) Sender(c Channel) (Sender, bool) {
	if r == nil {
		return nil, false
	}
	s, ok := r.senders[c]
	return s, ok
}

// Configured reports whether anything can be delivered on this channel.
func (r *Registry) Configured(c Channel) bool {
	_, ok := r.Sender(c)
	return ok
}

// Any reports whether any channel at all is configured. When this is false
// the dispatcher has nothing to do and says so once, at startup, rather than
// waking up every minute to discover it again.
func (r *Registry) Any() bool {
	return r != nil && len(r.senders) > 0
}

// Send delivers on one channel, or returns ErrNotConfigured.
func (r *Registry) Send(ctx context.Context, c Channel, to string, msg Message) error {
	s, ok := r.Sender(c)
	if !ok {
		return fmt.Errorf("%w: %s", ErrNotConfigured, c)
	}
	return s.Send(ctx, to, msg)
}

// ValidateAddress checks that addr is a plausible destination for a channel.
//
// This is the gate in front of a stored contact, so it is deliberately
// stricter than what a provider would accept: a destination is typed once
// and then used unattended forever, and the failure mode of a typo is a
// reminder that silently goes nowhere for months.
func ValidateAddress(c Channel, addr string) error {
	switch c {
	case ChannelEmail:
		return validateEmail(addr)
	case ChannelSMS:
		return validatePhone(addr)
	default:
		return fmt.Errorf("notify: unknown channel %q", c)
	}
}

// validateEmail accepts one plain address. A display name ("Ben <b@x>") is
// refused rather than parsed out: the stored value is used as an envelope
// recipient, and accepting a form that has to be unwrapped later invites the
// unwrapping to be forgotten somewhere.
func validateEmail(addr string) error {
	if addr == "" {
		return errors.New("notify: email address is required")
	}
	parsed, err := mail.ParseAddress(addr)
	if err != nil || parsed.Address != addr || parsed.Name != "" {
		return fmt.Errorf("notify: %q is not a plain email address like you@example.com", addr)
	}
	if strings.Count(addr, "@") != 1 {
		return fmt.Errorf("notify: %q is not a plain email address like you@example.com", addr)
	}
	return nil
}

// validatePhone accepts E.164 only: a leading + and 8 to 15 digits.
//
// Local formats are refused on purpose. "5551234" is unroutable without a
// country the server has no way to know, and Twilio's own guidance is E.164
// for exactly this reason — better a clear refusal while somebody is typing
// than a message that fails, or reaches a stranger, weeks later.
func validatePhone(addr string) error {
	if addr == "" {
		return errors.New("notify: phone number is required")
	}
	if !strings.HasPrefix(addr, "+") {
		return fmt.Errorf("notify: %q must be in international format, starting with + and the country code (e.g. +15551234567)", addr)
	}
	digits := addr[1:]
	for _, r := range digits {
		if !unicode.IsDigit(r) {
			return fmt.Errorf("notify: %q must be + followed by digits only — no spaces, dashes or parentheses (e.g. +15551234567)", addr)
		}
	}
	if len(digits) < 8 || len(digits) > 15 {
		return fmt.Errorf("notify: %q must have between 8 and 15 digits after the + (E.164)", addr)
	}
	return nil
}

// Redact reduces an address to something safe to log or show in a list:
// enough to recognise which of your own destinations it is, not enough to
// be worth harvesting from a log file.
func Redact(c Channel, addr string) string {
	switch c {
	case ChannelEmail:
		at := strings.LastIndex(addr, "@")
		if at <= 0 {
			return "…"
		}
		local := addr[:at]
		if len(local) <= 2 {
			return "…" + addr[at:]
		}
		return local[:1] + "…" + local[len(local)-1:] + addr[at:]
	case ChannelSMS:
		if len(addr) <= 4 {
			return "…"
		}
		return "…" + addr[len(addr)-4:]
	default:
		return "…"
	}
}
