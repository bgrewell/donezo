package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// This file implements invite-code persistence: minting, the admin
// listing, revocation, and the atomic claim that turns a code into a
// member account. The plaintext code never touches the database — only
// its SHA-256 (computed by the auth package) is stored.

// Invite is a row in core.db's invites table.
type Invite struct {
	// ID identifies the invite row (an identifier, not a secret).
	ID string
	// CodeHash is the hex-encoded SHA-256 of the plaintext code, the
	// lookup key for claims. Never serialized.
	CodeHash string `json:"-"`
	// CodePrefix is the first characters of the rendered code, kept so
	// the admin list can identify an invite without exposing it.
	CodePrefix string
	// CreatedBy is the admin who minted the invite.
	CreatedBy int64
	// CreatedAt is when the invite was minted (RFC 3339 UTC).
	CreatedAt string
	// ExpiresAt is the absolute claim deadline (RFC 3339 UTC).
	ExpiresAt string
	// UsedBy is the user created by claiming this invite, nil while
	// unclaimed.
	UsedBy *int64
	// UsedAt is when the invite was claimed, nil while unclaimed.
	UsedAt *string
	// RevokedAt is when an admin revoked the invite, nil if never
	// revoked.
	RevokedAt *string
	// Email is the address the invite was sent to, nil for a code that
	// was generated without one. A label for the admin, never used to
	// claim the invite.
	Email *string
}

// InviteListing is one row of the admin invite list: invite metadata
// joined with the creator's — and, once claimed, the claimant's —
// username. The code hash is deliberately not part of the listing.
type InviteListing struct {
	// ID identifies the invite row.
	ID string
	// CodePrefix is the identifying fragment of the rendered code.
	CodePrefix string
	// CreatedBy is the minting admin's user id.
	CreatedBy int64
	// CreatorUsername is the minting admin's username.
	CreatorUsername string
	// CreatedAt is when the invite was minted (RFC 3339 UTC).
	CreatedAt string
	// ExpiresAt is the absolute claim deadline (RFC 3339 UTC).
	ExpiresAt string
	// UsedBy is the claimant's user id, nil while unclaimed.
	UsedBy *int64
	// UsedByUsername is the claimant's username, nil while unclaimed.
	UsedByUsername *string
	// UsedAt is when the invite was claimed, nil while unclaimed.
	UsedAt *string
	// RevokedAt is when the invite was revoked, nil if never revoked.
	RevokedAt *string
	// Email is the address the invite was sent to, nil if it was only
	// minted as a code.
	Email *string
}

// ErrInviteInvalid is returned by RegisterInvitedUser for every unusable
// invite code — unknown, already used, revoked, or expired — as one
// sentinel, so callers cannot tell those states apart and neither can
// their clients (no invite-state oracle).
var ErrInviteInvalid = errors.New("invite code invalid")

// ErrUsernameTaken is returned by RegisterInvitedUser when the requested
// username already exists. Unlike invite state this is deliberately
// distinguishable: the registrant has to pick another name.
var ErrUsernameTaken = errors.New("username taken")

// ErrEmailTaken is returned when creating an account whose email already
// belongs to another account. Emails are unique so a reset request resolves
// to at most one account; a registrant who collides has to use another.
var ErrEmailTaken = errors.New("email taken")

// userUniqueConflict maps a UNIQUE-constraint failure on the users table to
// the field that collided, so callers can tell "username taken" from "email
// taken". SQLite names the offending index/column in the driver message, so
// that is what is inspected; a unique failure that mentions the email is an
// email collision, and anything else is attributed to the username (the
// original, always-present constraint). A non-uniqueness error passes through.
func userUniqueConflict(err error) error {
	if !errors.Is(classifyConstraint(err), ErrDuplicateID) {
		return err
	}
	if strings.Contains(strings.ToLower(err.Error()), "email") {
		return ErrEmailTaken
	}
	return ErrUsernameTaken
}

