package store

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// This file implements per-user notification destinations: the address or
// number a reminder is delivered to, and the challenge that proves the
// person adding it can actually receive there. See migrations/core/0005.
//
// The verification rules live here rather than in the API layer because they
// are the security boundary: an unverified destination must never be
// deliverable, however the request that created it arrived.

// ContactCodeTTL is how long a verification code stays usable.
//
// Long enough to switch to a mail client and back, short enough that a code
// glimpsed on a lock screen is not still valid tomorrow.
const ContactCodeTTL = 15 * time.Minute

// ContactMaxAttempts is how many wrong codes a challenge survives.
//
// Six digits is a million possibilities, which is plenty against five
// guesses and nothing at all against unlimited ones — this limit is what
// makes the short code safe, so it is enforced in the store rather than
// left to a caller to remember.
const ContactMaxAttempts = 5

// ContactResendInterval is the minimum gap between sending codes to one
// destination. It stops the resend button being an amplifier for texting a
// stranger — the cost of getting it wrong is paid by whoever owns the
// number, not by the person clicking.
const ContactResendInterval = time.Minute

// ErrTooManyAttempts is returned when a challenge has been exhausted. The
// destination is not verified and a fresh code must be requested.
var ErrTooManyAttempts = errors.New("too many attempts")

// ErrCodeExpired is returned when a challenge has aged out or was never
// started.
var ErrCodeExpired = errors.New("verification code expired")

// ErrCodeIncorrect is returned when a challenge is live but the code is
// wrong. The attempt has already been counted.
var ErrCodeIncorrect = errors.New("verification code incorrect")

// ErrResendTooSoon is returned when a code is requested again before
// ContactResendInterval has passed.
var ErrResendTooSoon = errors.New("a code was just sent; wait before asking for another")

// MaxContactsPerUser caps how many delivery destinations one account may
// hold. Each unverified add sends a verification message, so without a cap a
// member could post an unbounded list of numbers and have the instance text
// every one at the operator's expense. Ten is far more destinations than a
// person needs and still bounds the abuse.
const MaxContactsPerUser = 10

// ErrTooManyContacts is returned when a user is already at MaxContactsPerUser.
var ErrTooManyContacts = errors.New("too many delivery destinations")

// UserContact is one destination reminders can be delivered to.
type UserContact struct {
	// ID identifies the row (an identifier, not a secret).
	ID string `json:"id"`
	// UserID owns the destination.
	UserID int64 `json:"-"`
	// Channel is notify.ChannelEmail or notify.ChannelSMS.
	Channel string `json:"channel"`
	// Address is the email address or E.164 number.
	Address string `json:"address"`
	// Label is an optional name for it ("phone", "work").
	Label string `json:"label"`
	// VerifiedAt is when its owner proved they receive there, nil until
	// then. Nothing is ever delivered to a destination with this unset.
	VerifiedAt *string `json:"verifiedAt,omitempty"`
	// CreatedAt is when it was added (RFC 3339 UTC).
	CreatedAt string `json:"createdAt"`
	// PendingCode reports that a challenge is outstanding, so a UI can show
	// the code entry without being told the code.
	PendingCode bool `json:"pendingCode"`
}

// Verified reports whether this destination may be delivered to.
func (c UserContact) Verified() bool { return c.VerifiedAt != nil }

