package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// tokenBytes is the entropy of a session token before encoding.
const tokenBytes = 32

// NewToken returns a fresh session token — the value placed in the
// cookie, 32 random bytes base64url-encoded without padding — together
// with the hash under which it is stored. Only the hash ever touches the
// database, so a leaked core.db cannot be replayed as cookies.
func NewToken() (token, hash string, err error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("auth: generate session token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	return token, HashToken(token), nil
}

// HashToken returns the hex-encoded SHA-256 of token, the storage key
// for the sessions table.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