// CreateInvite inserts an invite from inv's identity fields (ID,
// CodeHash, CodePrefix, CreatedBy), stamping CreatedAt from the store
// clock and ExpiresAt ttl later, and returns the stored row.
func (s *CoreStore) CreateInvite(ctx context.Context, inv Invite, ttl time.Duration) (Invite, error) {
	if inv.ID == "" {
		return Invite{}, errors.New("store: invite id is required")
	}
	if inv.CodeHash == "" {
		return Invite{}, errors.New("store: invite code hash is required")
	}
	if inv.CodePrefix == "" {
		return Invite{}, errors.New("store: invite code prefix is required")
	}
	if inv.CreatedBy <= 0 {
		return Invite{}, errors.New("store: invite creator is required")
	}
	if ttl <= 0 {
		return Invite{}, fmt.Errorf("store: invite ttl must be positive, got %s", ttl)
	}
	now := s.opts.clock().UTC()
	inv.CreatedAt = now.Format(time.RFC3339)
	inv.ExpiresAt = now.Add(ttl).Format(time.RFC3339)
	inv.UsedBy, inv.UsedAt, inv.RevokedAt = nil, nil, nil
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO invites (id, code_hash, code_prefix, created_by, created_at, expires_at, used_by, used_at, revoked_at, email)
		 VALUES (?, ?, ?, ?, ?, ?, NULL, NULL, NULL, ?)`,
		inv.ID, inv.CodeHash, inv.CodePrefix, inv.CreatedBy, inv.CreatedAt, inv.ExpiresAt, inv.Email); err != nil {
		return Invite{}, fmt.Errorf("store: create invite %q: %w", inv.ID, classifyConstraint(err))
	}
	return inv, nil
}

// ListInvites returns every invite joined with its creator's username
// and — once claimed — the claimant's, newest first. This is the
// single-instance admin view: all invites, whoever minted them.
func (s *CoreStore) ListInvites(ctx context.Context) ([]InviteListing, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT i.id, i.code_prefix, i.created_by, c.username, i.created_at, i.expires_at,
		        i.used_by, u.username, i.used_at, i.revoked_at, i.email
		 FROM invites i
		 JOIN users c ON c.id = i.created_by
		 LEFT JOIN users u ON u.id = i.used_by
		 ORDER BY i.created_at DESC, i.id`)
	if err != nil {
		return nil, fmt.Errorf("store: list invites: %w", err)
	}
	defer closeQuietly(rows)
	listings := []InviteListing{}
	for rows.Next() {
		var l InviteListing
		if err := rows.Scan(&l.ID, &l.CodePrefix, &l.CreatedBy, &l.CreatorUsername, &l.CreatedAt,
			&l.ExpiresAt, &l.UsedBy, &l.UsedByUsername, &l.UsedAt, &l.RevokedAt, &l.Email); err != nil {
			return nil, fmt.Errorf("store: scan invite: %w", err)
		}
		listings = append(listings, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list invites: %w", err)
	}
	return listings, nil
}

