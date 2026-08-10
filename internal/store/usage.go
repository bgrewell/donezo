package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Usage statistics (#45), derived half.
//
// Every figure here is computed from what is already stored — no event log,
// no new columns. That is deliberate: an optional field nobody fills is a
// feature nobody uses, and that question can be answered today. The other
// half of #45 (which views get opened, which controls get pressed) genuinely
// needs instrumentation and is left to its own design, because what to
// instrument is easier to decide once these numbers exist.
//
// WHAT IS DELIBERATELY ABSENT, and must stay absent: item text of any kind,
// and project or space identifiers. Ids are not neutral in donezo — a
// project's id is slugified from its name ("home-infra", "5g-testbed"), and a
// space id is its name plus a suffix. Reporting "project home-infra has 12
// activities" would publish the contents of somebody's private space to an
// admin under the heading of usage statistics. Counts and distributions
// answer every question the issue asks; names answer none of them.

// EntityUsage counts one kind of thing, and how much of it is recent.
//
// The windows are what separate "used donezo heavily last year" from "uses
// donezo", which is the distinction that should steer development.
type EntityUsage struct {
	Total  int `json:"total"`
	Last7  int `json:"last7"`
	Last30 int `json:"last30"`
	Last90 int `json:"last90"`
}

// FieldAdoption is how many rows have an optional field filled in.
//
// The ratio is the point: Set/Total is "do people actually use this field",
// which is the cheapest feature-adoption signal donezo has.
type FieldAdoption struct {
	Total int `json:"total"`
	Set   int `json:"set"`
}

// UserUsage is one person's use of donezo, in counts.
type UserUsage struct {
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Role        string `json:"role"`
	CreatedAt   string `json:"createdAt"`

	Spaces         int `json:"spaces"`
	ArchivedSpaces int `json:"archivedSpaces"`

	Projects   EntityUsage `json:"projects"`
	Activities EntityUsage `json:"activities"`
	Tasks      EntityUsage `json:"tasks"`
	Notes      EntityUsage `json:"notes"`
	Reminders  EntityUsage `json:"reminders"`
	Inbox      EntityUsage `json:"inbox"`

	// Fields maps a field name to how often it is filled in. Keyed by the
	// frontend field name so a reader does not have to translate.
	Fields map[string]FieldAdoption `json:"fields"`

	// ActivityTypes, ProjectStatuses and InboxStatuses are distributions:
	// which kinds actually occur, and which are theoretical.
	ActivityTypes   map[string]int `json:"activityTypes"`
	ProjectStatuses map[string]int `json:"projectStatuses"`
	InboxStatuses   map[string]int `json:"inboxStatuses"`

	// TasksOpen, TasksDone and TasksOverdue describe whether tasks are
	// worked or merely written.
	TasksOpen    int `json:"tasksOpen"`
	TasksDone    int `json:"tasksDone"`
	TasksOverdue int `json:"tasksOverdue"`

	// AltNextActionsUsed is how many projects carry at least one alternate,
	// which answers whether that idea was real or speculative.
	AltNextActionsUsed int `json:"altNextActionsUsed"`

	// DistinctTags is how many different tags appear across projects and
	// activities — one tag used everywhere and fifty used once are very
	// different features.
	DistinctTags int `json:"distinctTags"`

	// APITokens is how many MCP tokens exist and how many have ever been
	// used, which is the closest stored signal for "drives donezo through
	// an agent".
	APITokens     int `json:"apiTokens"`
	APITokensUsed int `json:"apiTokensUsed"`

	// NotifyContacts is how many delivery destinations exist and how many
	// were confirmed — the drop-off between them says whether verification
	// is a speed bump or a wall (#52).
	NotifyContacts         int `json:"notifyContacts"`
	NotifyContactsVerified int `json:"notifyContactsVerified"`

	// LastWriteAt is the most recent write anywhere in their spaces, as a
	// civil date. The single best "is this person still here" figure.
	LastWriteAt string `json:"lastWriteAt,omitempty"`
}

// InstanceUsage is the whole instance: every user, plus the totals.
type InstanceUsage struct {
	// GeneratedAt is when this was computed (RFC 3339 UTC). Everything here
	// is derived on demand, so it is only ever as fresh as this.
	GeneratedAt string `json:"generatedAt"`
	// Users are the per-user rows. Per user rather than instance-only
	// because "who is actually using this" is the question on an instance
	// with a handful of invited people (#45's open question).
	Users []UserUsage `json:"users"`
	// Totals is every user's figures added up.
	Totals UserUsage `json:"totals"`
	// ActiveLast30 is how many users wrote anything in the last 30 days.
	ActiveLast30 int `json:"activeLast30"`
	// NotDerivable names the questions #45 asks that this cannot answer,
	// so a reader is not left believing a zero means "never used".
	NotDerivable []string `json:"notDerivable"`
}

// notDerivable lists what the stored data genuinely cannot say, so the panel
// can show it rather than implying the numbers are complete.
//
// Being explicit here matters more than it looks: an absent figure reads as
// "nobody does this", and two of these would be read exactly that way.
var notDerivable = []string{
	"Which views and controls get opened — nothing records a read, so an unused view leaves no trace. Needs the event log half of #45.",
	"Web versus MCP writes — the surface is known at write time (it drives the revision counter) but is not stored on the row.",
	"Inbox triage latency — captures record when they arrived, not when they were classified.",
	"Whether the model polish is ever accepted — the rewrite is not marked as model-assisted once saved.",
}

// UsageStats computes usage across every user on the instance.
//
// One pass per space, opening each space database in turn. That is fine at
// donezo's scale (a personal instance with invited people) and is why there
// is no rollup table: a cache would be another thing to invalidate for a
// figure nobody reads more than once a week.
func (s *CoreStore) UsageStats(ctx context.Context, spaces *SpaceStore) (InstanceUsage, error) {
	users, err := s.ListUsers(ctx)
	if err != nil {
		return InstanceUsage{}, err
	}
	all, err := s.ListSpaces(ctx)
	if err != nil {
		return InstanceUsage{}, err
	}
	bySpaceOwner := make(map[int64][]Space, len(users))
	for _, sp := range all {
		bySpaceOwner[sp.UserID] = append(bySpaceOwner[sp.UserID], sp)
	}

	now := s.opts.clock().UTC()
	out := InstanceUsage{
		GeneratedAt:  now.Format(time.RFC3339),
		Users:        make([]UserUsage, 0, len(users)),
		Totals:       newUserUsage(),
		NotDerivable: notDerivable,
	}
	for _, u := range users {
		usage, err := s.userUsage(ctx, spaces, u, bySpaceOwner[u.ID], now)
		if err != nil {
			return InstanceUsage{}, err
		}
		out.Users = append(out.Users, usage)
		addUsage(&out.Totals, usage)
		if recentlyActive(usage.LastWriteAt, now) {
			out.ActiveLast30++
		}
	}
	out.Totals.Username = ""
	out.Totals.DisplayName = ""
	out.Totals.Role = ""
	out.Totals.CreatedAt = ""
	return out, nil
}

// newUserUsage returns a zero row with its maps ready.
func newUserUsage() UserUsage {
	return UserUsage{
		Fields:          map[string]FieldAdoption{},
		ActivityTypes:   map[string]int{},
		ProjectStatuses: map[string]int{},
		InboxStatuses:   map[string]int{},
	}
}

// userUsage computes one person's figures across their spaces.
func (s *CoreStore) userUsage(ctx context.Context, spaces *SpaceStore, u User, owned []Space, now time.Time) (UserUsage, error) {
	usage := newUserUsage()
	usage.Username = u.Username
	usage.DisplayName = u.DisplayName
	usage.Role = u.Role
	usage.CreatedAt = u.CreatedAt

	tags := map[string]struct{}{}
	for _, sp := range owned {
		usage.Spaces++
		if sp.ArchivedAt != nil {
			usage.ArchivedSpaces++
			// An archived space is still theirs and still counts: excluding
			// it would make archiving look like deletion in the figures.
		}
		if err := s.spaceUsage(ctx, spaces, sp.ID, now, &usage, tags); err != nil {
			return UserUsage{}, err
		}
	}
	usage.DistinctTags = len(tags)

	tokens, err := s.ListAPITokens(ctx, u.ID)
	if err != nil {
		return UserUsage{}, err
	}
	for _, t := range tokens {
		if t.RevokedAt != nil {
			continue
		}
		usage.APITokens++
		if t.LastUsedAt != nil {
			usage.APITokensUsed++
		}
	}

	contacts, err := s.ListUserContacts(ctx, u.ID)
	if err != nil {
		return UserUsage{}, err
	}
	usage.NotifyContacts = len(contacts)
	for _, c := range contacts {
		if c.Verified() {
			usage.NotifyContactsVerified++
		}
	}
	return usage, nil
}

// spaceUsage adds one space's figures into usage.
func (s *CoreStore) spaceUsage(ctx context.Context, spaces *SpaceStore, spaceID string, now time.Time, usage *UserUsage, tags map[string]struct{}) error {
	db, err := spaces.db(ctx, spaceID)
	if err != nil {
		return err
	}
	cut7 := now.AddDate(0, 0, -7).Format(time.RFC3339)
	cut30 := now.AddDate(0, 0, -30).Format(time.RFC3339)
	cut90 := now.AddDate(0, 0, -90).Format(time.RFC3339)
	today := now.Format("2006-01-02")

	// Projects: counts, field adoption, status distribution.
	var p struct {
		total, l7, l30, l90                            int
		purpose, outcome, focus, next, resume, waiting int
		alt, tagged                                    int
	}
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*),
		       COALESCE(SUM(CASE WHEN created_at >= ? THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN created_at >= ? THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN created_at >= ? THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN TRIM(purpose)        <> '' THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN TRIM(outcome)        <> '' THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN TRIM(current_focus)  <> '' THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN TRIM(next_action)    <> '' THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN TRIM(resume_context) <> '' THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN waiting_on IS NOT NULL AND TRIM(waiting_on) <> '' THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN alt_next_actions NOT IN ('[]', '') THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN tags NOT IN ('[]', '') THEN 1 ELSE 0 END), 0)
		FROM projects WHERE deleted_at IS NULL`,
		cut7, cut30, cut90).
		Scan(&p.total, &p.l7, &p.l30, &p.l90, &p.purpose, &p.outcome, &p.focus,
			&p.next, &p.resume, &p.waiting, &p.alt, &p.tagged)
	if err != nil {
		return fmt.Errorf("store: usage projects: %w", err)
	}
	addEntity(&usage.Projects, EntityUsage{Total: p.total, Last7: p.l7, Last30: p.l30, Last90: p.l90})
	addField(usage.Fields, "purpose", p.total, p.purpose)
	addField(usage.Fields, "outcome", p.total, p.outcome)
	addField(usage.Fields, "currentFocus", p.total, p.focus)
	addField(usage.Fields, "nextAction", p.total, p.next)
	addField(usage.Fields, "resumeContext", p.total, p.resume)
	addField(usage.Fields, "waitingOn", p.total, p.waiting)
	addField(usage.Fields, "projectTags", p.total, p.tagged)
	usage.AltNextActionsUsed += p.alt

	if err := addDistribution(ctx, db,
		`SELECT status, COUNT(*) FROM projects WHERE deleted_at IS NULL GROUP BY status`,
		usage.ProjectStatuses); err != nil {
		return err
	}

	// Activities: counts, effort and detail adoption, type distribution.
	var a struct {
		total, l7, l30, l90            int
		effort, details, tagged, links int
	}
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*),
		       COALESCE(SUM(CASE WHEN created_at >= ? THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN created_at >= ? THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN created_at >= ? THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN effort_hours IS NOT NULL THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN TRIM(details) <> '' THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN tags  NOT IN ('[]', '') THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN links NOT IN ('[]', '') THEN 1 ELSE 0 END), 0)
		FROM activities WHERE deleted_at IS NULL`,
		cut7, cut30, cut90).
		Scan(&a.total, &a.l7, &a.l30, &a.l90, &a.effort, &a.details, &a.tagged, &a.links)
	if err != nil {
		return fmt.Errorf("store: usage activities: %w", err)
	}
	addEntity(&usage.Activities, EntityUsage{Total: a.total, Last7: a.l7, Last30: a.l30, Last90: a.l90})
	addField(usage.Fields, "effortHours", a.total, a.effort)
	addField(usage.Fields, "activityDetails", a.total, a.details)
	addField(usage.Fields, "activityTags", a.total, a.tagged)
	addField(usage.Fields, "activityLinks", a.total, a.links)

	if err := addDistribution(ctx, db,
		`SELECT type, COUNT(*) FROM activities WHERE deleted_at IS NULL GROUP BY type`,
		usage.ActivityTypes); err != nil {
		return err
	}

	// Tasks: counts, dating and waiting adoption, worked-or-written.
	var tk struct {
		total, l7, l30, l90         int
		due, details, waiting       int
		open, done, overdue, linked int
	}
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*),
		       COALESCE(SUM(CASE WHEN created_at >= ? THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN created_at >= ? THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN created_at >= ? THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN due IS NOT NULL AND TRIM(due) <> '' THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN TRIM(COALESCE(details, '')) <> '' THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN waiting_on IS NOT NULL AND TRIM(waiting_on) <> '' THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN status <> 'done' THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN status =  'done' THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN status <> 'done' AND due IS NOT NULL AND due <> '' AND due < ? THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN project_id IS NOT NULL THEN 1 ELSE 0 END), 0)
		FROM tasks WHERE deleted_at IS NULL`,
		cut7, cut30, cut90, today).
		Scan(&tk.total, &tk.l7, &tk.l30, &tk.l90, &tk.due, &tk.details, &tk.waiting,
			&tk.open, &tk.done, &tk.overdue, &tk.linked)
	if err != nil {
		return fmt.Errorf("store: usage tasks: %w", err)
	}
	addEntity(&usage.Tasks, EntityUsage{Total: tk.total, Last7: tk.l7, Last30: tk.l30, Last90: tk.l90})
	addField(usage.Fields, "taskDue", tk.total, tk.due)
	addField(usage.Fields, "taskDetails", tk.total, tk.details)
	addField(usage.Fields, "taskWaitingOn", tk.total, tk.waiting)
	addField(usage.Fields, "taskProject", tk.total, tk.linked)
	usage.TasksOpen += tk.open
	usage.TasksDone += tk.done
	usage.TasksOverdue += tk.overdue

	// Notes.
	var n struct{ total, l7, l30, l90, bodies, linked int }
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*),
		       COALESCE(SUM(CASE WHEN created_at >= ? THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN created_at >= ? THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN created_at >= ? THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN TRIM(body) <> '' THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN project_id IS NOT NULL THEN 1 ELSE 0 END), 0)
		FROM notes WHERE deleted_at IS NULL`,
		cut7, cut30, cut90).
		Scan(&n.total, &n.l7, &n.l30, &n.l90, &n.bodies, &n.linked)
	if err != nil {
		return fmt.Errorf("store: usage notes: %w", err)
	}
	addEntity(&usage.Notes, EntityUsage{Total: n.total, Last7: n.l7, Last30: n.l30, Last90: n.l90})
	addField(usage.Fields, "noteBody", n.total, n.bodies)
	addField(usage.Fields, "noteProject", n.total, n.linked)

	// Reminders. No created_at on this table, so the windows are measured
	// on the time it is set for — which is what a reminder is anyway.
	var rm struct{ total, l7, l30, l90, details, done, delivered int }
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*),
		       COALESCE(SUM(CASE WHEN remind_at >= ? THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN remind_at >= ? THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN remind_at >= ? THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN TRIM(COALESCE(details, '')) <> '' THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN done = 1 THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN notified_at IS NOT NULL THEN 1 ELSE 0 END), 0)
		FROM reminders WHERE deleted_at IS NULL`,
		cut7[:10], cut30[:10], cut90[:10]).
		Scan(&rm.total, &rm.l7, &rm.l30, &rm.l90, &rm.details, &rm.done, &rm.delivered)
	if err != nil {
		return fmt.Errorf("store: usage reminders: %w", err)
	}
	addEntity(&usage.Reminders, EntityUsage{Total: rm.total, Last7: rm.l7, Last30: rm.l30, Last90: rm.l90})
	addField(usage.Fields, "reminderDetails", rm.total, rm.details)
	addField(usage.Fields, "reminderDelivered", rm.total, rm.delivered)

	// Inbox: counts and where captures end up.
	var in struct{ total, l7, l30, l90, suggested int }
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*),
		       COALESCE(SUM(CASE WHEN captured_at >= ? THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN captured_at >= ? THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN captured_at >= ? THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN suggested_project_id IS NOT NULL THEN 1 ELSE 0 END), 0)
		FROM inbox WHERE deleted_at IS NULL`,
		cut7, cut30, cut90).
		Scan(&in.total, &in.l7, &in.l30, &in.l90, &in.suggested)
	if err != nil {
		return fmt.Errorf("store: usage inbox: %w", err)
	}
	addEntity(&usage.Inbox, EntityUsage{Total: in.total, Last7: in.l7, Last30: in.l30, Last90: in.l90})
	addField(usage.Fields, "inboxSuggestedProject", in.total, in.suggested)

	if err := addDistribution(ctx, db,
		`SELECT status, COUNT(*) FROM inbox WHERE deleted_at IS NULL GROUP BY status`,
		usage.InboxStatuses); err != nil {
		return err
	}

	// Distinct tags, across projects and activities. Read as JSON text and
	// counted here rather than in SQL: the values are a JSON array, and the
	// point is how many different ones exist, not what they say.
	if err := collectTags(ctx, db, tags); err != nil {
		return err
	}

	// Last write: the newest updated_at anywhere that has one.
	var last sql.NullString
	if err := db.QueryRowContext(ctx, `
		SELECT MAX(stamp) FROM (
			SELECT MAX(updated_at) AS stamp FROM projects   WHERE deleted_at IS NULL
			UNION ALL
			SELECT MAX(updated_at) AS stamp FROM activities WHERE deleted_at IS NULL
			UNION ALL
			SELECT MAX(created_at) AS stamp FROM tasks      WHERE deleted_at IS NULL
			UNION ALL
			SELECT MAX(created_at) AS stamp FROM notes      WHERE deleted_at IS NULL
		)`).Scan(&last); err != nil {
		return fmt.Errorf("store: usage last write: %w", err)
	}
	if last.Valid && last.String > usage.LastWriteAt {
		usage.LastWriteAt = last.String
	}
	return nil
}

