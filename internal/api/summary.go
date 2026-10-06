package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/bgrewell/donezo/internal/summary"
)

// handleSummary answers GET /api/spaces/{id}/summary: what got done over a
// period. The numbers come from internal/summary; this only reads the query,
// the space and the person's time zone.
//
// Query parameters, all optional:
//
//	period     a preset (see summary.Presets); default last_week on the first
//	           day of a week, this_week otherwise
//	from, to   a custom inclusive range, yyyy-MM-dd, instead of a preset
//	weekStart  monday (default), sunday or saturday
//	projects   comma-separated project ids
//	types      comma-separated activity types
//	tags       comma-separated tags (activity or project)
//	planned    1 to count planned activities
//	catchall   0 to leave out the Miscellaneous project
//	compare    1 to add the previous period of the same length
//	detail     headline, items (default) or full
//
// Reading is allowed on an archived space: looking back is exactly what an
// archive is for.
func (s *Server) handleSummary(w http.ResponseWriter, r *http.Request) {
	sp, ok := s.ownedSpace(w, r)
	if !ok {
		return
	}
	user, _ := userFrom(r.Context())
	q := r.URL.Query()

	weekStart, err := summary.ParseWeekStart(q.Get("weekStart"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	loc := s.userLocation(r.Context(), user.ID)
	now := s.clock()
	period, err := summary.ResolvePeriod(q.Get("period"), q.Get("from"), q.Get("to"), weekStart, now, loc)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	opt := summary.Options{
		ProjectIDs:      csv(q.Get("projects")),
		Types:           csv(q.Get("types")),
		Tags:            csv(q.Get("tags")),
		IncludePlanned:  q.Get("planned") == "1",
		ExcludeCatchall: q.Get("catchall") == "0",
		Compare:         q.Get("compare") == "1",
		Detail:          q.Get("detail"),
	}
	if err := opt.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	in, err := s.summaryInput(r.Context(), sp.ID)
	if err != nil {
		s.logger.Printf("space %s summary: %v", sp.ID, err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out, err := summary.Build(in, period, opt, now, loc)
	if err != nil {
		// Build's only failures are option checks it shares with Validate,
		// plus an unknown project id — all the caller's to fix.
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// summaryInput loads what a summary is built from.
func (s *Server) summaryInput(ctx context.Context, spaceID string) (summary.Input, error) {
	st, err := s.spaces.State(ctx, spaceID)
	if err != nil {
		return summary.Input{}, err
	}
	changes, err := s.spaces.ListProjectStatusChanges(ctx, spaceID)
	if err != nil {
		return summary.Input{}, err
	}
	since, err := s.spaces.CompletionsRecordedSince(ctx, spaceID)
	if err != nil {
		return summary.Input{}, err
	}
	return summary.Input{State: st, StatusChanges: changes, RecordedSince: since}, nil
}

// userLocation is the zone a user's calendar days are read in: their stored
// timezone, else the instance default. Failures fall back rather than error,
// for the same reason as the MCP handler's callerLocation — a summary should
// not be refused because a preference could not be read.
func (s *Server) userLocation(ctx context.Context, userID int64) *time.Location {
	settings, err := s.core.GetUserSettings(ctx, userID)
	if err != nil {
		s.logger.Printf("reading timezone for user %d: %v", userID, err)
		return s.location
	}
	if settings.Timezone == "" {
		return s.location
	}
	loc, err := time.LoadLocation(settings.Timezone)
	if err != nil {
		s.logger.Printf("stored timezone %q for user %d is unusable: %v", settings.Timezone, userID, err)
		return s.location
	}
	return loc
}

// csv splits a comma-separated query value, dropping empty entries.
func csv(v string) []string {
	var out []string
	for _, part := range strings.Split(v, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
