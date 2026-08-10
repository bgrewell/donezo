package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/bgrewell/donezo/internal/notify"
)

// newContactServer builds a server with email configured and SMS not, which
// is the interesting asymmetry: it is how a real instance looks when the
// operator has set up one channel.
func newContactServer(t *testing.T) (*Server, *recordingSender) {
	t.Helper()
	email := &recordingSender{channel: notify.ChannelEmail}
	return newTestServer(t, WithNotifiers(notify.NewRegistry(email))), email
}

// contactFromBody pulls the contact object out of a handler response.
func contactFromBody(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var payload struct {
		Contact map[string]any `json:"contact"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode body %s: %v", body, err)
	}
	return payload.Contact
}

// codeFromMessage digs the six digits out of the delivered message.
func codeFromMessage(t *testing.T, msg notify.Message) string {
	t.Helper()
	fields := strings.FieldsFunc(msg.Subject+" "+msg.Body, func(r rune) bool {
		return r < '0' || r > '9'
	})
	for _, f := range fields {
		if len(f) == 6 {
			return f
		}
	}
	t.Fatalf("no six-digit code in %+v", msg)
	return ""
}

func TestContactLifecycle(t *testing.T) {
	s, email := newContactServer(t)
	h := s.Handler()

	rec := doJSON(t, h, http.MethodPost, "/api/notify/contacts",
		`{"channel":"email","address":"ben@example.com","label":"inbox"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", rec.Code, rec.Body)
	}
	contact := contactFromBody(t, rec.Body.Bytes())
	id, _ := contact["id"].(string)
	if id == "" {
		t.Fatalf("no id in %v", contact)
	}
	if _, verified := contact["verifiedAt"]; verified {
		t.Fatal("a new contact came back verified")
	}

	// The code was sent, and only to the address just added.
	sent := email.deliveries()
	if len(sent) != 1 {
		t.Fatalf("sent %d messages on create, want 1", len(sent))
	}
	if sent[0].to != "ben@example.com" {
		t.Fatalf("code sent to %q", sent[0].to)
	}
	code := codeFromMessage(t, sent[0].msg)

	rec = doJSON(t, h, http.MethodPost, "/api/notify/contacts/"+id+"/verify", `{"code":"`+code+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("verify = %d: %s", rec.Code, rec.Body)
	}
	if contactFromBody(t, rec.Body.Bytes())["verifiedAt"] == nil {
		t.Fatal("contact not verified after the right code")
	}

	rec = doJSON(t, h, http.MethodGet, "/api/notify/contacts", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "ben@example.com") {
		t.Fatalf("list = %d: %s", rec.Code, rec.Body)
	}

	rec = doJSON(t, h, http.MethodDelete, "/api/notify/contacts/"+id, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d: %s", rec.Code, rec.Body)
	}
}

func TestCreateContactValidation(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantText   string
	}{
		{
			name: "unknown channel", body: `{"channel":"pigeon","address":"ben@example.com"}`,
			wantStatus: http.StatusBadRequest, wantText: "channel must be one of",
		},
		{
			name: "malformed email", body: `{"channel":"email","address":"not-an-address"}`,
			wantStatus: http.StatusBadRequest, wantText: "plain email address",
		},
		{
			name: "email with a display name", body: `{"channel":"email","address":"Ben <ben@example.com>"}`,
			wantStatus: http.StatusBadRequest, wantText: "plain email address",
		},
		{
			name: "unconfigured channel", body: `{"channel":"sms","address":"+15551234567"}`,
			wantStatus: http.StatusConflict, wantText: "cannot send sms",
		},
		{
			name: "overlong label", body: `{"channel":"email","address":"ben@example.com","label":"` + strings.Repeat("x", 41) + `"}`,
			wantStatus: http.StatusBadRequest, wantText: "label must be",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := newContactServer(t)
			rec := doJSON(t, s.Handler(), http.MethodPost, "/api/notify/contacts", tt.body)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.wantStatus, rec.Body)
			}
			if !strings.Contains(rec.Body.String(), tt.wantText) {
				t.Fatalf("body = %s, want it to mention %q", rec.Body, tt.wantText)
			}
		})
	}
}

func TestCreateContactRejectsDuplicate(t *testing.T) {
	s, _ := newContactServer(t)
	h := s.Handler()
	body := `{"channel":"email","address":"ben@example.com"}`
	if rec := doJSON(t, h, http.MethodPost, "/api/notify/contacts", body); rec.Code != http.StatusCreated {
		t.Fatalf("first create = %d: %s", rec.Code, rec.Body)
	}
	rec := doJSON(t, h, http.MethodPost, "/api/notify/contacts", body)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate create = %d, want 409: %s", rec.Code, rec.Body)
	}
}