// collectTags adds every distinct tag in a space to seen.
func collectTags(ctx context.Context, db *sql.DB, seen map[string]struct{}) error {
	for _, q := range []string{
		`SELECT tags FROM projects   WHERE deleted_at IS NULL AND tags NOT IN ('[]', '')`,
		`SELECT tags FROM activities WHERE deleted_at IS NULL AND tags NOT IN ('[]', '')`,
	} {
		rows, err := db.QueryContext(ctx, q)
		if err != nil {
			return fmt.Errorf("store: usage tags: %w", err)
		}
		for rows.Next() {
			var raw string
			if err := rows.Scan(&raw); err != nil {
				closeQuietly(rows)
				return fmt.Errorf("store: usage tags: %w", err)
			}
			list, err := unmarshalList[string](raw)
			if err != nil {
				// A tag column that will not parse is a broken row, not a
				// reason to fail the whole panel.
				continue
			}
			for _, tag := range list {
				seen[tag] = struct{}{}
			}
		}
		err = rows.Err()
		closeQuietly(rows)
		if err != nil {
			return fmt.Errorf("store: usage tags: %w", err)
		}
	}
	return nil
}

// addDistribution runs a two-column "value, count" query into dst.
func addDistribution(ctx context.Context, db *sql.DB, query string, dst map[string]int) error {
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("store: usage distribution: %w", err)
	}
	defer closeQuietly(rows)
	for rows.Next() {
		var key string
		var n int
		if err := rows.Scan(&key, &n); err != nil {
			return fmt.Errorf("store: usage distribution: %w", err)
		}
		dst[key] += n
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("store: usage distribution: %w", err)
	}
	return nil
}

