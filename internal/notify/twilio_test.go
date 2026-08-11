package notify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"
)

func validTwilioConfig() TwilioConfig {
	return TwilioConfig{
		AccountSID: "AC00000000000000000000000000000001",
		AuthToken:  "token-value",
		From:       "+15550001111",
	}
}

func TestNewTwilioSenderValidation(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*TwilioConfig)
		wantErr string
	}{
		{name: "valid", mutate: func(*TwilioConfig) {}},
		{name: "messaging service sid as from", mutate: func(c *TwilioConfig) { c.From = "MG0000000000000000000000000000001" }},
		{name: "no sid", mutate: func(c *TwilioConfig) { c.AccountSID = "" }, wantErr: "account SID is required"},
		{name: "sid wrong prefix", mutate: func(c *TwilioConfig) { c.AccountSID = "SK123" }, wantErr: "should start with AC"},
		{name: "no token", mutate: func(c *TwilioConfig) { c.AuthToken = "" }, wantErr: "auth token is required"},
		{name: "no from", mutate: func(c *TwilioConfig) { c.From = "" }, wantErr: "from number is required"},
		{name: "from not e164", mutate: func(c *TwilioConfig) { c.From = "5550001111" }, wantErr: "international format"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validTwilioConfig()
			tt.mutate(&cfg)
			_, err := NewTwilioSender(cfg)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("NewTwilioSender = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("NewTwilioSender = %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

// newTestTwilio points a sender at a fake Twilio and returns the last form
// it received.
func newTestTwilio(t *testing.T, handler http.HandlerFunc) (*TwilioSender, *url.Values, *string) {
	t.Helper()
	var got url.Values
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		got, path = r.PostForm, r.URL.Path
		handler(w, r)
	}))
	t.Cleanup(srv.Close)

	cfg := validTwilioConfig()
	cfg.BaseURL = srv.URL
	s, err := NewTwilioSender(cfg)
	if err != nil {
		t.Fatalf("NewTwilioSender: %v", err)
	}
	return s, &got, &path
}

func TestTwilioSendPostsMessage(t *testing.T) {
	s, form, path := newTestTwilio(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"sid":"SM1","status":"queued"}`))
	})

	err := s.Send(context.Background(), "+15551234567", Message{
		Subject: "Reminder: clean up the deck",
		Body:    "Put the bins curbside.",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if want := "/2010-04-01/Accounts/" + validTwilioConfig().AccountSID + "/Messages.json"; *path != want {
		t.Fatalf("path = %q, want %q", *path, want)
	}
	if got := form.Get("To"); got != "+15551234567" {
		t.Fatalf("To = %q", got)
	}
	if got := form.Get("From"); got != "+15550001111" {
		t.Fatalf("From = %q", got)
	}
	body := form.Get("Body")
	if !strings.Contains(body, "clean up the deck") {
		t.Fatalf("Body = %q, missing the reminder itself", body)
	}
	if !strings.Contains(body, "Put the bins curbside.") {
		t.Fatalf("Body = %q, missing the details", body)
	}
}

func TestTwilioSendAuthenticates(t *testing.T) {
	var user, pass string
	var ok bool
	s, _, _ := newTestTwilio(t, func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok = r.BasicAuth()
		w.WriteHeader(http.StatusCreated)
	})
	if err := s.Send(context.Background(), "+15551234567", Message{Subject: "x"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !ok {
		t.Fatal("request carried no basic auth")
	}
	cfg := validTwilioConfig()
	if user != cfg.AccountSID || pass != cfg.AuthToken {
		t.Fatalf("basic auth = (%q, %q), want the account SID and token", user, pass)
	}
}

func TestTwilioSendReportsAPIError(t *testing.T) {
	s, _, _ := newTestTwilio(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":21211,"message":"The 'To' number is not a valid phone number."}`))
	})
	err := s.Send(context.Background(), "+15551234567", Message{Subject: "x"})
	if err == nil {
		t.Fatal("Send returned nil on a 400")
	}
	if !strings.Contains(err.Error(), "not a valid phone number") {
		t.Fatalf("Send = %v, want Twilio's own message surfaced", err)
	}
	if !strings.Contains(err.Error(), "21211") {
		t.Fatalf("Send = %v, want the Twilio error code", err)
	}
}

