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

	// These must appear VERBATIM — three rejections came from paraphrases.
	// The first is Twilio's own "would pass review" example from the 30908
	// error page; the second is the exact phrase Twilio's rejection quotes;
	// the third is the carrier no-sharing sentence; the fourth is the opt-in
	// data carve-out with the aggregator exception. A change here means a
	// change to what Twilio's automated crawler reads, so the test pins the
	// exact strings rather than their spirit.
	for _, want := range []string{
		"We do not share, sell, or provide your mobile phone number or messaging consent data to third parties or affiliates for marketing or promotional purposes",
		"No mobile information will be shared with third parties or affiliates for marketing or promotional purposes",
		"Mobile information and messaging consent are not shared with third parties or affiliates for marketing or promotional purposes",
		"Text messaging originator opt-in data and consent will not be shared with any third parties, excluding aggregators and providers of the Text Message services",
		"never sold, rented, or traded",
	} {
		normalized := strings.Join(strings.Fields(want), " ")
		if !strings.Contains(strings.Join(strings.Fields(body), " "), normalized) {
			t.Fatalf("privacy policy is missing the required verbatim wording: %q", normalized)
		}
	}

	// The old dangling "All other categories exclude..." sentence read as
	// conflicting content to the crawler and was removed; it must not creep
	// back.
	if strings.Contains(body, "All other categories exclude") {
		t.Fatal("the conflicting 'All other categories exclude' sentence is back")
	}

	// The two most load-bearing sentences must appear in the RAW HTML on a
	// single unbroken run — not only after whitespace normalization — because
	// a naive crawler greps the bytes. Three rejections make this worth
	// pinning: rewrapping the template across lines would reintroduce the
	// break that hid the phrase from a literal match.
	for _, raw := range []string{
		"We do not share, sell, or provide your mobile phone number or messaging consent data to third parties or affiliates for marketing or promotional purposes.",
		"No mobile information will be shared with third parties or affiliates for marketing or promotional purposes.",
	} {
		if !strings.Contains(body, raw) {
			t.Fatalf("required sentence is broken across lines in the raw HTML: %q", raw)
		}
	}
}

// Twilio rejects a campaign (error 30908) when the PRIVACY POLICY itself does
// not carry the messaging-program disclosures — carrying them only in the
// terms is what got the first submission rejected.
func TestPrivacyCarriesTheMessagingDisclosures(t *testing.T) {
	s := operatorServer(t)
	body := doJSON(t, s.Handler(), http.MethodGet, "/privacy", "").Body.String()
	flat := strings.Join(strings.Fields(body), " ")

	tests := []struct {
		name string
		want string
	}{
		{name: "program name", want: "donezo Reminders"},
		{name: "message frequency", want: "Message frequency:"},
		{name: "message and data rates", want: "Message and data rates may apply"},
		{name: "how consent is captured", want: "That confirmation is your opt-in"},
		{name: "consent is not traded", want: "Consent is never bought, sold, or shared"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(flat, tt.want) {
				t.Fatalf("privacy policy is missing %s (%q)", tt.name, tt.want)
			}
		})
	}
	for _, keyword := range []string{"STOP", "HELP"} {
		if !strings.Contains(body, "<strong>"+keyword+"</strong>") {
			t.Fatalf("privacy policy does not carry %s in bold", keyword)
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
