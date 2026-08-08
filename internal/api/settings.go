package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/bgrewell/donezo/internal/llm"
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

	// Onboarding progress. These do not behave like the appearance fields
	// above: see apply for why they only ever move one way.
	Welcomed       *bool    `json:"welcomed"`
	TourDone       *bool    `json:"tourDone"`
	DismissedHints []string `json:"dismissedHints"`
	// ResetOnboarding clears all three, for the deliberate "show me the
	// first-run experience again" action. It exists so that a reset is an
	// explicit intent rather than something a stale client can do by
	// accident — see apply.
	ResetOnboarding *bool `json:"resetOnboarding"`

	// Prompts sets this user's own wording for language-model prompts, keyed
	// by prompt id. An empty value clears that key, so the operator's or the
	// built-in wording applies again. Keys not mentioned are left alone.
	Prompts map[string]string `json:"prompts"`
}

// maxPromptBodyRunes bounds one user-supplied prompt body. The instruction is
// sent on every polish call, so an unbounded one is a standing token cost as
// well as an unbounded settings document.
const maxPromptBodyRunes = 4000

// maxDismissedHints bounds the stored hint list. Hint ids come from the
// client, so without a ceiling a buggy or hostile caller could grow one
// user's settings document without limit.
const maxDismissedHints = 128

// maxHintIDRunes bounds one hint id.
const maxHintIDRunes = 64

// validate checks each supplied preference against the union the web UI
// offers, so a stored value can always be rendered.
func (p settingsPatch) validate() error {
	return firstError(
		optionalOneOf("theme", p.Theme, themeIDs),
		optionalOneOf("font", p.Font, fontIDs),
		optionalOneOf("fontSize", p.FontSize, fontSizeIDs),
		p.validateHints(),
		p.validatePrompts(),
	)
}

// validatePrompts checks that each key names a prompt this build serves and
// that each body is within size. An unknown id is refused rather than stored:
// a typo would otherwise be saved and silently never used, which looks exactly
// like the setting not working.
func (p settingsPatch) validatePrompts() error {
	for id, body := range p.Prompts {
		if _, known := llm.BuiltInPromptSet().ByID(id); !known {
			return fmt.Errorf("unknown prompt id %q", id)
		}
		if utf8.RuneCountInString(body) > maxPromptBodyRunes {
			return fmt.Errorf("prompt %q must be at most %d characters", id, maxPromptBodyRunes)
		}
	}
	return nil
}

// validateHints bounds the hint ids a caller may add in one patch. The stored
// total is capped separately in apply, since a union can exceed this on its
// own.
func (p settingsPatch) validateHints() error {
	if len(p.DismissedHints) > maxDismissedHints {
		return fmt.Errorf("dismissedHints must hold at most %d ids", maxDismissedHints)
	}
	for _, id := range p.DismissedHints {
		if id == "" {
			return errors.New("dismissedHints must not contain an empty id")
		}
		if utf8.RuneCountInString(id) > maxHintIDRunes {
			return fmt.Errorf("each dismissed hint id must be at most %d characters", maxHintIDRunes)
		}
	}
	return nil
}

// apply writes the supplied fields onto the stored settings.
//
// The appearance fields are last-write-wins: they are preferences, and the
// most recent deliberate choice should stand.
//
// Onboarding progress is not a preference but a record of something that
// already happened, so it is merged **one way** — flags only move false to
// true, and dismissed hints only accumulate. Without that, a browser that has
// never seen the welcome would push its empty state over a server that knows
// better and resurrect the dialog everywhere. The client guards against this
// too by not writing until it has hydrated, but the rule belongs here as
// well: the store is reachable by anything holding a session, including an
// agent over MCP, and a monotonic field cannot be walked backwards by a
// caller that simply does not know any better.
//
// ResetOnboarding is the deliberate exception, and runs last so that a patch
// combining it with progress flags still ends up reset rather than in a state
// that depends on field order.
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
	if p.Welcomed != nil && *p.Welcomed {
		s.Welcomed = true
	}
	if p.TourDone != nil && *p.TourDone {
		s.TourDone = true
	}
	if len(p.DismissedHints) > 0 {
		s.DismissedHints = unionHints(s.DismissedHints, p.DismissedHints)
	}
	if p.ResetOnboarding != nil && *p.ResetOnboarding {
		s.Welcomed = false
		s.TourDone = false
		s.DismissedHints = nil
	}
	for id, body := range p.Prompts {
		trimmed := strings.TrimSpace(body)
		if trimmed == "" {
			// Clearing is how a user goes back to the shipped wording, so an
			// empty value deletes the key rather than storing a blank
			// instruction that would be sent as the whole body.
			delete(s.Prompts, id)
			continue
		}
		if s.Prompts == nil {
			s.Prompts = make(map[string]string, len(p.Prompts))
		}
		s.Prompts[id] = trimmed
	}
	if len(s.Prompts) == 0 {
		s.Prompts = nil
	}
	return nil
}

// unionHints merges add into have, preserving first-seen order and dropping
// duplicates. The result is capped: past the ceiling the oldest entries are
// dropped, because a hint dismissed long ago matters less than one dismissed
// just now, and silently refusing the new one would make the chip reappear.
func unionHints(have, add []string) []string {
	seen := make(map[string]struct{}, len(have)+len(add))
	out := make([]string, 0, len(have)+len(add))
	for _, id := range append(append([]string{}, have...), add...) {
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	if len(out) > maxDismissedHints {
		out = out[len(out)-maxDismissedHints:]
	}
	return out
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
