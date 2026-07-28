// Package auth implements donezod authentication: argon2id password
// hashing with PHC string encoding, opaque session tokens stored only as
// hashes, a session-cookie Authenticator for the API layer, a
// sliding-window rate limiter for credential endpoints, and a background
// sweeper that clears expired state.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// MinPasswordLength is the minimum accepted password length, counted in
// characters (runes).
const MinPasswordLength = 10

// ValidatePassword checks a candidate password against the local policy:
// non-empty and at least MinPasswordLength characters. The returned
// error text is calm and safe to show directly to the user.
func ValidatePassword(password string) error {
	if password == "" {
		return errors.New("a password is required")
	}
	if utf8.RuneCountInString(password) < MinPasswordLength {
		return fmt.Errorf("please use a password of at least %d characters", MinPasswordLength)
	}
	return nil
}

// PasswordHasher hashes passwords for storage and verifies candidates
// against stored encodings. It is the seam that lets tests substitute a
// fast, countable implementation for the real argon2id one.
type PasswordHasher interface {
	// Hash returns a self-describing encoding of password suitable for
	// storage.
	Hash(password string) (string, error)
	// Verify reports whether password matches the stored encoding.
	// The bool is meaningless when the error is non-nil.
	Verify(encoded, password string) (bool, error)
}

// Default argon2id parameters: the interactive-grade settings from the
// argon2 recommendations (RFC 9106 second recommendation, adjusted to
// this decade's hardware). New hashes use these; verification always
// uses the parameters embedded in the stored encoding, so the defaults
// can evolve without invalidating existing hashes.
const (
	defaultArgon2Time    = 1
	defaultArgon2Memory  = 64 * 1024 // KiB (64 MiB)
	defaultArgon2Threads = 4
	defaultArgon2KeyLen  = 32
	defaultArgon2SaltLen = 16
)

// maxArgon2Memory caps the memory parameter accepted when decoding a
// stored hash: 256 MiB in KiB, four times the current default — no
// legitimate hash for this application needs more. Stored encodings
// are server-written, but Verify pays the full argon2 cost before
// comparing, so without a tight cap one tampered row would turn every
// login attempt against that account into a memory-amplification bomb.
const maxArgon2Memory = 256 * 1024

// DummyHash is a valid argon2id encoding (default parameters) of a
// random secret that was discarded at generation time. Login flows
// verify candidate passwords against it when the username is unknown or
// the account has no password set, so those paths cost the same argon2
// work as a real verification and cannot be told apart by timing. The
// result is always discarded and the request refused regardless.
const DummyHash = "$argon2id$v=19$m=65536,t=1,p=4$1Hn2w7bGYMjXSnrZxsLAdA$NskUyis6j92UhFNZfntGL5sYxXrtrZQ4UeiWD0F72iI"

// Argon2 is the production PasswordHasher: argon2id keyed hashes in PHC
// string form ($argon2id$v=19$m=...,t=...,p=...$salt$hash, base64
// without padding). Construct with NewArgon2.
type Argon2 struct {
	time    uint32
	memory  uint32 // KiB
	threads uint8
	keyLen  uint32
	saltLen uint32
}

// Argon2Option configures an Argon2 hasher (functional options pattern).
type Argon2Option func(*Argon2)

// WithTime sets the argon2 time (passes) parameter for new hashes.
// Values below 1 are ignored, keeping the default.
func WithTime(t uint32) Argon2Option {
	return func(a *Argon2) {
		if t >= 1 {
			a.time = t
		}
	}
}

// WithMemory sets the argon2 memory parameter, in KiB, for new hashes.
// Zero is ignored, keeping the default.
func WithMemory(kib uint32) Argon2Option {
	return func(a *Argon2) {
		if kib > 0 {
			a.memory = kib
		}
	}
}

// WithThreads sets the argon2 parallelism parameter for new hashes.
// Zero is ignored, keeping the default.
func WithThreads(p uint8) Argon2Option {
	return func(a *Argon2) {
		if p > 0 {
			a.threads = p
		}
	}
}

// WithKeyLength sets the derived key length in bytes for new hashes.
// Zero is ignored, keeping the default.
func WithKeyLength(n uint32) Argon2Option {
	return func(a *Argon2) {
		if n > 0 {
			a.keyLen = n
		}
	}
}

