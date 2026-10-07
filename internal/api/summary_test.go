package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/bgrewell/donezo/internal/summary"
)

func TestSummaryEndpoint(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, WithClock(fixedClock), WithLocation(time.UTC))
	h := s.Handler()
	// fixedClock is Sunday 2026-07-26 12:00 UTC.
	for _, st := range []step{
		{http.MethodPost, "/api/spaces/sandbox/activities", activityBody}, // loom, 2026-07-20
		{http.MethodPost, "/api/spaces/sandbox/tasks", strings.Replace(taskBody, `"title"`, `"projectId":"loom","title"`, 1)},
		{http.MethodPatch, "/api/spaces/sandbox/tasks/tsk-1", `{"status":"done"}`},
	} {
		if rec := doJSON(t, h, st.method, st.path, st.body); rec.Code >= 300 {
			t.Fatalf("seed %s %s: %d %s", st.method, st.path, rec.Code, rec.Body.String())
		}
	}

	get := func(query string) (int, summary.Summary, string) {
		t.Helper()
		rec := doJSON(t, h, http.MethodGet, "/api/spaces/sandbox/summary"+query, "")
		var out summary.Summary
		if rec.Code == http.StatusOK {
			if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
				t.Fatalf("parse: %v", err)
			}
		}
		return rec.Code, out, rec.Body.String()
	}

	code, out, body := get("?period=this_week&compare=1")
	if code != http.StatusOK {
		t.Fatalf("status = %d: %s", code, body)
	}
	if out.Period.From != "2026-07-20" || out.Period.To != "2026-07-26" {
		t.Errorf("period = %+v", out.Period)
	}
	if out.Totals.Activities != 1 || out.Totals.TasksCompleted != 1 || len(out.Projects) != 1 || out.Projects[0].ID != "loom" {
		t.Errorf("summary = %+v", out)
	}
	if out.Previous == nil || out.Previous.Period.From != "2026-07-13" {
		t.Errorf("previous = %+v", out.Previous)
	}

	// The user's own zone decides the days: at 12:00 UTC it is already
	// Monday 2026-07-27 on Kiritimati, a new week with nothing in it.
	if rec := doJSON(t, h, http.MethodPatch, "/api/settings", `{"timezone":"Pacific/Kiritimati"}`); rec.Code != http.StatusOK {
		t.Fatalf("set timezone: %d %s", rec.Code, rec.Body.String())
	}
	_, out, _ = get("?period=this_week")
	if out.Period.From != "2026-07-27" || out.Period.Timezone != "Pacific/Kiritimati" || out.Totals.Activities != 0 {
		t.Errorf("in the user's zone: period=%+v totals=%+v", out.Period, out.Totals)
	}
	_, out, _ = get("?from=2026-07-01&to=2026-07-31&detail=headline&types=work,meeting")
	if out.Period.Days != 31 || out.Totals.Activities != 1 || out.Projects[0].Items != nil {
		t.Errorf("custom headline = %+v", out)
	}

	for _, tt := range []struct{ query, want string }{
		{"?period=fortnight", "period must be one of"},
		{"?from=2026-07-01", "needs both"},
		{"?detail=verbose", "detail must be one of"},
		{"?types=nap", "types must be among"},
		{"?projects=ghost", `project \"ghost\" not found`},
		{"?weekStart=tuesday", "week start must be"},
	} {
		if code, _, body := get(tt.query); code != http.StatusBadRequest || !strings.Contains(body, tt.want) {
			t.Errorf("%s: %d %s, want 400 %q", tt.query, code, body, tt.want)
		}
	}

	if rec := doJSON(t, h, http.MethodPost, "/api/spaces/sandbox/summary", "{}"); rec.Code != http.StatusMethodNotAllowed ||
		rec.Header().Get("Allow") != http.MethodGet {
		t.Errorf("POST summary: %d Allow=%q, want 405 Allow=GET", rec.Code, rec.Header().Get("Allow"))
	}

	if rec := doJSON(t, h, http.MethodGet, "/api/spaces/private/summary", ""); rec.Code != http.StatusNotFound {
		t.Errorf("someone else's space: %d, want 404", rec.Code)
	}

	// Looking back is what an archive is for.
	if _, err := s.core.SetSpaceArchived(context.Background(), "sandbox", true); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if code, _, body := get("?period=last_30_days"); code != http.StatusOK {
		t.Errorf("archived space: %d %s, want 200", code, body)
	}
}
