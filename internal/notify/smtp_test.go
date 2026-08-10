package notify

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// newTestSMTP builds a sender with the transport captured rather than dialled.
func newTestSMTP(t *testing.T, cfg SMTPConfig) (*SMTPSender, *[]byte, *string) {
	t.Helper()
	s, err := NewSMTPSender(cfg)
	if err != nil {
		t.Fatalf("NewSMTPSender: %v", err)
	}
	var sent []byte
	var to string
	s.send = func(_ context.Context, _ SMTPConfig, rcpt string, msg []byte) error {
		to, sent = rcpt, msg
		return nil
	}
	s.now = func() time.Time { return time.Date(2026, 8, 15, 14, 0, 0, 0, time.UTC) }
	return s, &sent, &to
}

func validSMTPConfig() SMTPConfig {
	return SMTPConfig{Host: "smtp.example.com", Port: 587, From: "donezo@example.com"}
}

func TestNewSMTPSenderValidation(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*SMTPConfig)
		wantErr string
	}{
		{name: "valid", mutate: func(*SMTPConfig) {}},
		{name: "no host", mutate: func(c *SMTPConfig) { c.Host = "" }, wantErr: "host is required"},
		{name: "port zero", mutate: func(c *SMTPConfig) { c.Port = 0 }, wantErr: "out of range"},
		{name: "port too high", mutate: func(c *SMTPConfig) { c.Port = 70000 }, wantErr: "out of range"},
		{name: "no from", mutate: func(c *SMTPConfig) { c.From = "" }, wantErr: "from address"},
		{name: "bad from", mutate: func(c *SMTPConfig) { c.From = "not-an-address" }, wantErr: "from address"},
		{name: "username without password", mutate: func(c *SMTPConfig) { c.Username = "ben" }, wantErr: "without a password"},
		{name: "unknown security", mutate: func(c *SMTPConfig) { c.Security = "ssl" }, wantErr: "unknown smtp security"},
		{name: "implicit tls ok", mutate: func(c *SMTPConfig) { c.Security = SMTPImplicitTLS }},
		{name: "none ok", mutate: func(c *SMTPConfig) { c.Security = SMTPNone }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validSMTPConfig()
			tt.mutate(&cfg)
			_, err := NewSMTPSender(cfg)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("NewSMTPSender = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("NewSMTPSender = %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestSMTPSenderDefaultsToStartTLS(t *testing.T) {
	s, err := NewSMTPSender(validSMTPConfig())
	if err != nil {
		t.Fatalf("NewSMTPSender: %v", err)
	}
	if s.cfg.Security != SMTPStartTLS {
		t.Fatalf("Security = %q, want %q — an unset mode must not mean cleartext", s.cfg.Security, SMTPStartTLS)
	}
}

func TestSMTPSendComposesMessage(t *testing.T) {
	cfg := validSMTPConfig()
	cfg.FromName = "donezo"
	s, sent, to := newTestSMTP(t, cfg)

	err := s.Send(context.Background(), "ben@example.com", Message{
		Subject: "Reminder: clean up the deck",
		Body:    "Put the bins curbside for pickup.",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if *to != "ben@example.com" {
		t.Fatalf("envelope recipient = %q", *to)
	}
	msg := string(*sent)
	for _, want := range []string{
		"From: donezo <donezo@example.com>\r\n",
		"To: ben@example.com\r\n",
		"MIME-Version: 1.0\r\n",
		"Content-Type: text/plain; charset=utf-8\r\n",
		// Marks the mail as machine-generated so a vacation responder does
		// not answer it.
		"Auto-Submitted: auto-generated\r\n",
		"Put the bins curbside for pickup.\r\n",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message missing %q:\n%s", want, msg)
		}
	}
	if !strings.Contains(msg, "Subject: ") || !strings.Contains(msg, "clean") {
		t.Fatalf("message missing its subject:\n%s", msg)
	}
	headers, body, ok := strings.Cut(msg, "\r\n\r\n")
	if !ok {
		t.Fatalf("message has no header/body separator:\n%s", msg)
	}
	if strings.Contains(headers, "Put the bins") {
		t.Fatalf("body leaked into the headers:\n%s", headers)
	}
	if !strings.HasSuffix(body, "\r\n") {
		t.Fatalf("body does not end CRLF: %q", body)
	}
}

// The reminder text is written by the user and goes straight into a header.
// A bare newline in it would end the Subject line and let everything after
// be read as more headers — the way a reminder becomes an extra Bcc.
func TestSMTPSendRefusesHeaderInjection(t *testing.T) {
	s, sent, _ := newTestSMTP(t, validSMTPConfig())

	err := s.Send(context.Background(), "ben@example.com", Message{
		Subject: "Deck cleanup\r\nBcc: victim@example.com",
		Body:    "ok",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	msg := string(*sent)
	headers, _, _ := strings.Cut(msg, "\r\n\r\n")

	// The danger is a NEW header line, not the words appearing in the
	// subject: folded onto the Subject line the text is inert, and refusing
	// to deliver a reminder because someone typed a newline would be worse
	// than sending it with an ugly subject. So the property is that no
	// header line begins with the injected field.
	var subject string
	for _, line := range strings.Split(headers, "\r\n") {
		if strings.HasPrefix(strings.ToLower(line), "bcc:") {
			t.Fatalf("injection produced a real header line %q:\n%s", line, headers)
		}
		if strings.HasPrefix(line, "Subject:") {
			subject = line
		}
	}
	if subject == "" {
		t.Fatalf("no Subject header:\n%s", headers)
	}
	if !strings.Contains(subject, "Deck cleanup") {
		t.Fatalf("Subject = %q, want the real subject kept", subject)
	}
	if strings.ContainsAny(subject, "\r\n") {
		t.Fatalf("Subject = %q, want exactly one line", subject)
	}
}

func TestSMTPSendRefusesInjectedRecipient(t *testing.T) {
	s, _, _ := newTestSMTP(t, validSMTPConfig())
	err := s.Send(context.Background(), "ben@example.com\r\nBcc: victim@example.com", Message{Subject: "x"})
	if err == nil {
		t.Fatal("Send accepted a recipient with an embedded header")
	}
}

func TestSMTPSendRequiresSubject(t *testing.T) {
	s, _, _ := newTestSMTP(t, validSMTPConfig())
	if err := s.Send(context.Background(), "ben@example.com", Message{Body: "no subject"}); err == nil {
		t.Fatal("Send accepted a message with no subject")
	}
}

func TestSMTPSendWrapsTransportError(t *testing.T) {
	s, err := NewSMTPSender(validSMTPConfig())
	if err != nil {
		t.Fatalf("NewSMTPSender: %v", err)
	}
	boom := errors.New("connection refused")
	s.send = func(context.Context, SMTPConfig, string, []byte) error { return boom }
	err = s.Send(context.Background(), "ben@example.com", Message{Subject: "x"})
	if !errors.Is(err, boom) {
		t.Fatalf("Send = %v, want the transport error wrapped", err)
	}
	if !strings.Contains(err.Error(), "smtp.example.com") {
		t.Fatalf("Send error %q does not name the relay", err)
	}
}

func TestSMTPDescribeHidesCredentials(t *testing.T) {
	cfg := validSMTPConfig()
	cfg.Username = "ben@example.com"
	cfg.Password = "hunter2-super-secret"
	s, err := NewSMTPSender(cfg)
	if err != nil {
		t.Fatalf("NewSMTPSender: %v", err)
	}
	got := s.Describe()
	if strings.Contains(got, cfg.Password) {
		t.Fatalf("Describe() = %q, which contains the password", got)
	}
	if strings.Contains(got, cfg.Username) && cfg.Username != cfg.From {
		t.Fatalf("Describe() = %q, which contains the relay username", got)
	}
	if !strings.Contains(got, "smtp.example.com") || !strings.Contains(got, "authenticated") {
		t.Fatalf("Describe() = %q, want the relay and whether it authenticates", got)
	}
}

func TestNormalizeBody(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty stays empty", in: "", want: ""},
		{name: "adds trailing newline", in: "one", want: "one\r\n"},
		{name: "lf becomes crlf", in: "one\ntwo", want: "one\r\ntwo\r\n"},
		{name: "crlf preserved", in: "one\r\ntwo\r\n", want: "one\r\ntwo\r\n"},
		{name: "bare cr becomes crlf", in: "one\rtwo", want: "one\r\ntwo\r\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeBody(tt.in); got != tt.want {
				t.Fatalf("normalizeBody(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
