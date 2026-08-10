package api

import (
	"net/http"
)

// Usage statistics (#45). Admin only, and the boundary is here rather than
// in the UI that renders it: hiding a section is a layout decision, and this
// is the thing that actually stops a member reading everybody else's
// figures.
//
// What the response deliberately does not contain is documented on
// store.UsageStats — no item text, and no project or space identifiers,
// because a project id in donezo is its name slugified.

// handleUsageStats answers the admin usage panel.
//
// Everything is computed on demand from the space databases. There is no
// cache: at donezo's scale the pass is quick, and a rollup would be another
// thing to invalidate for a number nobody reads twice in a minute.
func (s *Server) handleUsageStats(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	stats, err := s.core.UsageStats(r.Context(), s.spaces)
	if err != nil {
		s.logger.Printf("usage stats: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, stats)
}
