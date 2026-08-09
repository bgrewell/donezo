package api

import (
	"net/http"

	"github.com/bgrewell/donezo/internal/store"
)

// This file serves the trash (#16). Deleting anywhere else in the API now
// moves a row here rather than removing it; these routes are what makes that
// visible and reversible.
//
// Restore and purge name one row but act on its whole delete batch, so a
// project comes back with the content it took — see store/trash.go for why a
// batch exists at all.

// trashEntities are the entity names the restore and purge paths accept,
// mirroring store's constants. Anything else is a 400 naming these, rather
// than a 404 that reads like the item is missing.
var trashEntities = []string{
	store.TrashProject, store.TrashActivity, store.TrashTask,
	store.TrashNote, store.TrashReminder, store.TrashInbox,
}

// handleListTrash returns everything currently trashed in the space.
func (s *Server) handleListTrash(w http.ResponseWriter, r *http.Request) {
	sp, ok := s.ownedSpace(w, r)
	if !ok {
		return
	}
	items, err := s.spaces.ListTrash(r.Context(), sp.ID)
	if err != nil {
		s.logger.Printf("list trash: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"trash": items})
}

// handleRestoreTrash restores one trashed row and its batch.
func (s *Server) handleRestoreTrash(w http.ResponseWriter, r *http.Request) {
	sp, entity, ok := s.trashTarget(w, r)
	if !ok {
		return
	}
	restored, err := s.spaces.RestoreItem(r.Context(), sp.ID, entity, r.PathValue("tid"))
	if err != nil {
		s.writeStoreError(w, "trashed "+entity, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"restored": restored})
}

// handlePurgeTrash permanently removes one trashed row and its batch.
func (s *Server) handlePurgeTrash(w http.ResponseWriter, r *http.Request) {
	sp, entity, ok := s.trashTarget(w, r)
	if !ok {
		return
	}
	purged, err := s.spaces.PurgeItem(r.Context(), sp.ID, entity, r.PathValue("tid"))
	if err != nil {
		s.writeStoreError(w, "trashed "+entity, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"purged": purged})
}

// handleEmptyTrash permanently removes everything in the trash.
func (s *Server) handleEmptyTrash(w http.ResponseWriter, r *http.Request) {
	sp, ok := s.ownedLiveSpace(w, r)
	if !ok {
		return
	}
	purged, err := s.spaces.EmptyTrash(r.Context(), sp.ID)
	if err != nil {
		s.logger.Printf("empty trash: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"purged": purged})
}

// trashTarget resolves the space and validates the entity segment for the
// restore and purge routes. Both are writes, so both require a live space.
func (s *Server) trashTarget(w http.ResponseWriter, r *http.Request) (store.Space, string, bool) {
	sp, ok := s.ownedLiveSpace(w, r)
	if !ok {
		return store.Space{}, "", false
	}
	entity := r.PathValue("entity")
	if err := oneOf("entity", entity, trashEntities); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return store.Space{}, "", false
	}
	return sp, entity, true
}