// WithSaltLength sets the salt length in bytes for new hashes. Zero is
// ignored, keeping the default.
func WithSaltLength(n uint32) Argon2Option {
	return func(a *Argon2) {
		if n > 0 {
			a.saltLen = n
		}
	}
}

// NewArgon2 returns an argon2id hasher with the default interactive
// parameters (time=1, memory=64 MiB, threads=4, 32-byte key, 16-byte
// salt), adjusted by opts.
func NewArgon2(opts ...Argon2Option) *Argon2 {
	a := &Argon2{
		time:    defaultArgon2Time,
		memory:  defaultArgon2Memory,
		threads: defaultArgon2Threads,
		keyLen:  defaultArgon2KeyLen,
		saltLen: defaultArgon2SaltLen,
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// Hash derives an argon2id key from password under a fresh random salt
// and returns the PHC-encoded string.
func (a *Argon2) Hash(password string) (string, error) {
	if password == "" {
		return "", errors.New("auth: refusing to hash an empty password")
	}
	salt := make([]byte, a.saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: generate salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, a.time, a.memory, a.threads, a.keyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, a.memory, a.time, a.threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// Verify re-derives the key using the parameters embedded in encoded —
// not the hasher's own, so stored hashes keep verifying after the
// defaults change — and compares in constant time.
func (a *Argon2) Verify(encoded, password string) (bool, error) {
	params, salt, key, err := decodePHC(encoded)
	if err != nil {
		return false, err
	}
	candidate := argon2.IDKey([]byte(password), salt, params.time, params.memory, params.threads, uint32(len(key)))
	return subtle.ConstantTimeCompare(candidate, key) == 1, nil
}

// phcParams are the argon2 cost parameters carried by a PHC string.
type phcParams struct {
	time    uint32
	memory  uint32
	threads uint8
}

// decodePHC parses a $argon2id$v=19$m=...,t=...,p=...$salt$hash string
// into its parameters, salt, and key.
func decodePHC(encoded string) (phcParams, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" {
		return phcParams{}, nil, nil, errors.New("auth: malformed password hash")
	}
	if parts[1] != "argon2id" {
		return phcParams{}, nil, nil, fmt.Errorf("auth: unsupported hash variant %q", parts[1])
	}
	version, err := phcField(parts[2], "v", 32)
	if err != nil {
		return phcParams{}, nil, nil, err
	}
	if version != argon2.Version {
		return phcParams{}, nil, nil, fmt.Errorf("auth: unsupported argon2 version %d", version)
	}
	params, err := parsePHCParams(parts[3])
	if err != nil {
		return phcParams{}, nil, nil, err
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) == 0 {
		return phcParams{}, nil, nil, errors.New("auth: malformed password hash salt")
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(key) == 0 {
		return phcParams{}, nil, nil, errors.New("auth: malformed password hash key")
	}
	return params, salt, key, nil
}

// parsePHCParams parses the "m=...,t=...,p=..." section, rejecting
// values argon2.IDKey would panic on or that are plainly unreasonable.
func parsePHCParams(section string) (phcParams, error) {
	fields := strings.Split(section, ",")
	if len(fields) != 3 {
		return phcParams{}, errors.New("auth: malformed password hash parameters")
	}
	m, err := phcField(fields[0], "m", 32)
	if err != nil {
		return phcParams{}, err
	}
	t, err := phcField(fields[1], "t", 32)
	if err != nil {
		return phcParams{}, err
	}
	p, err := phcField(fields[2], "p", 8)
	if err != nil {
		return phcParams{}, err
	}
	if m == 0 || m > maxArgon2Memory || t == 0 || p == 0 {
		return phcParams{}, errors.New("auth: password hash parameters out of range")
	}
	return phcParams{time: uint32(t), memory: uint32(m), threads: uint8(p)}, nil
}

// phcField parses one name=value field where value is an unsigned
// integer of the given bit size.
func phcField(field, name string, bits int) (uint64, error) {
	prefix, value, ok := strings.Cut(field, "=")
	if !ok || prefix != name {
		return 0, fmt.Errorf("auth: malformed password hash field %q", field)
	}
	n, err := strconv.ParseUint(value, 10, bits)
	if err != nil {
		return 0, fmt.Errorf("auth: malformed password hash field %q", field)
	}
	return n, nil
}
