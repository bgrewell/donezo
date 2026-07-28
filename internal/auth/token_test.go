package auth

import (
	"encoding/base64"
	"testing"
)

func TestNewToken(t *testing.T) {
	t.Parallel()
	token, hash, err := NewToken()
	if err != nil {
		t.Fatalf("NewToken: %v", err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatalf("token %q is not padless base64url: %v", token, err)
	}
	if len(raw) != tokenBytes {
		t.Errorf("token entropy = %d bytes, want %d", len(raw), tokenBytes)
	}
	if got := HashToken(token); got != hash {
		t.Errorf("returned hash %q != HashToken(token) %q", hash, got)
	}

	other, _, err := NewToken()
	if err != nil {
		t.Fatalf("NewToken (second): %v", err)
	}
	if other == token {
		t.Error("two tokens are identical; rand not random?")
	}
}

func TestHashToken(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		token string
		want  string
	}{
		{
			// Fixed SHA-256 vector: the stored form must never silently
			// change or every session would be invalidated.
			name:  "known vector",
			token: "abc",
			want:  "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
		},
		{
			name:  "empty string",
			token: "",
			want:  "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := HashToken(tt.token); got != tt.want {
				t.Errorf("HashToken(%q) = %q, want %q", tt.token, got, tt.want)
			}
		})
	}
}
