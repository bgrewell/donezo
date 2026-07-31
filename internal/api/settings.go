package api

import (
	"errors"
	"net/http"

	"github.com/bgrewell/donezo/internal/store"
)

// This file serves a user's own preferences. Settings are account-wide, not
// per-space: they describe how the app behaves for this user everywhere, so
// they live in core.db alongside the user rather than in any space database.
//
// Both routes act on the authenticated user only — there is no user id in
// the path, so one account can never read or write another's preferences.

// settingsPatch is the PATCH /api/settings body. Fields are pointers so an
// omitted field is left alone; an explicit empty string clears the stored
// preference, letting it follow the current default again.
type settingsPatch struct {
	Theme    *string `json:"theme"`
	Font     *string `json:"font"`
	FontSize *string `json:"fontSize"`
}

// validate checks each supplied preference against the union the web UI
// offers, so a stored value can always be rendered.
func (p settingsPatch) validate() error {
	return firstError(
		optionalOneOf("theme", p.Theme, themeIDs),
		optionalOneOf("font", p.Font, fontIDs),
		optionalOneOf("fontSize", p.FontSize, fontSizeIDs),
	)
}

// apply writes the supplied fields onto the stored settings.
func (p settingsPatch) apply(s *store.UserSettings) error {
	if p.Theme != nil {
		s.Theme = *p.Theme
	}
	if p.Font != nil {
		s.Font = *p.Font
	}
	if p.FontSize != nil {
		s.FontSize = *p.FontSize
	}
	return nil
}

// handleGetSettings returns the authenticated user's preferences. A user who
// has never saved one gets an empty object rather than a 404 — having no
// stored preferences is a normal state, not a missing resource.
func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	user, ok := userFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	settings, err := s.core.GetUserSettings(r.Context(), user.ID)
	if err != nil {
		s.logger.Printf("get user settings: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]store.UserSettings{"settings": settings})
}

// handlePatchSettings updates the supplied preferences and returns the full
// stored set, so a caller never has to merge the response itself.
func (s *Server) handlePatchSettings(w http.ResponseWriter, r *http.Request) {
	user, ok := userFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var p settingsPatch
	if !s.decodeBody(w, r, &p) {
		return
	}
	if err := p.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	settings, err := s.core.PatchUserSettings(r.Context(), user.ID, p.apply)
	switch {
	case errors.Is(err, store.ErrNotFound):
		// The request authenticated, but the account no longer exists —
		// deleted mid-session. The identity is what is stale, not the
		// request, so answer like any other invalid credential.
		writeError(w, http.StatusUnauthorized, "authentication required")
	case err != nil:
		s.logger.Printf("patch user settings: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	default:
		writeJSON(w, http.StatusOK, map[string]store.UserSettings{"settings": settings})
	}
}
