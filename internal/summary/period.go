// Package summary answers "what got done over this period?" for one space.
//
// It is pure: callers hand it the space's state, its project status history,
// the clock and the person's time zone, and get back a structured summary.
// No storage, no HTTP, no model — the API, MCP and (later) a model-written
// narrative all build on the same numbers.
package summary

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Period presets. Each "this_*" period runs from its start to today, not to
// the end of the week or month: a summary covers what has happened, and a
// period padded with days still to come would dilute every per-day figure.
const (
	PresetToday       = "today"
	PresetYesterday   = "yesterday"
	PresetThisWeek    = "this_week"
	PresetLastWeek    = "last_week"
	PresetLast7Days   = "last_7_days"
	PresetThisMonth   = "this_month"
	PresetLastMonth   = "last_month"
	PresetLast30Days  = "last_30_days"
	PresetThisQuarter = "this_quarter"
	PresetLastQuarter = "last_quarter"
	PresetYearToDate  = "year_to_date"
	PresetLastYear    = "last_year"
)

// Presets lists every preset in the order a picker should offer them.
var Presets = []string{
	PresetToday, PresetYesterday, PresetThisWeek, PresetLastWeek, PresetLast7Days,
	PresetThisMonth, PresetLastMonth, PresetLast30Days, PresetThisQuarter,
	PresetLastQuarter, PresetYearToDate, PresetLastYear,
}

// MaxDays bounds a custom range. A decade is far beyond any useful summary;
// the cap exists to refuse nonsense, not to constrain real use.
const MaxDays = 3660

const day = "2006-01-02"

// Period is a resolved, inclusive range of local calendar days.
type Period struct {
	// Preset names the preset this came from; empty for a custom range.
	Preset string `json:"preset,omitempty"`
	// From and To are inclusive yyyy-MM-dd days in Timezone.
	From string `json:"from"`
	To   string `json:"to"`
	// Days is the number of calendar days From..To covers.
	Days int `json:"days"`
	// Timezone is the IANA zone the days are read in — or, for the host's own
	// zone, which Go knows only as "Local", its abbreviation and offset.
	Timezone string `json:"timezone"`
}

// ParseWeekStart reads a week-start name. Empty is Monday — ISO weeks, and
// what most work calendars use.
func ParseWeekStart(s string) (time.Weekday, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "monday":
		return time.Monday, nil
	case "sunday":
		return time.Sunday, nil
	case "saturday":
		return time.Saturday, nil
	}
	return 0, errors.New("week start must be monday, sunday or saturday")
}

// ResolvePeriod turns a preset, or a custom from/to, into a Period.
//
// With neither, it picks the default a person looking back most likely
// wants: last week on the first day of a week (this week has barely begun),
// this week otherwise. A custom range needs both ends; a preset and a range
// together are refused rather than one silently winning.
func ResolvePeriod(preset, from, to string, weekStart time.Weekday, now time.Time, loc *time.Location) (Period, error) {
	today := dateOf(now.In(loc))
	zone := zoneName(now, loc)
	custom := from != "" || to != ""
	switch {
	case preset != "" && custom:
		return Period{}, errors.New("give a period preset or from/to dates, not both")
	case custom:
		if from == "" || to == "" {
			return Period{}, errors.New("a custom period needs both from and to")
		}
		f, err := time.ParseInLocation(day, from, loc)
		if err != nil {
			return Period{}, errors.New("from must be a yyyy-MM-dd date")
		}
		t, err := time.ParseInLocation(day, to, loc)
		if err != nil {
			return Period{}, errors.New("to must be a yyyy-MM-dd date")
		}
		if t.Before(f) {
			return Period{}, errors.New("from must not be after to")
		}
		p := newPeriod("", f, t, zone)
		if p.Days > MaxDays {
			return Period{}, fmt.Errorf("a period can cover at most %d days", MaxDays)
		}
		return p, nil
	case preset == "":
		if today.Weekday() == weekStart {
			preset = PresetLastWeek
		} else {
			preset = PresetThisWeek
		}
	}

	weekStartDay := today.AddDate(0, 0, -int((7+today.Weekday()-weekStart)%7))
	monthStart := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, loc)
	quarterStart := time.Date(today.Year(), today.Month()-(today.Month()-1)%3, 1, 0, 0, 0, 0, loc)
	yearStart := time.Date(today.Year(), 1, 1, 0, 0, 0, 0, loc)

	var f, t time.Time
	switch preset {
	case PresetToday:
		f, t = today, today
	case PresetYesterday:
		f = today.AddDate(0, 0, -1)
		t = f
	case PresetThisWeek:
		f, t = weekStartDay, today
	case PresetLastWeek:
		f, t = weekStartDay.AddDate(0, 0, -7), weekStartDay.AddDate(0, 0, -1)
	case PresetLast7Days:
		f, t = today.AddDate(0, 0, -6), today
	case PresetThisMonth:
		f, t = monthStart, today
	case PresetLastMonth:
		f, t = monthStart.AddDate(0, -1, 0), monthStart.AddDate(0, 0, -1)
	case PresetLast30Days:
		f, t = today.AddDate(0, 0, -29), today
	case PresetThisQuarter:
		f, t = quarterStart, today
	case PresetLastQuarter:
		f, t = quarterStart.AddDate(0, -3, 0), quarterStart.AddDate(0, 0, -1)
	case PresetYearToDate:
		f, t = yearStart, today
	case PresetLastYear:
		f, t = yearStart.AddDate(-1, 0, 0), yearStart.AddDate(0, 0, -1)
	default:
		return Period{}, fmt.Errorf("period must be one of %s", strings.Join(Presets, ", "))
	}
	return newPeriod(preset, f, t, zone), nil
}

// Previous is the period of the same length immediately before p, for
// comparison.
func (p Period) Previous(loc *time.Location) Period {
	f, _ := time.ParseInLocation(day, p.From, loc)
	return newPeriod("", f.AddDate(0, 0, -p.Days), f.AddDate(0, 0, -1), p.Timezone)
}

// Contains reports whether the local day d (yyyy-MM-dd) falls in p.
func (p Period) Contains(d string) bool { return d >= p.From && d <= p.To }

func newPeriod(preset string, f, t time.Time, zone string) Period {
	return Period{
		Preset:   preset,
		From:     f.Format(day),
		To:       t.Format(day),
		Days:     daysBetween(f, t) + 1,
		Timezone: zone,
	}
}

// zoneName names loc for a reader. An IANA zone names itself; the host's
// zone does not — Go calls it "Local", which says nothing to someone reading
// a summary — so it is given as the abbreviation and offset in force now.
func zoneName(now time.Time, loc *time.Location) string {
	if name := loc.String(); name != "Local" {
		return name
	}
	t := now.In(loc)
	if abbr := t.Format("MST"); abbr != "UTC" {
		return abbr + " (UTC" + t.Format("-07:00") + ")"
	}
	return "UTC"
}

// dateOf is local midnight of t's calendar day, in t's location.
func dateOf(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// daysBetween counts calendar days from a to b. Computed on the dates, not
// the elapsed duration, so a daylight-saving change cannot make a day 23 or
// 25 hours long and throw the count off by one.
func daysBetween(a, b time.Time) int {
	ua := time.Date(a.Year(), a.Month(), a.Day(), 0, 0, 0, 0, time.UTC)
	ub := time.Date(b.Year(), b.Month(), b.Day(), 0, 0, 0, 0, time.UTC)
	return int(ub.Sub(ua).Hours() / 24)
}
