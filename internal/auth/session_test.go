package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bgrewell/donezo/internal/store"
)

// sessionTestNow is the fixed "current time" for authenticator tests.
var sessionTestNow = time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)

// fakeSessionStore is a scripted SessionStore that records lookups and
// touches.
type fakeSessionStore struct {
	sess     store.Session
	user     store.User
	err      error
	touchErr error

	gotHashes []string
	touched   []string
}

func (f *fakeSessionStore) GetSessionUser(_ context.Context, tokenHash string) (store.Session, store.User, error) {
	f.gotHashes = append(f.gotHashes, tokenHash)
	if f.err != nil {
		return store.Session{}, store.User{}, f.err
	}
	return f.sess, f.user, nil
}

func (f *fakeSessionStore) TouchSession(_ context.Context, tokenHash string) error {
	f.touched = append(f.touched, tokenHash)
	return f.touchErr
}

// sessionAt builds a session expiring ttl after sessionTestNow with the
// given last-seen value (nil for never).
func sessionAt(ttl time.Duration, lastSeen *string) store.Session {
	return store.Session{
		TokenHash:  HashToken("token"),
		UserID:     1,
		CreatedAt:  sessionTestNow.Format(time.RFC3339),
		ExpiresAt:  sessionTestNow.Add(ttl).Format(time.RFC3339),
		LastSeenAt: lastSeen,
	}
}

// rfc3339Ptr renders t as an RFC 3339 pointer for LastSeenAt fields.
func rfc3339Ptr(t time.Time) *string {
	s := t.Format(time.RFC3339)
	return &s
}

func TestSessionAuthenticatorAuthenticate(t *testing.T) {
	t.Parallel()
	benUser := store.User{ID: 1, Username: "ben", DisplayName: "Ben"}
	storeFault := errors.New("disk on fire")
	tests := []struct {
		name        string
		cookie      string // "" = no cookie
		fake        *fakeSessionStore
		wantUser    string
		wantUnauth  bool
		wantErr     bool
		wantTouched int
	}{
		{
			name:       "no cookie",
			cookie:     "",
			fake:       &fakeSessionStore{},
			wantUnauth: true,
		},
		{
			name:       "unknown token",
			cookie:     "token",
			fake:       &fakeSessionStore{err: store.ErrNotFound},
			wantUnauth: true,
		},
		{
			name:       "expired session",
			cookie:     "token",
			fake:       &fakeSessionStore{sess: sessionAt(-time.Minute, nil), user: benUser},
			wantUnauth: true,
		},
		{
			name:       "expiry boundary: expires exactly now",
			cookie:     "token",
			fake:       &fakeSessionStore{sess: sessionAt(0, nil), user: benUser},
			wantUnauth: true,
		},
		{
			name:        "valid session, never seen: touched",
			cookie:      "token",
			fake:        &fakeSessionStore{sess: sessionAt(time.Hour, nil), user: benUser},
			wantUser:    "ben",
			wantTouched: 1,
		},
		{
			name:     "valid session, seen 30s ago: not touched",
			cookie:   "token",
			fake:     &fakeSessionStore{sess: sessionAt(time.Hour, rfc3339Ptr(sessionTestNow.Add(-30*time.Second))), user: benUser},
			wantUser: "ben",
		},
		{
			name:        "valid session, seen 61s ago: touched",
			cookie:      "token",
			fake:        &fakeSessionStore{sess: sessionAt(time.Hour, rfc3339Ptr(sessionTestNow.Add(-61*time.Second))), user: benUser},
			wantUser:    "ben",
			wantTouched: 1,
		},
		{
			name:        "touch failure does not reject the session",
			cookie:      "token",
			fake:        &fakeSessionStore{sess: sessionAt(time.Hour, nil), user: benUser, touchErr: storeFault},
			wantUser:    "ben",
			wantTouched: 1,
		},
		{
			name:    "store fault surfaces as non-unauthenticated error",
			cookie:  "token",
			fake:    &fakeSessionStore{err: storeFault},
			wantErr: true,
		},
		{
			name:    "malformed expiry surfaces as error",
			cookie:  "token",
			fake:    &fakeSessionStore{sess: store.Session{ExpiresAt: "not a timestamp"}, user: benUser},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a := NewSessionAuthenticator(tt.fake, WithSessionClock(func() time.Time { return sessionTestNow }))
			req := httptest.NewRequest(http.MethodGet, "/api/spaces", nil)
			if tt.cookie != "" {
				req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: tt.cookie})
			}
			user, err := a.Authenticate(req)
			switch {
			case tt.wantUnauth:
				if !errors.Is(err, ErrUnauthenticated) {
					t.Fatalf("err = %v, want ErrUnauthenticated", err)
				}
			case tt.wantErr:
				if err == nil || errors.Is(err, ErrUnauthenticated) {
					t.Fatalf("err = %v, want a non-unauthenticated error", err)
				}
			default:
				if err != nil {
					t.Fatalf("Authenticate: %v", err)
				}
				if user.Username != tt.wantUser {
					t.Errorf("user = %q, want %q", user.Username, tt.wantUser)
				}
			}
			if got := len(tt.fake.touched); got != tt.wantTouched {
				t.Errorf("touch count = %d, want %d", got, tt.wantTouched)
			}
			if tt.cookie != "" && len(tt.fake.gotHashes) == 1 {
				if want := HashToken(tt.cookie); tt.fake.gotHashes[0] != want {
					t.Errorf("looked up hash %q, want sha256 of cookie %q", tt.fake.gotHashes[0], want)
				}
			}
		})
	}
}