// RevokeInvite stamps the invite's revoked_at from the store clock. It
// is idempotent: revoking an already-revoked invite keeps the original
// stamp and still succeeds. Returns ErrNotFound if the id does not
// exist.
func (s *CoreStore) RevokeInvite(ctx context.Context, id string) error {
	if id == "" {
		return errors.New("store: invite id is required")
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE invites SET revoked_at = COALESCE(revoked_at, ?) WHERE id = ?`, s.opts.now(), id)
	if err != nil {
		return fmt.Errorf("store: revoke invite %q: %w", id, err)
	}
	return notFoundIfZero(res, "invite", id)
}

// usableInviteGuard is the SQL predicate deciding — inside the claim
// UPDATE itself — whether an invite can still be claimed: never used,
// never revoked, not yet expired. Because the check and the write are
// one statement, SQLite executes them in isolation and two concurrent
// claims of the same code cannot both pass: exactly one wins, mirroring
// SetupOwner's first-run guard. RFC 3339 UTC timestamps order
// lexicographically, so string comparison is time comparison.
const usableInviteGuard = `used_at IS NULL AND revoked_at IS NULL AND expires_at > ?`

// RegisterInvitedUser atomically redeems the invite stored under
// codeHash: it claims the invite, creates u as a member with the given
// password hash, and registers sp (position 0 — the new member's first
// space) in one transaction, so a failure at any step leaves nothing
// behind, including the claim. The space's content database file is NOT
// created here; the caller ensures it afterwards like any other space
// creation.
//
// The claim is a single guarded UPDATE, so concurrent registrations with
// the same code are TOCTOU-proof: exactly one wins. It also runs before
// the user insert, so a request with an unusable code learns nothing
// about usernames. Unusable codes of every kind return ErrInviteInvalid
// — one sentinel, no state oracle — and a taken username returns
// ErrUsernameTaken.
func (s *CoreStore) RegisterInvitedUser(ctx context.Context, codeHash string, u User, sp Space) (User, Space, error) {
	if codeHash == "" {
		return User{}, Space{}, errors.New("store: invite code hash is required")
	}
	if u.Username == "" {
		return User{}, Space{}, errors.New("store: username is required")
	}
	if u.DisplayName == "" {
		return User{}, Space{}, errors.New("store: display name is required")
	}
	if u.PasswordHash == "" {
		return User{}, Space{}, errors.New("store: password hash is required")
	}
	if err := ValidateSpaceID(sp.ID); err != nil {
		return User{}, Space{}, err
	}
	if sp.Name == "" {
		return User{}, Space{}, errors.New("store: space name is required")
	}
	now := s.opts.now()
	u.Role = RoleMember
	u.CreatedAt = now
	sp.Position = 0
	sp.ArchivedAt = nil
	sp.CreatedAt = now

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, Space{}, fmt.Errorf("store: register %q: begin: %w", u.Username, err)
	}
	defer rollbackQuietly(tx)

	// Claim the invite (used_by follows once the user row exists).
	res, err := tx.ExecContext(ctx,
		`UPDATE invites SET used_at = ? WHERE code_hash = ? AND `+usableInviteGuard,
		now, codeHash, now)
	if err != nil {
		return User{}, Space{}, fmt.Errorf("store: register %q: claim invite: %w", u.Username, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return User{}, Space{}, fmt.Errorf("store: register %q: claim invite: %w", u.Username, err)
	}
	if n == 0 {
		return User{}, Space{}, fmt.Errorf("store: register %q: %w", u.Username, ErrInviteInvalid)
	}

	// Create the member account.
	res, err = tx.ExecContext(ctx,
		`INSERT INTO users (username, display_name, role, password_hash, created_at, email) VALUES (?, ?, ?, ?, ?, ?)`,
		u.Username, u.DisplayName, u.Role, u.PasswordHash, u.CreatedAt, u.Email)
	if err != nil {
		if conflict := userUniqueConflict(err); errors.Is(conflict, ErrUsernameTaken) || errors.Is(conflict, ErrEmailTaken) {
			return User{}, Space{}, fmt.Errorf("store: register %q: %w", u.Username, conflict)
		}
		return User{}, Space{}, fmt.Errorf("store: register %q: create user: %w", u.Username, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return User{}, Space{}, fmt.Errorf("store: register %q: create user: %w", u.Username, err)
	}
	u.ID = id
	sp.UserID = id

	// Record who claimed the invite, now that the row exists.
	if _, err := tx.ExecContext(ctx,
		`UPDATE invites SET used_by = ? WHERE code_hash = ?`, u.ID, codeHash); err != nil {
		return User{}, Space{}, fmt.Errorf("store: register %q: record claimant: %w", u.Username, err)
	}

	// The member's first space.
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO spaces (id, user_id, name, color, position, archived_at, created_at)
		 VALUES (?, ?, ?, ?, 0, NULL, ?)`,
		sp.ID, sp.UserID, sp.Name, sp.Color, sp.CreatedAt); err != nil {
		return User{}, Space{}, fmt.Errorf("store: register %q: create space %q: %w",
			u.Username, sp.ID, classifyConstraint(err))
	}

	if err := tx.Commit(); err != nil {
		return User{}, Space{}, fmt.Errorf("store: register %q: commit: %w", u.Username, err)
	}
	return u, sp, nil
}

// UnregisterInvitedUser reverses a committed RegisterInvitedUser in one
// transaction: it clears the invite's claim (used_by / used_at), removes
// the member's first space row, and removes the member — all or nothing.
// It exists as compensation for a failed registration: when the space's
// content database cannot be created after RegisterInvitedUser commits,
// the caller unwinds the half-made account and returns the code, so a
// server-side fault does not burn the invite.
//
// Atomicity is the invariant, not a convenience: releasing the claim and
// deleting the account must not be separable, because an invite that
// becomes claimable again while its original account survives would let
// one code mint two accounts. If the transaction fails, everything —
// including the claim — stays put, which errs in the safe direction (a
// burned code and an orphaned account, never a double claim). The claim
// is only cleared where used_by matches userID, and deleting the user
// while any invite still references them trips the foreign key, so a
// mismatched call cannot release somebody else's claim.
func (s *CoreStore) UnregisterInvitedUser(ctx context.Context, codeHash string, userID int64, spaceID string) error {
	if codeHash == "" {
		return errors.New("store: invite code hash is required")
	}
	if userID <= 0 {
		return errors.New("store: user id is required")
	}
	if spaceID == "" {
		return errors.New("store: space id is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: unregister user %d: begin: %w", userID, err)
	}
	defer rollbackQuietly(tx)
	if _, err := tx.ExecContext(ctx,
		`UPDATE invites SET used_by = NULL, used_at = NULL WHERE code_hash = ? AND used_by = ?`,
		codeHash, userID); err != nil {
		return fmt.Errorf("store: unregister user %d: release invite: %w", userID, err)
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM spaces WHERE id = ? AND user_id = ?`, spaceID, userID); err != nil {
		return fmt.Errorf("store: unregister user %d: delete space %q: %w", userID, spaceID, err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, userID); err != nil {
		return fmt.Errorf("store: unregister user %d: delete user: %w", userID, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: unregister user %d: commit: %w", userID, err)
	}
	return nil
}