func TestTwilioSendReportsNonJSONError(t *testing.T) {
	// A proxy or captive portal answers with HTML, not Twilio's JSON.
	s, _, _ := newTestTwilio(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("<html>502 Bad Gateway</html>"))
	})
	err := s.Send(context.Background(), "+15551234567", Message{Subject: "x"})
	if err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("Send = %v, want the status reported", err)
	}
}

func TestTwilioSendValidatesRecipient(t *testing.T) {
	s, _, _ := newTestTwilio(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Error("request sent for an invalid recipient")
	})
	if err := s.Send(context.Background(), "5551234567", Message{Subject: "x"}); err == nil {
		t.Fatal("Send accepted a non-E.164 recipient")
	}
}

func TestSMSTextTruncates(t *testing.T) {
	long := strings.Repeat("a", smsMaxRunes*2)
	got := smsText(Message{Subject: "Reminder", Body: long})
	if n := len([]rune(got)); n > smsMaxRunes {
		t.Fatalf("smsText length = %d runes, want at most %d", n, smsMaxRunes)
	}
	if !strings.HasPrefix(got, "Reminder") {
		t.Fatalf("smsText = %q, want the subject kept when the body is truncated", got[:32])
	}
	if !strings.Contains(got, "…") {
		t.Fatalf("smsText = %q, want a truncation marker", got)
	}
}

// Carriers expect the recipient to be able to stop messages from the message
// itself, and the campaign's registered samples must match what is sent.
func TestSMSTextCarriesTheOptOut(t *testing.T) {
	got := smsText(Message{Subject: "Clean up the deck", Body: "Green bin only."})
	if !strings.HasSuffix(got, smsOptOut) {
		t.Fatalf("smsText = %q, want it to end with the opt-out line", got)
	}
	for _, keyword := range []string{"STOP", "HELP"} {
		if !strings.Contains(got, keyword) {
			t.Fatalf("smsText = %q, missing %s", got, keyword)
		}
	}
}

// Truncation must never eat the way out — the reminder text loses characters
// instead.
func TestSMSOptOutSurvivesTruncation(t *testing.T) {
	got := smsText(Message{Subject: strings.Repeat("x", smsMaxRunes*3)})
	if !strings.HasSuffix(got, smsOptOut) {
		t.Fatalf("a truncated message lost its opt-out line: %q", got[len(got)-60:])
	}
	if n := len([]rune(got)); n > smsMaxRunes {
		t.Fatalf("smsText length = %d runes, want at most %d", n, smsMaxRunes)
	}
}

// An empty message stays empty rather than becoming a bare opt-out line sent
// to somebody for no reason.
func TestSMSTextEmptyStaysEmpty(t *testing.T) {
	if got := smsText(Message{}); got != "" {
		t.Fatalf("smsText(empty) = %q, want empty", got)
	}
}

func TestSMSTextTruncatesOnRuneBoundary(t *testing.T) {
	// Multi-byte characters must not be cut in half — the result would be
	// invalid UTF-8 and Twilio would reject the whole message.
	got := smsText(Message{Subject: strings.Repeat("é", smsMaxRunes*2)})
	if !utf8.ValidString(got) {
		t.Fatalf("smsText produced invalid UTF-8: %q", got)
	}
	if n := len([]rune(got)); n > smsMaxRunes {
		t.Fatalf("smsText length = %d runes, want at most %d", n, smsMaxRunes)
	}
}

func TestTwilioDescribeHidesToken(t *testing.T) {
	cfg := validTwilioConfig()
	s, err := NewTwilioSender(cfg)
	if err != nil {
		t.Fatalf("NewTwilioSender: %v", err)
	}
	got := s.Describe()
	if strings.Contains(got, cfg.AuthToken) {
		t.Fatalf("Describe() = %q, which contains the auth token", got)
	}
	if strings.Contains(got, cfg.AccountSID) {
		t.Fatalf("Describe() = %q, which contains the whole account SID", got)
	}
	if !strings.Contains(got, "+15550001111") {
		t.Fatalf("Describe() = %q, want the sending number", got)
	}
}