func TestVerifyContactWrongCode(t *testing.T) {
	s, email := newContactServer(t)
	h := s.Handler()
	rec := doJSON(t, h, http.MethodPost, "/api/notify/contacts", `{"channel":"email","address":"ben@example.com"}`)
	id := contactFromBody(t, rec.Body.Bytes())["id"].(string)
	real := codeFromMessage(t, email.deliveries()[0].msg)

	wrong := "000000"
	if wrong == real {
		wrong = "111111"
	}
	rec = doJSON(t, h, http.MethodPost, "/api/notify/contacts/"+id+"/verify", `{"code":"`+wrong+`"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("wrong code = %d, want 400: %s", rec.Code, rec.Body)
	}

	// And the destination is still not deliverable.
	rec = doJSON(t, h, http.MethodGet, "/api/notify/contacts", "")
	if strings.Contains(rec.Body.String(), "verifiedAt") {
		t.Fatalf("contact verified after a wrong code: %s", rec.Body)
	}
}

func TestVerifyContactRequiresCode(t *testing.T) {
	s, _ := newContactServer(t)
	h := s.Handler()
	rec := doJSON(t, h, http.MethodPost, "/api/notify/contacts", `{"channel":"email","address":"ben@example.com"}`)
	id := contactFromBody(t, rec.Body.Bytes())["id"].(string)

	rec = doJSON(t, h, http.MethodPost, "/api/notify/contacts/"+id+"/verify", `{"code":""}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty code = %d, want 400: %s", rec.Code, rec.Body)
	}
}

func TestResendContactCodeIsThrottled(t *testing.T) {
	s, _ := newContactServer(t)
	h := s.Handler()
	rec := doJSON(t, h, http.MethodPost, "/api/notify/contacts", `{"channel":"email","address":"ben@example.com"}`)
	id := contactFromBody(t, rec.Body.Bytes())["id"].(string)

	// The clock is fixed, so the resend is inside the interval by
	// construction — which is the case that matters, since this endpoint
	// sends to an address that may not belong to the person clicking.
	rec = doJSON(t, h, http.MethodPost, "/api/notify/contacts/"+id+"/code", "")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("immediate resend = %d, want 429: %s", rec.Code, rec.Body)
	}
}

func TestContactsAreNotVisibleAcrossUsers(t *testing.T) {
	s, _ := newContactServer(t)
	h := s.Handler()
	rec := doJSON(t, h, http.MethodPost, "/api/notify/contacts", `{"channel":"email","address":"ben@example.com"}`)
	id := contactFromBody(t, rec.Body.Bytes())["id"].(string)

	// newTestServer authenticates as ben; swap the identity to the other
	// seeded user and try the same id.
	other, err := s.core.GetUserByUsername(t.Context(), "other")
	if err != nil {
		t.Fatalf("get other user: %v", err)
	}
	s.auth = StaticAuthenticator{User: other}
	h = s.Handler()

	for _, tt := range []struct {
		method, path, body string
	}{
		{http.MethodDelete, "/api/notify/contacts/" + id, ""},
		{http.MethodPost, "/api/notify/contacts/" + id + "/code", ""},
		{http.MethodPost, "/api/notify/contacts/" + id + "/verify", `{"code":"123456"}`},
	} {
		rec := doJSON(t, h, tt.method, tt.path, tt.body)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s %s as another user = %d, want 404: %s", tt.method, tt.path, rec.Code, rec.Body)
		}
	}
	if rec := doJSON(t, h, http.MethodGet, "/api/notify/contacts", ""); strings.Contains(rec.Body.String(), "ben@example.com") {
		t.Fatalf("another user's listing contains the address: %s", rec.Body)
	}
}

func TestNotifyStatusReportsConfiguredChannels(t *testing.T) {
	s, _ := newContactServer(t)
	rec := doJSON(t, s.Handler(), http.MethodGet, "/api/notify/status", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	var payload struct {
		Channels []channelStatus `json:"channels"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Channels) != len(notify.Channels) {
		t.Fatalf("got %d channels, want %d", len(payload.Channels), len(notify.Channels))
	}
	for _, c := range payload.Channels {
		switch c.Channel {
		case "email":
			if !c.Configured {
				t.Fatal("email reported as unconfigured")
			}
		case "sms":
			if c.Configured {
				t.Fatal("sms reported as configured when it is not")
			}
			if c.Provider != "" {
				t.Fatalf("unconfigured channel described a provider: %q", c.Provider)
			}
		}
	}
}

func TestNotifyStatusCarriesNoCredentials(t *testing.T) {
	// The status endpoint is readable by every signed-in user, so whatever
	// Describe returns is effectively public to the instance's members.
	const password = "hunter2-super-secret"
	sender, err := notify.NewSMTPSender(notify.SMTPConfig{
		Host: "smtp.example.com", Port: 587, From: "donezo@example.com",
		Username: "relay-user", Password: password,
	})
	if err != nil {
		t.Fatalf("NewSMTPSender: %v", err)
	}
	s := newTestServer(t, WithNotifiers(notify.NewRegistry(sender)))

	rec := doJSON(t, s.Handler(), http.MethodGet, "/api/notify/status", "")
	body := rec.Body.String()
	if strings.Contains(body, password) {
		t.Fatalf("status response contains the relay password: %s", body)
	}
	if strings.Contains(body, "relay-user") {
		t.Fatalf("status response contains the relay username: %s", body)
	}
}
