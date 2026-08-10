package notify

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"strings"
	"time"
)

// SMTPSecurity selects how the connection to the relay is protected.
type SMTPSecurity string

const (
	// SMTPStartTLS connects in the clear and upgrades with STARTTLS. The
	// usual choice, and what submission port 587 expects.
	SMTPStartTLS SMTPSecurity = "starttls"
	// SMTPImplicitTLS wraps the connection in TLS from the first byte, which
	// is what port 465 expects.
	SMTPImplicitTLS SMTPSecurity = "tls"
	// SMTPNone sends in the clear with no upgrade. For a relay on localhost
	// or a mail catcher in development — never across a network.
	SMTPNone SMTPSecurity = "none"
)

// SMTPSecurities lists the accepted security modes, for config validation
// and its error message.
var SMTPSecurities = []SMTPSecurity{SMTPStartTLS, SMTPImplicitTLS, SMTPNone}

// SMTPConfig is everything needed to hand a message to a relay.
type SMTPConfig struct {
	// Host is the relay hostname. Required.
	Host string
	// Port is the relay port. Required.
	Port int
	// Username and Password authenticate to the relay. Both empty means an
	// unauthenticated relay, which is normal for a local one.
	Username string
	Password string
	// From is the envelope sender and the From header. Required.
	From string
	// FromName is the optional display name shown beside From.
	FromName string
	// Security selects TLS handling. Empty means SMTPStartTLS.
	Security SMTPSecurity
}

// smtpTransport is the seam the tests replace. It matches smtp.SendMail's
// shape so the production implementation is a thin adapter rather than a
// reimplementation.
type smtpTransport func(ctx context.Context, cfg SMTPConfig, to string, msg []byte) error

// SMTPSender delivers email through an SMTP relay.
type SMTPSender struct {
	cfg SMTPConfig
	// now supplies the Date header; injectable so a test can assert on it.
	now func() time.Time
	// send performs the delivery. Replaced in tests.
	send smtpTransport
}

// NewSMTPSender builds a sender from cfg, or returns an error describing
// what is missing. Callers that treat email as optional should check
// cfg.Host first rather than calling this and discarding the error, so that
// a half-filled configuration is still reported instead of silently
// disabling the channel.
func NewSMTPSender(cfg SMTPConfig) (*SMTPSender, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if cfg.Security == "" {
		cfg.Security = SMTPStartTLS
	}
	return &SMTPSender{cfg: cfg, now: time.Now, send: sendSMTP}, nil
}

// validate reports a configuration that cannot possibly deliver.
func (c SMTPConfig) validate() error {
	if c.Host == "" {
		return errors.New("notify: smtp host is required")
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("notify: smtp port %d out of range 1-65535", c.Port)
	}
	if err := validateEmail(c.From); err != nil {
		return fmt.Errorf("notify: smtp from address: %w", err)
	}
	if c.Username != "" && c.Password == "" {
		return errors.New("notify: smtp username given without a password")
	}
	switch c.Security {
	case "", SMTPStartTLS, SMTPImplicitTLS, SMTPNone:
	default:
		return fmt.Errorf("notify: unknown smtp security %q (want one of starttls, tls, none)", c.Security)
	}
	return nil
}

// Channel implements Sender.
func (s *SMTPSender) Channel() Channel { return ChannelEmail }

// Describe implements Sender. It names the relay and how the connection is
// protected — never the password, and never the username, which on most
// hosted relays is an address worth not printing.
func (s *SMTPSender) Describe() string {
	auth := "no auth"
	if s.cfg.Username != "" {
		auth = "authenticated"
	}
	return fmt.Sprintf("smtp %s:%d (%s, %s) from %s",
		s.cfg.Host, s.cfg.Port, s.cfg.Security, auth, s.cfg.From)
}

// Send delivers one message.
func (s *SMTPSender) Send(ctx context.Context, to string, msg Message) error {
	if err := validateEmail(to); err != nil {
		return err
	}
	body, err := s.compose(to, msg)
	if err != nil {
		return err
	}
	if err := s.send(ctx, s.cfg, to, body); err != nil {
		return fmt.Errorf("notify: send email via %s:%d: %w", s.cfg.Host, s.cfg.Port, err)
	}
	return nil
}

