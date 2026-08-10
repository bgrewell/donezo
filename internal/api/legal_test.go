package api

import (
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/bgrewell/donezo/internal/notify"
)

// operatorServer publishes the policy pages, as a registered instance does.
func operatorServer(t *testing.T, extra ...ServerOption) *Server {
	t.Helper()
	opts := append([]ServerOption{WithOperator("Grewell Tech", "ben@grewelltech.com")}, extra...)
	return newTestServer(t, opts...)
}

// The pages have to be readable by somebody with no account — the carrier
// reviewing them does not have one, and a policy behind a login is not
// published.
func TestPolicyPagesArePublic(t *testing.T) {
	s := operatorServer(t)
	s.auth = anonymousAuthenticator{}
	h := s.Handler()

	for _, path := range []string{"/privacy", "/terms"} {
		rec := doJSON(t, h, http.MethodGet, path, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("anonymous GET %s = %d, want 200: %s", path, rec.Code, rec.Body)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Fatalf("GET %s served %q, want HTML", path, ct)
		}
	}
}

// An instance that has not named an operator must not publish a policy in
// somebody else's name — or in nobody's.
func TestPolicyPagesUnpublishedWithoutAnOperator(t *testing.T) {
	s := newTestServer(t)
	h := s.Handler()
	for _, path := range []string{"/privacy", "/terms"} {
		rec := doJSON(t, h, http.MethodGet, path, "")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("GET %s with no operator = %d, want 404: %s", path, rec.Code, rec.Body)
		}
	}
}

func TestPolicyPagesNameTheOperator(t *testing.T) {
	s := operatorServer(t)
	h := s.Handler()
	for _, path := range []string{"/privacy", "/terms"} {
		body := doJSON(t, h, http.MethodGet, path, "").Body.String()
		if !strings.Contains(body, "Grewell Tech") {
			t.Fatalf("GET %s does not name the operator", path)
		}
		if !strings.Contains(body, "ben@grewelltech.com") {
			t.Fatalf("GET %s does not carry the support address", path)
		}
	}
}

// What a carrier reviewer checks the terms for. Each of these is a rejection
// if it is missing, so each gets an assertion.
func TestTermsCarryTheCarrierRequirements(t *testing.T) {
	s := operatorServer(t)
	body := doJSON(t, s.Handler(), http.MethodGet, "/terms", "").Body.String()

	tests := []struct {
		name    string
		pattern string
	}{
		{name: "program name", pattern: `donezo Reminders`},
		{name: "program description", pattern: `reminders that you scheduled for`},
		{name: "message and data rates", pattern: `Message and data rates may apply`},
		{name: "message frequency", pattern: `[Mm]essage frequency`},
		{name: "support contact", pattern: `ben@grewelltech\.com`},
		{name: "carrier liability", pattern: `not liable for delayed or`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !regexp.MustCompile(tt.pattern).MatchString(body) {
				t.Fatalf("terms are missing %s (/%s/)", tt.name, tt.pattern)
			}
		})
	}

	// HELP and STOP must be bold — the requirement names the emphasis, not
	// just the words, and a reviewer checks for it.
	for _, keyword := range []string{"STOP", "HELP"} {
		if !strings.Contains(body, "<strong>"+keyword+"</strong>") {
			t.Fatalf("terms do not carry %s in bold", keyword)
		}
	}
}

// The sentence carriers look for in the privacy policy. Its exact shape
// matters more than its presence in spirit.
func TestPrivacyCarriesTheNoSharingLanguage(t *testing.T) {
	s := operatorServer(t)
	body := doJSON(t, s.Handler(), http.MethodGet, "/privacy", "").Body.String()

	for _, want := range []string{
		"No mobile\n  information is shared with third parties or affiliates for marketing or\n  promotional purposes",
		"not sold, rented, or traded",
		"opt-in data and\n  consent are never shared",
	} {
		normalized := strings.Join(strings.Fields(want), " ")
		if !strings.Contains(strings.Join(strings.Fields(body), " "), normalized) {
			t.Fatalf("privacy policy is missing the required wording: %q", normalized)
		}
	}
}

// The disclosure must describe THIS deployment. An instance that sends to
// Twilio has to say so; one that does not, must not claim it.
func TestPrivacyDisclosesConfiguredProcessorsOnly(t *testing.T) {
	t.Run("with SMS configured", func(t *testing.T) {
		sms := &recordingSender{channel: notify.ChannelSMS}
		s := operatorServer(t, WithNotifiers(notify.NewRegistry(sms)))
		body := doJSON(t, s.Handler(), http.MethodGet, "/privacy", "").Body.String()
		if !strings.Contains(body, "Twilio") {
			t.Fatal("SMS is configured but Twilio is not disclosed")
		}
		if strings.Contains(body, "no third-party service at all") {
			t.Fatal("policy claims nothing is shared while SMS is configured")
		}
	})

	t.Run("with nothing configured", func(t *testing.T) {
		s := operatorServer(t)
		body := doJSON(t, s.Handler(), http.MethodGet, "/privacy", "").Body.String()
		if strings.Contains(body, "Twilio") {
			t.Fatal("policy discloses Twilio on an instance that cannot send SMS")
		}
		if !strings.Contains(body, "no third-party service at all") {
			t.Fatal("policy does not state that nothing leaves this instance")
		}
	})
}

// The operator name reaches HTML, so it must be escaped like any other
// interpolated value — even though only the operator sets it.
func TestPolicyPagesEscapeTheOperatorName(t *testing.T) {
	s := newTestServer(t, WithOperator(`Acme<script>alert(1)</script>`, "ops@example.com"))
	body := doJSON(t, s.Handler(), http.MethodGet, "/terms", "").Body.String()
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Fatalf("operator name was interpolated unescaped:\n%s", body)
	}
	if !strings.Contains(body, "Acme&lt;script&gt;") {
		t.Fatalf("operator name is missing entirely, want it escaped:\n%s", body)
	}
}

// A "last updated" that moved on every request would tell a reader nothing,
// and a reviewer reads it as the date the terms were set.
func TestPolicyRevisionIsStable(t *testing.T) {
	s := operatorServer(t)
	first := doJSON(t, s.Handler(), http.MethodGet, "/terms", "").Body.String()
	second := doJSON(t, s.Handler(), http.MethodGet, "/terms", "").Body.String()
	if first != second {
		t.Fatal("the terms changed between two requests")
	}
	if !strings.Contains(first, policyRevision) {
		t.Fatalf("terms do not carry the revision date %q", policyRevision)
	}
}
