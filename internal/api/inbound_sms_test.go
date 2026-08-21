package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // Twilio signatures are HMAC-SHA1 by definition.
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"

	"github.com/bgrewell/donezo/internal/notify"
	"github.com/bgrewell/donezo/internal/store"
)

const testTwilioToken = "test-auth-token"

// signInbound mirrors Twilio's signing so the test can present a valid header.
func signInbound(fullURL string, params url.Values) string {
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
	mac := hmac.New(sha1.New, []byte(testTwilioToken))
	_, _ = mac.Write([]byte(b.String()))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func verifySMSContact(t *testing.T, core *store.CoreStore, userID int64, address string) {
	t.Helper()
	ctx := context.Background()
	id := "ct-" + address
	if _, err := core.CreateUserContact(ctx, store.UserContact{
		ID: id, UserID: userID, Channel: string(notify.ChannelSMS),
		Address: address, CreatedAt: "2026-08-21T00:00:00Z",
	}); err != nil {
		t.Fatalf("create contact: %v", err)
	}
	if _, err := core.StartContactChallenge(ctx, userID, id, "codehash"); err != nil {
		t.Fatalf("challenge: %v", err)
	}
	if _, err := core.VerifyUserContact(ctx, userID, id, "codehash"); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func postInbound(t *testing.T, h http.Handler, from, body, sig string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{"From": {from}, "Body": {body}}.Encode()
	req := httptest.NewRequest(http.MethodPost, "/sms", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Twilio-Signature", sig)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestInboundSMS(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t, WithTwilioAuthToken(testTwilioToken), WithPublicURL("https://donezo.site"))
	h := srv.Handler()
	ctx := context.Background()
	ben, err := srv.core.GetUserByUsername(ctx, "ben")
	if err != nil {
		t.Fatalf("get ben: %v", err)
	}
	const number = "+15551234567"
	verifySMSContact(t, srv.core, ben.ID, number)
	sign := func(from, body string) string {
		return signInbound("https://donezo.site/sms", url.Values{"From": {from}, "Body": {body}})
	}

	// A valid signed message from ben's verified number is saved to his inbox.
	rec := postInbound(t, h, number, "buy milk", sign(number, "buy milk"))
	if rec.Code != http.StatusOK {
		t.Fatalf("valid inbound = %d (%s)", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "Saved to your donezo inbox") {
		t.Errorf("no confirmation reply: %s", rec.Body)
	}
	items, err := srv.spaces.ListInboxItems(ctx, "sandbox")
	if err != nil {
		t.Fatalf("list inbox: %v", err)
	}
	if len(items) != 1 || items[0].Raw != "buy milk" {
		t.Fatalf("inbox after capture = %+v", items)
	}

	// A bad signature is a flat 403 and writes nothing.
	rec = postInbound(t, h, number, "buy milk", "wrong-signature")
	if rec.Code != http.StatusForbidden {
		t.Errorf("bad signature = %d, want 403", rec.Code)
	}

	// An unknown number gets an empty (200) reply and writes nothing.
	const unknown = "+15559999999"
	rec = postInbound(t, h, unknown, "hi", sign(unknown, "hi"))
	if rec.Code != http.StatusOK {
		t.Errorf("unknown number = %d, want 200", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "<Message>") {
		t.Errorf("unknown number got a reply body: %s", rec.Body)
	}
	items, _ = srv.spaces.ListInboxItems(ctx, "sandbox")
	if len(items) != 1 {
		t.Errorf("unknown number changed the inbox: %d items", len(items))
	}
}
