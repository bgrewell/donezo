package auth

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

// VerificationCodeDigits is the length of a contact verification code.
//
// Six digits, because it is typed by hand from a phone screen or an email,
// often one-handed, and every extra digit is a chance to give up. The
// entropy that costs is bought back by expiry and a hard attempt limit —
// see store.VerifyUserContact.
const VerificationCodeDigits = 6

// NewVerificationCode returns a fresh numeric code and the hash it is stored
// under. Only the hash reaches the database, so a leaked core.db cannot be
// read for codes that are still live.
//
// The digits come from crypto/rand rather than math/rand: a predictable code
// is the same as no code at all, since the whole point is that only someone
// holding the address could know it.
func NewVerificationCode() (code, hash string, err error) {
	max := big.NewInt(1)
	for i := 0; i < VerificationCodeDigits; i++ {
		max.Mul(max, big.NewInt(10))
	}
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", "", fmt.Errorf("auth: generate verification code: %w", err)
	}
	// Zero-padded so every code is the same length — "004213" rather than
	// "4213", which someone would otherwise type as four digits.
	code = fmt.Sprintf("%0*d", VerificationCodeDigits, n)
	return code, HashToken(code), nil
}
