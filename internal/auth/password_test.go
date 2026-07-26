package auth

import (
	"strings"
	"testing"
)

// fastArgon2 returns a hasher with deliberately weak parameters so
// tests stay quick; correctness is independent of cost.
func fastArgon2() *Argon2 {
	return NewArgon2(WithTime(1), WithMemory(8), WithThreads(1), WithKeyLength(16), WithSaltLength(8))
}

func TestValidatePassword(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		password string
		wantErr  bool
	}{
		{name: "empty", password: "", wantErr: true},
		{name: "nine chars", password: "123456789", wantErr: true},
		{name: "ten chars boundary", password: "1234567890"},
		{name: "long", password: "correct horse battery staple"},
		{name: "ten multibyte runes", password: "pässwörter"},
		{name: "nine multibyte runes", password: "pässwörte", wantErr: true},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidatePassword(tt.password)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePassword(%q) err = %v, wantErr %v", tt.password, err, tt.wantErr)
			}
		})
	}
}

func TestArgon2RoundTrip(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		hasher *Argon2
	}{
		{name: "default parameters", hasher: NewArgon2()},
		{name: "fast parameters", hasher: fastArgon2()},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			const password = "correct horse battery staple"
			encoded, err := tt.hasher.Hash(password)
			if err != nil {
				t.Fatalf("Hash: %v", err)
			}
			if !strings.HasPrefix(encoded, "$argon2id$v=19$") {
				t.Errorf("encoded = %q, want $argon2id$v=19$ prefix", encoded)
			}
			ok, err := tt.hasher.Verify(encoded, password)
			if err != nil {
				t.Fatalf("Verify (correct): %v", err)
			}
			if !ok {
				t.Error("Verify (correct) = false, want true")
			}
			ok, err = tt.hasher.Verify(encoded, "not the password")
			if err != nil {
				t.Fatalf("Verify (wrong): %v", err)
			}
			if ok {
				t.Error("Verify (wrong) = true, want false")
			}
		})
	}
}

func TestArgon2HashSaltsDiffer(t *testing.T) {
	t.Parallel()
	h := fastArgon2()
	a, err := h.Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	b, err := h.Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if a == b {
		t.Errorf("two hashes of the same password are identical (%q); salt not random?", a)
	}
}

func TestArgon2HashEmptyPassword(t *testing.T) {
	t.Parallel()
	if _, err := fastArgon2().Hash(""); err == nil {
		t.Fatal("Hash(\"\") = nil error, want refusal")
	}
}

// TestArgon2VerifyLegacyParams proves parameters are decoded from the
// stored string: hashes written with old (non-default) parameters keep
// verifying under a hasher configured with different ones.
func TestArgon2VerifyLegacyParams(t *testing.T) {
	t.Parallel()
	const password = "correct horse battery staple"
	legacy := NewArgon2(WithTime(2), WithMemory(16), WithThreads(2), WithKeyLength(24), WithSaltLength(12))
	encoded, err := legacy.Hash(password)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if !strings.Contains(encoded, "$m=16,t=2,p=2$") {
		t.Fatalf("encoded = %q, want legacy params m=16,t=2,p=2", encoded)
	}
	current := NewArgon2() // different (default) parameters
	ok, err := current.Verify(encoded, password)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !ok {
		t.Error("Verify = false, want true (params must come from the stored string)")
	}
	ok, err = current.Verify(encoded, "wrong password!")
	if err != nil {
		t.Fatalf("Verify (wrong): %v", err)
	}
	if ok {
		t.Error("Verify (wrong) = true, want false")
	}
}

func TestArgon2VerifyMalformed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		encoded string
	}{
		{name: "empty", encoded: ""},
		{name: "not a hash", encoded: "hunter2"},
		{name: "too few sections", encoded: "$argon2id$v=19$m=8,t=1,p=1$c2FsdA"},
		{name: "wrong variant argon2i", encoded: "$argon2i$v=19$m=8,t=1,p=1$c2FsdA$aGFzaA"},
		{name: "wrong version", encoded: "$argon2id$v=18$m=8,t=1,p=1$c2FsdA$aGFzaA"},
		{name: "bad salt base64", encoded: "$argon2id$v=19$m=8,t=1,p=1$!!!$aGFzaA"},
		{name: "bad key base64", encoded: "$argon2id$v=19$m=8,t=1,p=1$c2FsdA$!!!"},
		{name: "empty salt", encoded: "$argon2id$v=19$m=8,t=1,p=1$$aGFzaA"},
		{name: "zero passes", encoded: "$argon2id$v=19$m=8,t=0,p=1$c2FsdA$aGFzaA"},
		{name: "zero parallelism", encoded: "$argon2id$v=19$m=8,t=1,p=0$c2FsdA$aGFzaA"},
		{name: "oversized parallelism", encoded: "$argon2id$v=19$m=8,t=1,p=300$c2FsdA$aGFzaA"},
		{name: "absurd memory", encoded: "$argon2id$v=19$m=4294967295,t=1,p=1$c2FsdA$aGFzaA"},
		{name: "memory just above the cap", encoded: "$argon2id$v=19$m=262145,t=1,p=1$c2FsdA$aGFzaA"},
		{name: "1 GiB memory (old cap)", encoded: "$argon2id$v=19$m=1048576,t=1,p=1$c2FsdA$aGFzaA"},
		{name: "negative param", encoded: "$argon2id$v=19$m=-8,t=1,p=1$c2FsdA$aGFzaA"},
		{name: "missing param", encoded: "$argon2id$v=19$m=8,t=1$c2FsdA$aGFzaA"},
		{name: "reordered params", encoded: "$argon2id$v=19$t=1,m=8,p=1$c2FsdA$aGFzaA"},
	}
	h := fastArgon2()
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := h.Verify(tt.encoded, "whatever password"); err == nil {
				t.Errorf("Verify(%q) = nil error, want malformed-hash error", tt.encoded)
			}
		})
	}
}

// TestParsePHCParamsMemoryCap pins the decode-time memory ceiling: a
// tampered or corrupted stored hash must never be able to demand more
// than 256 MiB per verification, because Verify pays the full argon2
// cost before comparing. Parameters are checked at the parse layer so
// the boundary can be tested without actually allocating 256 MiB.
func TestParsePHCParamsMemoryCap(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		section string
		wantErr bool
	}{
		{name: "default memory well under the cap", section: "m=65536,t=1,p=4"},
		{name: "exactly at the 256 MiB cap", section: "m=262144,t=1,p=1"},
		{name: "one KiB above the cap", section: "m=262145,t=1,p=1", wantErr: true},
		{name: "1 GiB (the old, too-generous cap)", section: "m=1048576,t=1,p=1", wantErr: true},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			params, err := parsePHCParams(tt.section)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parsePHCParams(%q) err = %v, wantErr %v", tt.section, err, tt.wantErr)
			}
			if !tt.wantErr && params.memory > maxArgon2Memory {
				t.Errorf("accepted memory %d exceeds cap %d", params.memory, maxArgon2Memory)
			}
		})
	}
}

// TestDummyHashIsUsable guards the DummyHash constant: it must decode
// as a well-formed argon2id hash (so timing equalization actually runs
// the KDF) and must not match any password.
func TestDummyHashIsUsable(t *testing.T) {
	t.Parallel()
	ok, err := NewArgon2().Verify(DummyHash, "any password at all")
	if err != nil {
		t.Fatalf("Verify(DummyHash): %v", err)
	}
	if ok {
		t.Error("Verify(DummyHash) = true, want false")
	}
}
