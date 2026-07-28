package api

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/bgrewell/donezo/internal/store"
)

// This file implements the space lifecycle endpoints: create, partial
// update, archive, and unarchive. Space ids are server-generated — a slug
// of the name plus a random suffix — because they become database file
// names on disk.

// maxSlugLen bounds the name-derived fragment of a generated space id;
// with the dash and 8-hex suffix the total stays well under the 64-char
// space id limit.
const maxSlugLen = 40

// slugifyName reduces a space name to a lowercase a-z0-9 slug: every run
// of other characters collapses to a single dash, edges are trimmed, and
// a name with nothing usable falls back to "space".
func slugifyName(name string) string {
	var b strings.Builder
	dashPending := false
	for _, r := range strings.ToLower(name) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			if dashPending && b.Len() > 0 {
				b.WriteByte('-')
			}
			dashPending = false
			b.WriteRune(r)
		default:
			dashPending = true
		}
	}
	slug := b.String()
	if len(slug) > maxSlugLen {
		slug = strings.TrimRight(slug[:maxSlugLen], "-")
	}
	if slug == "" {
		return "space"
	}
	return slug
}

// newSpaceID composes a space id from the name's slug and a random 8-hex
// suffix (the same shape as the frontend's newId()), validated with the
// store's file-safe slug rules.
func newSpaceID(name string) (string, error) {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("api: generate space id: %w", err)
	}
	id := slugifyName(name) + "-" + hex.EncodeToString(buf[:])
	if err := store.ValidateSpaceID(id); err != nil {
		return "", err
	}
	return id, nil
}

// handleCreateSpace registers a new space for the requester: {name,
// color} in, {"space": ...} out. The space's database file is created
// immediately, so the space is usable the moment it is visible; if that
// fails the registry row is rolled back.
func (s *Server) handleCreateSpace(w http.ResponseWriter, r *http.Request) {
	user, ok := userFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	if !s.decodeBody(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if err := firstError(
		required("name", name),
		oneOf("color", req.Color, projectColors),
	); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// The 8-hex suffix makes collisions vanishingly rare; the retry loop
	// makes them invisible when they do happen.
	for attempt := 0; attempt < 3; attempt++ {
		id, err := newSpaceID(name)
		if err != nil {
			s.logger.Printf("create space: %v", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		sp, err := s.core.CreateSpaceAtEnd(r.Context(), store.Space{
			ID: id, UserID: user.ID, Name: name, Color: req.Color,
		})
		if errors.Is(err, store.ErrDuplicateID) {
			continue
		}
		if err != nil {
			s.logger.Printf("create space: %v", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if err := s.spaces.EnsureSpace(r.Context(), id); err != nil {
			s.logger.Printf("create space %s: ensure database: %v", id, err)
			if derr := s.core.DeleteSpace(r.Context(), id); derr != nil {
				s.logger.Printf("create space %s: roll back registry row: %v", id, derr)
			}
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]store.Space{"space": sp})
		return
	}
	s.logger.Printf("create space: could not allocate an id after 3 attempts")
	writeError(w, http.StatusInternalServerError, "internal error")
}

// spacePatch is the PATCH /api/spaces/{id} body.
type spacePatch struct {
	Name     *string `json:"name"`
	Color    *string `json:"color"`
	Position *int    `json:"position"`
}

// validate checks every present field.
func (p *spacePatch) validate() error {
	if p.Name != nil {
		*p.Name = strings.TrimSpace(*p.Name)
		if err := required("name", *p.Name); err != nil {
			return err
		}
	}
	if p.Color != nil {
		if err := oneOf("color", *p.Color, projectColors); err != nil {
			return err
		}
	}
	if p.Position != nil && *p.Position < 0 {
		return errors.New("position must not be negative")
	}
	return nil
}

// apply copies the present fields onto the stored space.
func (p *spacePatch) apply(cur *store.Space) error {
	if p.Name != nil {
		cur.Name = *p.Name
	}
	if p.Color != nil {
		cur.Color = *p.Color
	}
	if p.Position != nil {
		cur.Position = *p.Position
	}
	return nil
}

// handlePatchSpace applies a partial update — name, color, position — to
// an owned space.
func (s *Server) handlePatchSpace(w http.ResponseWriter, r *http.Request) {
	sp, ok := s.ownedSpace(w, r)
	if !ok {
		return
	}
	var p spacePatch
	if !s.decodeBody(w, r, &p) {
		return
	}
	if err := p.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := s.core.PatchSpace(r.Context(), sp.ID, p.apply)
	if err != nil {
		s.writeStoreError(w, "space", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]store.Space{"space": updated})
}

// handleArchiveSpace stamps an owned space's archivedAt.
func (s *Server) handleArchiveSpace(w http.ResponseWriter, r *http.Request) {
	s.setSpaceArchived(w, r, true)
}

// handleUnarchiveSpace clears an owned space's archivedAt.
func (s *Server) handleUnarchiveSpace(w http.ResponseWriter, r *http.Request) {
	s.setSpaceArchived(w, r, false)
}

// setSpaceArchived implements archive and unarchive: both answer the
// stored space, and both are idempotent from the client's point of view.
func (s *Server) setSpaceArchived(w http.ResponseWriter, r *http.Request, archived bool) {
	sp, ok := s.ownedSpace(w, r)
	if !ok {
		return
	}
	updated, err := s.core.SetSpaceArchived(r.Context(), sp.ID, archived)
	if err != nil {
		s.writeStoreError(w, "space", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]store.Space{"space": updated})
}
