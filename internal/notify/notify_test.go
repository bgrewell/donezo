package notify

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// stubSender records what it was asked to deliver.
type stubSender struct {
	channel Channel
	to      string
	msg     Message
	err     error
}

func (s *stubSender) Channel() Channel { return s.channel }
func (s *stubSender) Describe() string { return string(s.channel) + " stub" }
func (s *stubSender) Send(_ context.Context, to string, msg Message) error {
	s.to, s.msg = to, msg
	return s.err
}

func TestValidateAddress(t *testing.T) {
	tests := []struct {
		name    string
		channel Channel
		addr    string
		wantErr string
	}{
		{name: "plain email", channel: ChannelEmail, addr: "ben@example.com"},
		{name: "subaddressed email", channel: ChannelEmail, addr: "ben+donezo@example.com"},
		{name: "empty email", channel: ChannelEmail, addr: "", wantErr: "required"},
		{name: "display name refused", channel: ChannelEmail, addr: "Ben <ben@example.com>", wantErr: "plain email address"},
		{name: "no at sign", channel: ChannelEmail, addr: "ben.example.com", wantErr: "plain email address"},
		{name: "two addresses refused", channel: ChannelEmail, addr: "a@x.com, b@y.com", wantErr: "plain email address"},
		// A newline in a stored address would be a header injection at send
		// time; it must never reach the sender in the first place.
		{name: "newline refused", channel: ChannelEmail, addr: "ben@example.com\nBcc: victim@example.com", wantErr: "plain email address"},
		{name: "carriage return refused", channel: ChannelEmail, addr: "ben@example.com\r\nBcc: x@y.com", wantErr: "plain email address"},

		{name: "e164 number", channel: ChannelSMS, addr: "+15551234567"},
		{name: "long international", channel: ChannelSMS, addr: "+442071234567"},
		{name: "empty phone", channel: ChannelSMS, addr: "", wantErr: "required"},
		{name: "no plus", channel: ChannelSMS, addr: "5551234567", wantErr: "international format"},
		{name: "formatted rejected", channel: ChannelSMS, addr: "+1 (555) 123-4567", wantErr: "digits only"},
		{name: "too short", channel: ChannelSMS, addr: "+1234567", wantErr: "8 and 15"},
		{name: "too long", channel: ChannelSMS, addr: "+1234567890123456", wantErr: "8 and 15"},
		{name: "newline refused", channel: ChannelSMS, addr: "+1555123456\n", wantErr: "digits only"},

		{name: "unknown channel", channel: Channel("carrier-pigeon"), addr: "x", wantErr: "unknown channel"},
	}
	for _, tt := range tests {
		t.Run(string(tt.channel)+"/"+tt.name, func(t *testing.T) {
			err := ValidateAddress(tt.channel, tt.addr)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateAddress(%q) = %v, want nil", tt.addr, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateAddress(%q) = nil, want error containing %q", tt.addr, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidateAddress(%q) = %v, want error containing %q", tt.addr, err, tt.wantErr)
			}
		})
	}
}

func TestRedact(t *testing.T) {
	tests := []struct {
		name    string
		channel Channel
		addr    string
		want    string
	}{
		{name: "email keeps ends", channel: ChannelEmail, addr: "benjamin@example.com", want: "b…n@example.com"},
		{name: "short local part", channel: ChannelEmail, addr: "bg@example.com", want: "…@example.com"},
		{name: "not an address", channel: ChannelEmail, addr: "nope", want: "…"},
		{name: "phone keeps last four", channel: ChannelSMS, addr: "+15551234567", want: "…4567"},
		{name: "short phone", channel: ChannelSMS, addr: "+123", want: "…"},
		{name: "unknown channel", channel: Channel("x"), addr: "whatever", want: "…"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Redact(tt.channel, tt.addr); got != tt.want {
				t.Fatalf("Redact(%q) = %q, want %q", tt.addr, got, tt.want)
			}
		})
	}
}

func TestRedactHidesEnoughOfTheAddress(t *testing.T) {
	// The point of redaction is that the log cannot be harvested. A rule
	// that kept the local part intact would pass a spot check on shape
	// while still publishing the address.
	const addr = "benjamin@example.com"
	got := Redact(ChannelEmail, addr)
	if strings.Contains(got, "benjamin") {
		t.Fatalf("Redact(%q) = %q, which still contains the local part", addr, got)
	}
	if !strings.HasSuffix(got, "@example.com") {
		t.Fatalf("Redact(%q) = %q, want the domain kept so it is recognisable", addr, got)
	}
}

func TestRegistry(t *testing.T) {
	email := &stubSender{channel: ChannelEmail}
	reg := NewRegistry(email, nil)

	if !reg.Any() {
		t.Fatal("Any() = false with a sender registered")
	}
	if !reg.Configured(ChannelEmail) {
		t.Fatal("Configured(email) = false")
	}
	if reg.Configured(ChannelSMS) {
		t.Fatal("Configured(sms) = true with no SMS sender")
	}

	if err := reg.Send(context.Background(), ChannelEmail, "ben@example.com", Message{Subject: "hi"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if email.to != "ben@example.com" || email.msg.Subject != "hi" {
		t.Fatalf("sender got (%q, %+v)", email.to, email.msg)
	}

	err := reg.Send(context.Background(), ChannelSMS, "+15551234567", Message{Subject: "hi"})
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("Send on unconfigured channel = %v, want ErrNotConfigured", err)
	}
}

func TestRegistryZeroValueIsConfiguredForNothing(t *testing.T) {
	// An instance that has opted into nothing is a supported instance, so
	// the empty registry must answer rather than panic.
	var reg *Registry
	if reg.Any() {
		t.Fatal("nil registry reports senders")
	}
	if reg.Configured(ChannelEmail) {
		t.Fatal("nil registry reports email configured")
	}
	if err := reg.Send(context.Background(), ChannelEmail, "ben@example.com", Message{Subject: "x"}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("nil registry Send = %v, want ErrNotConfigured", err)
	}
}

func TestRegistrySendPropagatesSenderError(t *testing.T) {
	boom := errors.New("relay refused")
	reg := NewRegistry(&stubSender{channel: ChannelEmail, err: boom})
	if err := reg.Send(context.Background(), ChannelEmail, "ben@example.com", Message{Subject: "x"}); !errors.Is(err, boom) {
		t.Fatalf("Send = %v, want the sender's error", err)
	}
}

func TestValidChannel(t *testing.T) {
	for _, c := range []Channel{ChannelEmail, ChannelSMS} {
		if !ValidChannel(c) {
			t.Fatalf("ValidChannel(%q) = false", c)
		}
	}
	for _, c := range []Channel{"", "slack", "EMAIL"} {
		if ValidChannel(c) {
			t.Fatalf("ValidChannel(%q) = true", c)
		}
	}
}
