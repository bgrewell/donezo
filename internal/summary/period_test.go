package summary

import (
	"strings"
	"testing"
	"time"
)

func mustLoad(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("load %s: %v", name, err)
	}
	return loc
}

func TestResolvePeriodPresets(t *testing.T) {
	t.Parallel()
	la := mustLoad(t, "America/Los_Angeles")
	// Wednesday 2026-10-07 21:30 in Los Angeles — already Thursday in UTC,
	// so every preset is pinned to the person's day, not the server's.
	now := time.Date(2026, 10, 8, 4, 30, 0, 0, time.UTC)
	tests := []struct {
		preset, from, to string
		days             int
	}{
		{PresetToday, "2026-10-07", "2026-10-07", 1},
		{PresetYesterday, "2026-10-06", "2026-10-06", 1},
		{PresetThisWeek, "2026-10-05", "2026-10-07", 3},
		{PresetLastWeek, "2026-09-28", "2026-10-04", 7},
		{PresetLast7Days, "2026-10-01", "2026-10-07", 7},
		{PresetThisMonth, "2026-10-01", "2026-10-07", 7},
		{PresetLastMonth, "2026-09-01", "2026-09-30", 30},
		{PresetLast30Days, "2026-09-08", "2026-10-07", 30},
		{PresetThisQuarter, "2026-10-01", "2026-10-07", 7},
		{PresetLastQuarter, "2026-07-01", "2026-09-30", 92},
		{PresetYearToDate, "2026-01-01", "2026-10-07", 280},
		{PresetLastYear, "2025-01-01", "2025-12-31", 365},
	}
	for _, tt := range tests {
		t.Run(tt.preset, func(t *testing.T) {
			t.Parallel()
			p, err := ResolvePeriod(tt.preset, "", "", time.Monday, now, la)
			if err != nil {
				t.Fatalf("ResolvePeriod: %v", err)
			}
			if p.From != tt.from || p.To != tt.to || p.Days != tt.days || p.Preset != tt.preset {
				t.Errorf("got %+v, want %s..%s (%d days)", p, tt.from, tt.to, tt.days)
			}
			if p.Timezone != "America/Los_Angeles" {
				t.Errorf("timezone = %q", p.Timezone)
			}
		})
	}
}

func TestResolvePeriodWeekStartAndDefault(t *testing.T) {
	t.Parallel()
	utc := time.UTC
	wed := time.Date(2026, 10, 7, 12, 0, 0, 0, utc)
	mon := time.Date(2026, 10, 5, 12, 0, 0, 0, utc)
	sun := time.Date(2026, 10, 4, 12, 0, 0, 0, utc)

	if p, _ := ResolvePeriod(PresetThisWeek, "", "", time.Sunday, wed, utc); p.From != "2026-10-04" {
		t.Errorf("sunday-start this_week from = %s, want 2026-10-04", p.From)
	}
	if p, _ := ResolvePeriod(PresetLastWeek, "", "", time.Sunday, wed, utc); p.From != "2026-09-27" || p.To != "2026-10-03" {
		t.Errorf("sunday-start last_week = %s..%s", p.From, p.To)
	}
	// Default: last week on the first day of a week, this week otherwise.
	for _, tt := range []struct {
		name  string
		now   time.Time
		start time.Weekday
		want  string
	}{
		{"monday, monday start", mon, time.Monday, PresetLastWeek},
		{"wednesday", wed, time.Monday, PresetThisWeek},
		{"sunday, sunday start", sun, time.Sunday, PresetLastWeek},
		{"sunday, monday start", sun, time.Monday, PresetThisWeek},
	} {
		p, err := ResolvePeriod("", "", "", tt.start, tt.now, utc)
		if err != nil || p.Preset != tt.want {
			t.Errorf("%s: default = %q (%v), want %q", tt.name, p.Preset, err, tt.want)
		}
	}
}

func TestResolvePeriodCustom(t *testing.T) {
	t.Parallel()
	la := mustLoad(t, "America/Los_Angeles")
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, la)

	// Across the spring-forward day: still 10 calendar days, not 9.
	p, err := ResolvePeriod("", "2026-03-03", "2026-03-12", time.Monday, now, la)
	if err != nil || p.Days != 10 || p.Preset != "" {
		t.Errorf("custom = %+v (%v), want 10 days", p, err)
	}
	prev := p.Previous(la)
	if prev.From != "2026-02-21" || prev.To != "2026-03-02" || prev.Days != 10 {
		t.Errorf("previous = %+v, want 2026-02-21..2026-03-02", prev)
	}

	for _, tt := range []struct{ preset, from, to, want string }{
		{"", "2026-10-01", "", "needs both"},
		{"", "2026-10-07", "2026-10-01", "not be after"},
		{"", "10/01/2026", "2026-10-07", "from must be"},
		{PresetToday, "2026-10-01", "2026-10-07", "not both"},
		{"fortnight", "", "", "period must be one of"},
		{"", "2000-01-01", "2026-10-07", "at most"},
	} {
		if _, err := ResolvePeriod(tt.preset, tt.from, tt.to, time.Monday, now, la); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("ResolvePeriod(%q, %q, %q) err = %v, want %q", tt.preset, tt.from, tt.to, err, tt.want)
		}
	}
}

func TestZoneName(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	if got := zoneName(now, mustLoad(t, "America/Los_Angeles")); got != "America/Los_Angeles" {
		t.Errorf("IANA zone = %q", got)
	}
	// A location Go cannot name, as the host's zone is, gets abbreviation
	// and offset rather than "Local".
	host := time.FixedZone("PDT", -7*3600)
	if got := zoneName(now, host); got != "PDT" {
		t.Errorf("fixed zone = %q", got)
	}
	if got := zoneName(now, time.FixedZone("UTC", 0)); got != "UTC" {
		t.Errorf("UTC = %q, want plain UTC", got)
	}
	if got := zoneName(now, time.Local); got == "Local" || !strings.Contains(got, "UTC") {
		t.Errorf("time.Local = %q, want an abbreviation and offset", got)
	}
}

func TestParseWeekStart(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]time.Weekday{"": time.Monday, "Monday": time.Monday, "sunday": time.Sunday, " SATURDAY ": time.Saturday} {
		if got, err := ParseWeekStart(in); err != nil || got != want {
			t.Errorf("ParseWeekStart(%q) = %v, %v", in, got, err)
		}
	}
	if _, err := ParseWeekStart("tuesday"); err == nil {
		t.Error("tuesday accepted")
	}
}
