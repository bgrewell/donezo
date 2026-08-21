package notify

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // Twilio's request signature is defined as HMAC-SHA1; not our choice.
	"crypto/subtle"
	"encoding/base64"
	"net/url"
	"sort"
	"strings"
)

// ValidateTwilioSignature reports whether signature is a valid
// X-Twilio-Signature for a request Twilio made to fullURL carrying the POST
// form params, signed with authToken.
//
// Twilio builds the signed string by taking the full request URL and then, for
// each POST parameter sorted by name, appending the name immediately followed
// by its value (no delimiter); it HMAC-SHA1s that with the account auth token
// and base64-encodes the result. The comparison here is constant-time.
//
// An empty authToken or signature is always invalid: without the shared secret
// there is nothing to prove the request came from Twilio, so it must be
// rejected rather than waved through.
//
// See https://www.twilio.com/docs/usage/security#validating-requests
func ValidateTwilioSignature(authToken, fullURL string, params url.Values, signature string) bool {
	if authToken == "" || signature == "" {
		return false
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	b.WriteString(fullURL)
	for _, k := range keys {
		for _, v := range params[k] {
			b.WriteString(k)
			b.WriteString(v)
		}
	}

	mac := hmac.New(sha1.New, []byte(authToken))
	_, _ = mac.Write([]byte(b.String()))
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return subtle.ConstantTimeCompare([]byte(expected), []byte(signature)) == 1
}
