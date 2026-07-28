package auth

import (
	"regexp"
	"strings"
	"testing"
)

// inviteCodePattern is the documented rendering: "dz-" and two groups of
// five Crockford base32 symbols (uppercase, no I L O U).
var inviteCodePattern = regexp.MustCompile(`^dz-[0-9ABCDEFGHJKMNPQRSTVWXYZ]{5}-[0-9ABCDEFGHJKMNPQRSTVWXYZ]{5}$`)

func TestNewInviteCode(t *testing.T) {
	t.Parallel()
	seen := make(map[string]bool)
	for i := 0; i < 256; i++ {
		code, hash, err := NewInviteCode()
		if err != nil {
			t.Fatalf("NewInviteCode: %v", err)
		}
		if !inviteCodePattern.MatchString(code) {
			t.Fatalf("code %q does not match %s", code, inviteCodePattern)
		}
		if hash != HashInviteCode(code) {
			t.Errorf("returned hash %q != HashInviteCode(%q)", hash, code)
		}
		// The stored prefix identifies the invite without revealing the
		// second group: exactly "dz-" plus the first group.
		if prefix := code[:InviteCodePrefixLen]; !strings.HasPrefix(code, prefix) || prefix != "dz-"+code[3:8] {
			t.Errorf("prefix %q is not the dz- group of %q", prefix, code)
		}
		if seen[code] {
			t.Fatalf("duplicate code %q generated (run %d)", code, i)
		}
		seen[code] = true
	}
}

func TestHashInviteCode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		code string
	}{
		{name: "rendered code", code: "dz-ABCDE-FGHJK"},
		{name: "empty string still hashes", code: ""},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := HashInviteCode(tt.code)
			if len(h) != 64 { // hex SHA-256
				t.Fatalf("hash length = %d, want 64", len(h))
			}
			if h != HashInviteCode(tt.code) {
				t.Error("hash is not deterministic")
			}
			if strings.Contains(h, tt.code) && tt.code != "" {
				t.Error("hash contains the plaintext code")
			}
		})
	}
	if HashInviteCode("dz-AAAAA-AAAAA") == HashInviteCode("dz-AAAAA-AAAAB") {
		t.Error("distinct codes hash identically")
	}
}

// TestHashInviteCodeNormalization pins case-insensitive claims: a code
// retyped in another case (or with stray surrounding whitespace) must
// hash to the key the mint stored, while genuinely different codes must
// still hash apart.
func TestHashInviteCodeNormalization(t *testing.T) {
	t.Parallel()
	canonical := HashInviteCode("dz-ABCDE-FGHJK")
	tests := []struct {
		name string
		code string
		want bool // hash equals the canonical code's hash
	}{
		{name: "canonical form", code: "dz-ABCDE-FGHJK", want: true},
		{name: "all lowercase", code: "dz-abcde-fghjk", want: true},
		{name: "uppercase dz tag", code: "DZ-ABCDE-FGHJK", want: true},
		{name: "mixed case", code: "Dz-AbCdE-fGhJk", want: true},
		{name: "surrounding whitespace", code: "  dz-abcde-FGHJK\t", want: true},
		{name: "different symbol", code: "dz-ABCDE-FGHJ0", want: false},
		{name: "missing group", code: "dz-ABCDE", want: false},
		{name: "empty", code: "", want: false},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := HashInviteCode(tt.code) == canonical; got != tt.want {
				t.Errorf("HashInviteCode(%q) == HashInviteCode(canonical) = %v, want %v",
					tt.code, got, tt.want)
			}
		})
	}
}
