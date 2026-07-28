package auth

import (
	"crypto/rand"
	"fmt"
	"strings"
)

// APITokenScheme is the prefix every donezo API token carries. It makes a
// leaked token recognizable at a glance (so it can be revoked) and lets
// the MCP bearer check reject obviously non-donezo credentials early.
const APITokenScheme = "dzmcp-"

// apiTokenSymbols is how many Crockford base32 symbols follow the scheme
// prefix. At 5 bits per symbol, 26 symbols carry 130 bits of entropy —
// comfortably past the ~128-bit target, and far beyond what the MCP rate
// limiter would let anyone enumerate.
const apiTokenSymbols = 26

// APITokenPrefixLen is how much of a rendered token ("dzmcp-" plus the
// first six symbols) is stored in plaintext for the owner's listing; the
// full token is only ever stored hashed. Twelve characters name a token
// without leaking enough to reconstruct it.
const APITokenPrefixLen = len(APITokenScheme) + 6

// NewAPIToken returns a fresh API token — the value the user places in
// their MCP client's Authorization header, "dzmcp-" followed by 26
// Crockford base32 symbols — together with the hash under which it is
// stored and the plaintext prefix kept for the listing. Like session
// tokens and invite codes, only the hash ever touches the database, so a
// leaked core.db cannot be replayed as a bearer credential.
func NewAPIToken() (token, hash, prefix string, err error) {
	raw := make([]byte, apiTokenSymbols)
	if _, err := rand.Read(raw); err != nil {
		return "", "", "", fmt.Errorf("auth: generate api token: %w", err)
	}
	var b strings.Builder
	b.WriteString(APITokenScheme)
	for _, v := range raw {
		// 32 divides 256, so the modulo introduces no bias (the same
		// reasoning invite-code generation relies on).
		b.WriteByte(inviteAlphabet[v%byte(len(inviteAlphabet))])
	}
	token = b.String()
	return token, HashToken(token), token[:APITokenPrefixLen], nil
}
