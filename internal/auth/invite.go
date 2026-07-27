package auth

import (
	"crypto/rand"
	"fmt"
	"strings"
)

// inviteAlphabet is Crockford base32, uppercase: no I, L, O, or U, so a
// code survives being read aloud or retyped. Its 32 symbols divide the
// byte range evenly, so mapping bytes with a modulo stays uniform.
const inviteAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// Invite codes render as "dz-XXXXX-XXXXX": two groups of five base32
// symbols, 50 bits of entropy — far beyond what the credential rate
// limiter lets anyone enumerate.
const (
	inviteGroupLen = 5
	inviteGroups   = 2
)

// InviteCodePrefixLen is how much of a rendered invite code ("dz-" plus
// the first group) is stored in plaintext for the admin listing; the
// full code is only ever stored hashed.
const InviteCodePrefixLen = 8

// NewInviteCode returns a fresh invite code in the human-friendly form
// "dz-XXXXX-XXXXX" together with the hash under which it is stored.
// Like session tokens, only the hash ever touches the database, so a
// leaked core.db does not leak claimable codes.
func NewInviteCode() (code, hash string, err error) {
	raw := make([]byte, inviteGroups*inviteGroupLen)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("auth: generate invite code: %w", err)
	}
	var b strings.Builder
	b.WriteString("dz")
	for g := 0; g < inviteGroups; g++ {
		b.WriteByte('-')
		for i := 0; i < inviteGroupLen; i++ {
			// 32 divides 256, so the modulo introduces no bias.
			b.WriteByte(inviteAlphabet[raw[g*inviteGroupLen+i]%byte(len(inviteAlphabet))])
		}
	}
	code = b.String()
	return code, HashInviteCode(code), nil
}

// HashInviteCode returns the hex-encoded SHA-256 of code's canonical
// form (see normalizeInviteCode), the storage and lookup key for the
// invites table. Normalizing before hashing is what makes claims
// case-insensitive: a code retyped in the wrong case hashes to the same
// key the mint stored. Claims compare hashes, not codes: a SHA-256
// preimage cannot be steered by lookup timing, the same reasoning
// session-token lookups rely on, which is what makes the database-side
// claim effectively constant-time in the secret.
func HashInviteCode(code string) string {
	return HashToken(normalizeInviteCode(code))
}

// normalizeInviteCode maps code to the canonical rendering NewInviteCode
// produces: surrounding whitespace dropped, symbols upper-cased, and the
// leading "dz-" tag back in lowercase. Codes are read off screens and
// paper and retyped by hand — the reason the alphabet is Crockford
// base32 — so a claim must not hinge on the case the sender's keyboard
// happened to produce. Generated codes are already canonical, so their
// stored hashes are unchanged by this mapping.
func normalizeInviteCode(code string) string {
	c := strings.ToUpper(strings.TrimSpace(code))
	if strings.HasPrefix(c, "DZ-") {
		c = "dz" + c[2:]
	}
	return c
}
