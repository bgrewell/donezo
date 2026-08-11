package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"
)

// twilioAPIBase is Twilio's REST root. Overridable per sender so tests can
// point at an httptest server — there is no other way to exercise this
// without the network.
const twilioAPIBase = "https://api.twilio.com"

// smsMaxRunes bounds the text handed to Twilio.
//
// A single SMS segment is 160 GSM-7 characters (70 with any non-Latin
// character), and longer messages are split and billed per segment. donezo
// truncates instead of quietly sending five segments for a reminder whose
// details someone pasted a wall of text into: the point of the text message
// is to make you look, and the app has the rest.
const smsMaxRunes = 320

// TwilioConfig is everything needed to send a text message.
type TwilioConfig struct {
	// AccountSID identifies the Twilio account ("AC…"). Required.
	AccountSID string
	// AuthToken authenticates. Required, and environment-only by
	// convention — see config.EnvTwilioAuthToken.
	AuthToken string
	// From is the sending number in E.164, or a messaging service SID
	// ("MG…"), which Twilio accepts in the same field. Required.
	From string
	// BaseURL overrides the API root. Empty means Twilio's own.
	BaseURL string
}

// TwilioSender delivers SMS through Twilio's REST API.
//
// Twilio has an official Go SDK; this uses net/http directly because the
// whole integration is one form-encoded POST, and a dependency that exists
// to save nine lines is a dependency that has to be kept current forever.
type TwilioSender struct {
	cfg    TwilioConfig
	client *http.Client
}

// NewTwilioSender builds a sender from cfg, or returns an error describing
// what is missing.
func NewTwilioSender(cfg TwilioConfig) (*TwilioSender, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = twilioAPIBase
	}
	return &TwilioSender{cfg: cfg, client: &http.Client{}}, nil
}

// validate reports a configuration that cannot possibly deliver.
func (c TwilioConfig) validate() error {
	if c.AccountSID == "" {
		return errors.New("notify: twilio account SID is required")
	}
	if !strings.HasPrefix(c.AccountSID, "AC") {
		return fmt.Errorf("notify: twilio account SID should start with AC, got %q", firstRunes(c.AccountSID, 4)+"…")
	}
	if c.AuthToken == "" {
		return errors.New("notify: twilio auth token is required")
	}
	if c.From == "" {
		return errors.New("notify: twilio from number is required")
	}
	// A messaging service SID is equally valid here and is not a number, so
	// only the number form is checked for shape.
	if !strings.HasPrefix(c.From, "MG") {
		if err := validatePhone(c.From); err != nil {
			return fmt.Errorf("notify: twilio from number: %w", err)
		}
	}
	return nil
}

// Channel implements Sender.
func (s *TwilioSender) Channel() Channel { return ChannelSMS }

// Describe implements Sender. The account SID is an identifier rather than a
// credential, but it is still truncated: it is half of Twilio's basic-auth
// pair and there is no reason for a status page to publish it whole.
func (s *TwilioSender) Describe() string {
	return fmt.Sprintf("twilio %s… from %s", firstRunes(s.cfg.AccountSID, 6), s.cfg.From)
}

// Send delivers one message.
//
// Subject and Body are joined because SMS has no subject line: the subject
// carries the reminder itself, so dropping it would send the trimmings and
// leave out the point.
func (s *TwilioSender) Send(ctx context.Context, to string, msg Message) error {
	if err := validatePhone(to); err != nil {
		return err
	}
	text := smsText(msg)
	if text == "" {
		return errors.New("notify: message is empty")
	}

	form := url.Values{}
	form.Set("To", to)
	form.Set("From", s.cfg.From)
	form.Set("Body", text)

	endpoint := fmt.Sprintf("%s/2010-04-01/Accounts/%s/Messages.json",
		strings.TrimSuffix(s.cfg.BaseURL, "/"), url.PathEscape(s.cfg.AccountSID))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("notify: build twilio request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(s.cfg.AccountSID, s.cfg.AuthToken)

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("notify: send sms: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	// Bounded because an error page from something that is not Twilio (a
	// proxy, a captive portal) could be any size at all.
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("notify: send sms: twilio returned %s: %s", resp.Status, twilioError(body))
	}
	return nil
}

// smsOptOut is appended to every text message.
//
// Carriers expect a recipient to be able to stop messages from the message
// itself, and a campaign's registered sample messages have to match what is
// actually sent — so this rides on all of them rather than only the first.
// It is short on purpose: it is paid for in every segment.
const smsOptOut = "Reply STOP to cancel, HELP for help."

// smsText renders a message as one piece of text, truncated to something
// that will not be split into a dozen billed segments.
//
// The opt-out line is appended after truncation, never inside it: a
// recipient who cannot stop the messages is the one failure this must not
// have, so it is the reminder text that loses characters, not the way out.
func smsText(msg Message) string {
	text := strings.TrimSpace(msg.Subject)
	if body := strings.TrimSpace(msg.Body); body != "" {
		if text != "" {
			text += "\n"
		}
		text += body
	}
	if text == "" {
		return ""
	}
	budget := smsMaxRunes - utf8.RuneCountInString(smsOptOut) - 1 // the joining newline
	if utf8.RuneCountInString(text) > budget {
		text = firstRunes(text, budget-1) + "…"
	}
	return text + "\n" + smsOptOut
}

// twilioError pulls the human half out of Twilio's JSON error, falling back
// to the raw body when it is not the shape we expect.
func twilioError(body []byte) string {
	var payload struct {
		Message  string `json:"message"`
		Code     int    `json:"code"`
		MoreInfo string `json:"more_info"`
	}
	if err := json.Unmarshal(body, &payload); err == nil && payload.Message != "" {
		if payload.Code != 0 {
			return fmt.Sprintf("%s (code %d)", payload.Message, payload.Code)
		}
		return payload.Message
	}
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return "no detail"
	}
	return firstRunes(trimmed, 200)
}

// firstRunes returns at most n runes of s, cutting on a rune boundary so a
// truncated multi-byte character cannot produce invalid UTF-8.
func firstRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	count := 0
	for i := range s {
		if count == n {
			return s[:i]
		}
		count++
	}
	return s
}