// CreateUserContact adds an unverified destination.
//
// The address is stored as given; validating its shape belongs to the notify
// package and has already happened at the API boundary. What this enforces
// is uniqueness: the same destination twice would deliver every reminder
// twice, which reads as a bug in the reminder rather than in the settings.
func (s *CoreStore) CreateUserContact(ctx context.Context, c UserContact) (UserContact, error) {
	if c.ID == "" {
		return UserContact{}, errors.New("store: contact id is required")
	}
	if c.UserID <= 0 {
		return UserContact{}, errors.New("store: contact user id is required")
	}
	if c.Channel == "" {
		return UserContact{}, errors.New("store: contact channel is required")
	}
	if c.Address == "" {
		return UserContact{}, errors.New("store: contact address is required")
	}
	c.CreatedAt = s.opts.now()
	// The insert is guarded by a subquery cap so the count-and-insert is one
	// atomic statement: two concurrent adds cannot both slip past a separate
	// count. INSERT ... SELECT ... WHERE inserts zero rows when the user is
	// already at the cap, which shows up as RowsAffected() == 0.
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO user_contacts (id, user_id, channel, address, label, created_at)
		 SELECT ?, ?, ?, ?, ?, ?
		 WHERE (SELECT COUNT(*) FROM user_contacts WHERE user_id = ?) < ?`,
		c.ID, c.UserID, c.Channel, c.Address, c.Label, c.CreatedAt,
		c.UserID, MaxContactsPerUser)
	if err != nil {
		return UserContact{}, fmt.Errorf("store: create contact: %w", classifyConstraint(err))
	}
	if n, err := res.RowsAffected(); err != nil {
		return UserContact{}, fmt.Errorf("store: create contact: %w", err)
	} else if n == 0 {
		return UserContact{}, ErrTooManyContacts
	}
	c.VerifiedAt = nil
	return c, nil
}

// ListUserContacts returns one user's destinations, oldest first.
func (s *CoreStore) ListUserContacts(ctx context.Context, userID int64) ([]UserContact, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, channel, address, label, verified_at, created_at,
		        code_hash, code_expires_at
		 FROM user_contacts WHERE user_id = ? ORDER BY created_at, id`, userID)
	if err != nil {
		return nil, fmt.Errorf("store: list contacts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	contacts := []UserContact{}
	now := s.opts.clock().UTC()
	for rows.Next() {
		var c UserContact
		var verifiedAt, codeExpires sql.NullString
		var codeHash string
		if err := rows.Scan(&c.ID, &c.UserID, &c.Channel, &c.Address, &c.Label,
			&verifiedAt, &c.CreatedAt, &codeHash, &codeExpires); err != nil {
			return nil, fmt.Errorf("store: list contacts: %w", err)
		}
		if verifiedAt.Valid {
			v := verifiedAt.String
			c.VerifiedAt = &v
		}
		c.PendingCode = codeHash != "" && !expired(codeExpires, now)
		contacts = append(contacts, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list contacts: %w", err)
	}
	return contacts, nil
}

// ListVerifiedContacts returns only the destinations that may be delivered
// to. The dispatcher calls this and nothing else, so there is no path where
// forgetting to check VerifiedAt sends to an unverified address.
func (s *CoreStore) ListVerifiedContacts(ctx context.Context, userID int64) ([]UserContact, error) {
	all, err := s.ListUserContacts(ctx, userID)
	if err != nil {
		return nil, err
	}
	verified := make([]UserContact, 0, len(all))
	for _, c := range all {
		if c.Verified() {
			verified = append(verified, c)
		}
	}
	return verified, nil
}

// GetUserContact returns one destination owned by userID.
//
// Scoping the read to the owner is what stops one user's id guess reaching
// another user's row; every caller here goes through it for that reason.
func (s *CoreStore) GetUserContact(ctx context.Context, userID int64, id string) (UserContact, error) {
	var c UserContact
	var verifiedAt, codeExpires sql.NullString
	var codeHash string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, channel, address, label, verified_at, created_at,
		        code_hash, code_expires_at
		 FROM user_contacts WHERE id = ? AND user_id = ?`, id, userID).
		Scan(&c.ID, &c.UserID, &c.Channel, &c.Address, &c.Label,
			&verifiedAt, &c.CreatedAt, &codeHash, &codeExpires)
	if errors.Is(err, sql.ErrNoRows) {
		return UserContact{}, fmt.Errorf("store: contact %q: %w", id, ErrNotFound)
	}
	if err != nil {
		return UserContact{}, fmt.Errorf("store: get contact: %w", err)
	}
	if verifiedAt.Valid {
		v := verifiedAt.String
		c.VerifiedAt = &v
	}
	c.PendingCode = codeHash != "" && !expired(codeExpires, s.opts.clock().UTC())
	return c, nil
}

// DeleteUserContact removes a destination. Returns ErrNotFound when userID
// owns no such row.
func (s *CoreStore) DeleteUserContact(ctx context.Context, userID int64, id string) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM user_contacts WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return fmt.Errorf("store: delete contact: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: delete contact: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("store: contact %q: %w", id, ErrNotFound)
	}
	return nil
}

// StartContactChallenge stores a fresh code hash against a destination and
// returns the contact it belongs to, so the caller knows where to send it.
//
// It refuses a resend inside ContactResendInterval: the send goes to
// somebody who may not have asked for it, and the button that triggers it is
// on a page anyone signed in can reach.
func (s *CoreStore) StartContactChallenge(ctx context.Context, userID int64, id, codeHash string) (UserContact, error) {
	if codeHash == "" {
		return UserContact{}, errors.New("store: contact code hash is required")
	}
	now := s.opts.clock().UTC()
	cutoff := now.Add(-ContactResendInterval).Format(time.RFC3339)

	// One conditional UPDATE does the resend-throttle check and the write
	// together, so it cannot be raced: previously this read code_sent_at,
	// compared it in Go, then wrote in a separate statement, and concurrent
	// callers all read the same stale stamp and every one got through. The
	// WHERE only matches an unverified row whose last send is old enough;
	// RFC 3339 UTC timestamps compare correctly as strings.
	res, err := s.db.ExecContext(ctx,
		`UPDATE user_contacts
		 SET code_hash = ?, code_expires_at = ?, code_attempts = 0, code_sent_at = ?
		 WHERE id = ? AND user_id = ? AND verified_at IS NULL
		   AND (code_sent_at IS NULL OR code_sent_at <= ?)`,
		codeHash, now.Add(ContactCodeTTL).Format(time.RFC3339), now.Format(time.RFC3339),
		id, userID, cutoff)
	if err != nil {
		return UserContact{}, fmt.Errorf("store: contact challenge: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return UserContact{}, fmt.Errorf("store: contact challenge: %w", err)
	}
	if n == 1 {
		contact, err := s.GetUserContact(ctx, userID, id)
		if err != nil {
			return UserContact{}, err
		}
		contact.PendingCode = true
		return contact, nil
	}

	// No row updated: distinguish the reasons so the caller can answer well.
	contact, err := s.GetUserContact(ctx, userID, id)
	if err != nil {
		return UserContact{}, err // ErrNotFound
	}
	if contact.Verified() {
		return UserContact{}, errors.New("store: contact is already verified")
	}
	return UserContact{}, ErrResendTooSoon
}

// VerifyUserContact checks a code and marks the destination verified.
//
// A wrong code is counted before it is rejected, so the attempt limit cannot
// be sidestepped by abandoning the request. An exhausted or expired
// challenge clears itself: the next step is a new code, not another guess.
func (s *CoreStore) VerifyUserContact(ctx context.Context, userID int64, id, codeHash string) (UserContact, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return UserContact{}, fmt.Errorf("store: verify contact: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var storedHash string
	var expiresAt sql.NullString
	var attempts int
	var verifiedAt sql.NullString
	err = tx.QueryRowContext(ctx,
		`SELECT code_hash, code_expires_at, code_attempts, verified_at
		 FROM user_contacts WHERE id = ? AND user_id = ?`, id, userID).
		Scan(&storedHash, &expiresAt, &attempts, &verifiedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return UserContact{}, fmt.Errorf("store: contact %q: %w", id, ErrNotFound)
	}
	if err != nil {
		return UserContact{}, fmt.Errorf("store: verify contact: %w", err)
	}
	if verifiedAt.Valid {
		// Already done. Saying so plainly beats an error that reads like the
		// code was wrong.
		return s.GetUserContact(ctx, userID, id)
	}

	now := s.opts.clock().UTC()
	if storedHash == "" || expired(expiresAt, now) {
		if err := clearChallenge(ctx, tx, userID, id); err != nil {
			return UserContact{}, err
		}
		if err := tx.Commit(); err != nil {
			return UserContact{}, fmt.Errorf("store: verify contact: commit: %w", err)
		}
		return UserContact{}, ErrCodeExpired
	}
	if attempts >= ContactMaxAttempts {
		if err := clearChallenge(ctx, tx, userID, id); err != nil {
			return UserContact{}, err
		}
		if err := tx.Commit(); err != nil {
			return UserContact{}, fmt.Errorf("store: verify contact: commit: %w", err)
		}
		return UserContact{}, ErrTooManyAttempts
	}

	// Constant-time because the stored value is a hash of a six-digit
	// secret: a comparison that returns early on the first differing byte
	// leaks how much of a guess was right, and six digits is a small enough
	// space for that to matter.
	if subtle.ConstantTimeCompare([]byte(storedHash), []byte(codeHash)) != 1 {
		if _, err := tx.ExecContext(ctx,
			`UPDATE user_contacts SET code_attempts = code_attempts + 1 WHERE id = ? AND user_id = ?`,
			id, userID); err != nil {
			return UserContact{}, fmt.Errorf("store: verify contact: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return UserContact{}, fmt.Errorf("store: verify contact: commit: %w", err)
		}
		return UserContact{}, ErrCodeIncorrect
	}

	stamp := now.Format(time.RFC3339)
	if _, err := tx.ExecContext(ctx,
		`UPDATE user_contacts
		 SET verified_at = ?, code_hash = '', code_expires_at = NULL, code_attempts = 0
		 WHERE id = ? AND user_id = ?`, stamp, id, userID); err != nil {
		return UserContact{}, fmt.Errorf("store: verify contact: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return UserContact{}, fmt.Errorf("store: verify contact: commit: %w", err)
	}
	return s.GetUserContact(ctx, userID, id)
}

// clearChallenge wipes a spent or expired challenge.
func clearChallenge(ctx context.Context, tx *sql.Tx, userID int64, id string) error {
	if _, err := tx.ExecContext(ctx,
		`UPDATE user_contacts
		 SET code_hash = '', code_expires_at = NULL, code_attempts = 0
		 WHERE id = ? AND user_id = ?`, id, userID); err != nil {
		return fmt.Errorf("store: clear contact challenge: %w", err)
	}
	return nil
}

// expired reports whether a stored RFC 3339 deadline has passed. An
// unparseable or absent deadline counts as expired: the safe reading of a
// value we cannot interpret is that the challenge is not live.
func expired(deadline sql.NullString, now time.Time) bool {
	if !deadline.Valid {
		return true
	}
	at, err := time.Parse(time.RFC3339, deadline.String)
	if err != nil {
		return true
	}
	return now.After(at)
}
