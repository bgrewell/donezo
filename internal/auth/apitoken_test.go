package auth

import (
	"strings"
	"testing"
)

func TestNewAPIToken(t *testing.T) {
	t.Parallel()
	token, hash, prefix, err := NewAPIToken()
	if err != nil {
		t.Fatalf("NewAPIToken: %v", err)
	}

	if !strings.HasPrefix(token, APITokenScheme) {
		t.Errorf("token %q missing scheme prefix %q", token, APITokenScheme)
	}
	wantLen := len(APITokenScheme) + apiTokenSymbols
	if len(token) != wantLen {
		t.Errorf("token length = %d, want %d", len(token), wantLen)
	}
	if prefix != token[:APITokenPrefixLen] {
		t.Errorf("prefix = %q, want %q", prefix, token[:APITokenPrefixLen])
	}
	if len(prefix) != APITokenPrefixLen {
		t.Errorf("prefix length = %d, want %d", len(prefix), APITokenPrefixLen)
	}

	// The hash is the SHA-256 of the plaintext, and never equals it.
	if hash != HashToken(token) {
		t.Error("returned hash does not match HashToken(token)")
	}
	if hash == token {
		t.Error("hash must not equal the plaintext token")
	}

	// The symbol body uses only the Crockford base32 alphabet.
	for _, r := range token[len(APITokenScheme):] {
		if !strings.ContainsRune(inviteAlphabet, r) {
			t.Errorf("token symbol %q outside base32 alphabet", string(r))
		}
	}
}

func TestNewAPITokenUnique(t *testing.T) {
	t.Parallel()
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		token, _, _, err := NewAPIToken()
		if err != nil {
			t.Fatalf("NewAPIToken: %v", err)
		}
		if seen[token] {
			t.Fatalf("duplicate token generated: %q", token)
		}
		seen[token] = true
	}
}