// compose builds the RFC 5322 message.
//
// Every header value here is either configuration or user-written text, and
// a bare CR or LF in one of those would end the header and let the rest be
// read as more headers — the classic way a "reminder" becomes an extra Bcc.
//
// Two things stop that, in this order. mime.QEncoding.Encode is what makes
// it impossible: a control character forces an encoded-word, so a CRLF comes
// out as inert "=0D=0A" text rather than a line ending. sanitizeHeaderText
// runs first anyway — partly so the common case stays a readable ASCII
// subject instead of an encoded-word nobody can skim in a mail client, and
// partly so this does not silently become injectable the day someone
// replaces the encoder. The addresses are separately validated to contain no
// whitespace at all, so there is nothing to inject with there.
func (s *SMTPSender) compose(to string, msg Message) ([]byte, error) {
	subject := strings.TrimSpace(msg.Subject)
	if subject == "" {
		return nil, errors.New("notify: message subject is required")
	}
	if err := validateEmail(to); err != nil {
		return nil, err
	}
	from := s.cfg.From
	if name := sanitizeHeaderText(s.cfg.FromName); name != "" {
		from = fmt.Sprintf("%s <%s>", mime.QEncoding.Encode("utf-8", name), s.cfg.From)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", sanitizeHeaderText(subject)))
	fmt.Fprintf(&b, "Date: %s\r\n", s.now().Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("Auto-Submitted: auto-generated\r\n")
	b.WriteString("\r\n")
	// The body is dot-stuffed by net/smtp's data writer, so a line of "."
	// in a reminder cannot end the message early.
	b.WriteString(normalizeBody(msg.Body))
	return []byte(b.String()), nil
}

// sanitizeHeaderText removes anything that could end a header line early.
// Tabs and spaces survive; CR, LF and NUL do not.
func sanitizeHeaderText(v string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '\r', '\n', 0:
			return ' '
		}
		return r
	}, strings.TrimSpace(v))
}

// normalizeBody gives every line the CRLF ending SMTP expects.
func normalizeBody(body string) string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\r", "\n")
	if body != "" && !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	return strings.ReplaceAll(body, "\n", "\r\n")
}

// sendSMTP is the real transport: dial, optionally secure, optionally
// authenticate, hand over the message.
//
// It is written out rather than delegating to smtp.SendMail because
// SendMail cannot do implicit TLS (port 465) and cannot be given a deadline.
func sendSMTP(ctx context.Context, cfg SMTPConfig, to string, msg []byte) error {
	addr := net.JoinHostPort(cfg.Host, fmt.Sprint(cfg.Port))
	dialer := &net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		// The SMTP conversation is several round trips and net/smtp offers
		// no context, so the deadline is applied to the socket instead.
		_ = conn.SetDeadline(deadline)
	}
	if cfg.Security == SMTPImplicitTLS {
		conn = tls.Client(conn, &tls.Config{ServerName: cfg.Host})
	}

	client, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("greeting: %w", err)
	}
	defer func() { _ = client.Close() }()

	if cfg.Security == SMTPStartTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("relay does not offer STARTTLS; use --smtp-security tls for port 465, or none for a local relay")
		}
		if err := client.StartTLS(&tls.Config{ServerName: cfg.Host}); err != nil {
			return fmt.Errorf("starttls: %w", err)
		}
	}
	if cfg.Username != "" {
		// PLAIN is refused by net/smtp on an unencrypted connection unless
		// the server is localhost, which is the check we want anyway.
		if err := client.Auth(smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)); err != nil {
			return fmt.Errorf("auth: %w", err)
		}
	}
	if err := client.Mail(cfg.From); err != nil {
		return fmt.Errorf("mail from: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("rcpt to: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("data: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		_ = w.Close()
		return fmt.Errorf("write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("close data: %w", err)
	}
	return client.Quit()
}
