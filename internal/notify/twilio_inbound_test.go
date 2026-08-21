package notify

import (
	"net/url"
	"testing"
)

// Twilio's own documented example vector (used across their SDK test suites),
// so this checks the implementation against the published algorithm, not just
// against itself.
const (
	exampleURL   = "https://mycompany.com/myapp.php?foo=1&bar=2"
	exampleToken = "12345"
	exampleSig   = "RSOYDt4T1cUTdK1PDd93/VVr8B8="
)

func exampleParams() url.Values {
	return url.Values{
		"CallSid": {"CA1234567890ABCDE"},
		"Caller":  {"+14158675309"},
		"Digits":  {"1234"},
		"From":    {"+14158675309"},
		"To":      {"+18005551212"},
	}
}

func TestValidateTwilioSignature(t *testing.T) {
	t.Parallel()
	if !ValidateTwilioSignature(exampleToken, exampleURL, exampleParams(), exampleSig) {
		t.Fatal("valid Twilio signature was rejected")
	}

	tests := []struct {
		name   string
		token  string
		url    string
		params url.Values
		sig    string
	}{
		{"wrong token", "wrong", exampleURL, exampleParams(), exampleSig},
		{"wrong url", exampleToken, "https://evil.example/sms", exampleParams(), exampleSig},
		{"empty token", "", exampleURL, exampleParams(), exampleSig},
		{"empty signature", exampleToken, exampleURL, exampleParams(), ""},
		{"garbage signature", exampleToken, exampleURL, exampleParams(), "not-base64!!"},
	}
	// A tampered parameter must not verify against the untampered signature.
	tampered := exampleParams()
	tampered.Set("Digits", "9999")
	tests = append(tests, struct {
		name   string
		token  string
		url    string
		params url.Values
		sig    string
	}{"tampered param", exampleToken, exampleURL, tampered, exampleSig})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if ValidateTwilioSignature(tt.token, tt.url, tt.params, tt.sig) {
				t.Errorf("%s: signature accepted but should have been rejected", tt.name)
			}
		})
	}
}