// addEntity accumulates one entity's counts.
func addEntity(dst *EntityUsage, src EntityUsage) {
	dst.Total += src.Total
	dst.Last7 += src.Last7
	dst.Last30 += src.Last30
	dst.Last90 += src.Last90
}

// addField accumulates a field-adoption ratio.
func addField(dst map[string]FieldAdoption, name string, total, set int) {
	cur := dst[name]
	cur.Total += total
	cur.Set += set
	dst[name] = cur
}

// addUsage folds one user's figures into a running total.
func addUsage(dst *UserUsage, src UserUsage) {
	dst.Spaces += src.Spaces
	dst.ArchivedSpaces += src.ArchivedSpaces
	addEntity(&dst.Projects, src.Projects)
	addEntity(&dst.Activities, src.Activities)
	addEntity(&dst.Tasks, src.Tasks)
	addEntity(&dst.Notes, src.Notes)
	addEntity(&dst.Reminders, src.Reminders)
	addEntity(&dst.Inbox, src.Inbox)
	for name, f := range src.Fields {
		addField(dst.Fields, name, f.Total, f.Set)
	}
	for k, v := range src.ActivityTypes {
		dst.ActivityTypes[k] += v
	}
	for k, v := range src.ProjectStatuses {
		dst.ProjectStatuses[k] += v
	}
	for k, v := range src.InboxStatuses {
		dst.InboxStatuses[k] += v
	}
	dst.TasksOpen += src.TasksOpen
	dst.TasksDone += src.TasksDone
	dst.TasksOverdue += src.TasksOverdue
	dst.AltNextActionsUsed += src.AltNextActionsUsed
	// Distinct tags do not add up across users — the same tag in two
	// accounts is one tag — but summing is the honest approximation here,
	// since deduplicating would mean collecting tag text centrally.
	dst.DistinctTags += src.DistinctTags
	dst.APITokens += src.APITokens
	dst.APITokensUsed += src.APITokensUsed
	dst.NotifyContacts += src.NotifyContacts
	dst.NotifyContactsVerified += src.NotifyContactsVerified
	if src.LastWriteAt > dst.LastWriteAt {
		dst.LastWriteAt = src.LastWriteAt
	}
}

// recentlyActive reports whether a stamp is inside 30 days. Stamps come in
// two shapes here (RFC 3339 and civil dates), and comparing the first ten
// characters is correct for both.
func recentlyActive(stamp string, now time.Time) bool {
	if len(stamp) < 10 {
		return false
	}
	return stamp[:10] >= now.AddDate(0, 0, -30).Format("2006-01-02")
}
