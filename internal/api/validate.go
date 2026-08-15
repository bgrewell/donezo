package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/bgrewell/donezo/internal/store"
)

// This file is the request-validation layer for the mutation endpoints.
// The enum unions mirror web/src/domain/types.ts exactly; every rejection
// is a 400 with a calm, field-specific message.

// maxBodyBytes bounds entity mutation request bodies; generous for long
// note bodies and resume contexts while still refusing pathological
// payloads.
const maxBodyBytes = 1 << 20

// Enum unions from web/src/domain/types.ts, in declaration order.
var (
	// projectColors mirrors ProjectColor (also used for space colors —
	// spaces key into the same --dz-pj-* ramp).
	projectColors = []string{"blue", "green", "tan", "violet", "rose", "orange", "steel"}
	// projectStatuses mirrors ProjectStatus. Both "completed" and
	// "cancelled" are closed states; the server treats them alike.
	projectStatuses = []string{"active", "waiting", "blocked", "paused", "completed", "cancelled"}
	// activityTypes mirrors ActivityType.
	activityTypes = []string{"work", "research", "meeting", "decision", "blocker", "milestone"}
	// activitySources mirrors ActivityEntry["source"].
	activitySources = []string{"manual", "capture", "import"}
	// taskStatuses mirrors TaskStatus.
	taskStatuses = []string{"open", "waiting", "someday", "done"}
	// itemKinds mirrors ItemKind.
	itemKinds = []string{"task", "note", "reminder", "activity", "project"}
	// inboxStatuses mirrors InboxItem["status"].
	inboxStatuses = []string{"pending", "converted", "dismissed"}
	// themeIDs mirrors THEMES in web/src/lib/themes.ts.
	themeIDs = []string{"console", "slate", "paper", "blossom"}
	// fontIDs mirrors FONT_SETS in web/src/lib/themes.ts.
	fontIDs = []string{"plex", "inter", "system"}
	// fontSizeIDs mirrors FONT_SIZES in web/src/lib/themes.ts.
	fontSizeIDs = []string{"small", "medium", "large"}
)

// requireJSONContentType rejects a bodied request that does not declare
// Content-Type: application/json.
//
// This closes a login/CSRF vector. A cross-origin HTML form can POST a
// text/plain body that happens to be valid JSON — a CORS "simple request"
// that needs no preflight — so without this check a hostile page could drive
// the cookie-authenticated API (log a victim into the attacker's account, or
// write on their behalf). Requiring the JSON media type takes these routes
// out of the simple-request set: a cross-origin caller then needs a preflight,
// which donezod does not answer, so the browser blocks the request. The web
// app already sends this header on every write (web/src/api/client.ts).
func requireJSONContentType(w http.ResponseWriter, r *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return false
	}
	return true
}

// decodeBody parses one strict JSON value from the request into dst:
// unknown fields, type mismatches, oversized bodies, and trailing content
// all answer 400 with a calm message, reporting false.
func (s *Server) decodeBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	if !requireJSONContentType(w, r) {
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, decodeMessage(err))
		return false
	}
	if dec.More() {
		writeError(w, http.StatusBadRequest, "request body must be a single JSON object")
		return false
	}
	return true
}

// decodeMessage translates a JSON decoding failure into a calm,
// field-specific 400 message.
func decodeMessage(err error) string {
	var typeErr *json.UnmarshalTypeError
	var maxErr *http.MaxBytesError
	switch {
	case errors.As(err, &maxErr):
		return "request body is too large"
	case errors.As(err, &typeErr):
		if typeErr.Field != "" {
			return typeErr.Field + " has the wrong type"
		}
		return "request body must be a JSON object"
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		// Single quotes: the message lives inside a JSON string, where
		// double quotes would be escaped.
		name := strings.TrimPrefix(err.Error(), "json: unknown field ")
		return "unknown field " + strings.ReplaceAll(name, `"`, "'")
	default:
		return "invalid JSON body"
	}
}

// ─── Field validators ───────────────────────────────────────────────────

// entityIDPattern matches client-generated entity ids, the shape the
// frontend's newId() produces.
var entityIDPattern = regexp.MustCompile(`^[a-z0-9-]{1,64}$`)

// entityID validates a client-generated id: 1-64 chars of a-z, 0-9, -.
func entityID(field, v string) error {
	if v == "" {
		return fmt.Errorf("%s is required", field)
	}
	if !entityIDPattern.MatchString(v) {
		return fmt.Errorf("%s must be 1-64 characters of a-z, 0-9, or dashes", field)
	}
	return nil
}

// required rejects an empty string with "<field> is required".
func required(field, v string) error {
	if v == "" {
		return fmt.Errorf("%s is required", field)
	}
	return nil
}

// oneOf validates v against a types.ts union.
func oneOf(field, v string, allowed []string) error {
	for _, a := range allowed {
		if v == a {
			return nil
		}
	}
	return fmt.Errorf("%s must be one of %s", field, strings.Join(allowed, ", "))
}

// optionalOneOf validates an omittable union field. A nil pointer means the
// field was not sent and is left alone; an empty string clears the stored
// preference so it follows the current default again; anything else must be
// a member of the union.
func optionalOneOf(field string, v *string, allowed []string) error {
	if v == nil || *v == "" {
		return nil
	}
	return oneOf(field, *v, allowed)
}

// isoDate validates a yyyy-MM-dd date.
func isoDate(field, v string) error {
	if _, err := time.Parse("2006-01-02", v); err != nil {
		return fmt.Errorf("%s must be a yyyy-MM-dd date", field)
	}
	return nil
}

// isoDateTime validates an ISO datetime, with or without a zone offset
// (the frontend writes local datetimes like 2026-07-26T09:00:00).
func isoDateTime(field, v string) error {
	for _, layout := range []string{"2006-01-02T15:04:05", time.RFC3339} {
		if _, err := time.Parse(layout, v); err == nil {
			return nil
		}
	}
	return fmt.Errorf("%s must be an ISO datetime like 2026-07-26T09:00:00", field)
}

// optionalNonEmpty rejects an optional string that is present but empty.
func optionalNonEmpty(field string, v *string) error {
	if v != nil && *v == "" {
		return fmt.Errorf("%s must not be empty", field)
	}
	return nil
}

// firstError returns the first non-nil error, composing field checks.
func firstError(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// decodeNullable decodes an optional clearable PATCH field: an absent raw
// value leaves dst untouched, JSON null sets *dst to nil (clear), and a
// value of T points *dst at it.
func decodeNullable[T any](field, want string, raw json.RawMessage, dst **T) error {
	if raw == nil {
		return nil
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("%s must be a %s or null", field, want)
	}
	return nil
}

// ─── Create validators ──────────────────────────────────────────────────

// validateProjectCreate checks a POST projects body.
func validateProjectCreate(p store.Project) error {
	return firstError(
		entityID("id", p.ID),
		required("name", p.Name),
		oneOf("color", p.Color, projectColors),
		oneOf("status", p.Status, projectStatuses),
	)
}

// validateActivityCreate checks a POST activities body.
func validateActivityCreate(a store.ActivityEntry) error {
	return firstError(
		entityID("id", a.ID),
		required("projectId", a.ProjectID),
		isoDate("date", a.Date),
		oneOf("type", a.Type, activityTypes),
		required("title", a.Title),
		oneOf("source", a.Source, activitySources),
	)
}

// validateTaskCreate checks a POST tasks body.
func validateTaskCreate(t store.TaskItem) error {
	if err := firstError(
		entityID("id", t.ID),
		required("title", t.Title),
		oneOf("status", t.Status, taskStatuses),
		isoDate("createdAt", t.CreatedAt),
		optionalNonEmpty("projectId", t.ProjectID),
	); err != nil {
		return err
	}
	if t.Due != nil {
		return isoDate("due", *t.Due)
	}
	return nil
}

// validateNoteCreate checks a POST notes body.
func validateNoteCreate(n store.NoteItem) error {
	return firstError(
		entityID("id", n.ID),
		required("title", n.Title),
		isoDate("createdAt", n.CreatedAt),
		optionalNonEmpty("projectId", n.ProjectID),
	)
}

// validateReminderCreate checks a POST reminders body.
func validateReminderCreate(r store.Reminder) error {
	return firstError(
		entityID("id", r.ID),
		required("text", r.Text),
		isoDateTime("remindAt", r.RemindAt),
		optionalNonEmpty("projectId", r.ProjectID),
	)
}

// validateInboxCreate checks a POST inbox body.
func validateInboxCreate(it store.InboxItem) error {
	return firstError(
		entityID("id", it.ID),
		required("raw", it.Raw),
		isoDateTime("capturedAt", it.CapturedAt),
		oneOf("suggestedKind", it.SuggestedKind, itemKinds),
		oneOf("status", it.Status, inboxStatuses),
		optionalNonEmpty("suggestedProjectId", it.SuggestedProjectID),
	)
}

// ─── PATCH bodies ───────────────────────────────────────────────────────
//
// Each patch struct carries every mutable field of its entity, all
// optional. Plain pointer fields distinguish absent from present;
// clearable fields are json.RawMessage so null (clear) can be told apart
// from absent (keep), with the decoded value cached by validate for
// apply. The apply methods are fed to the store's transactional Patch*
// operations and never fail after validate has passed.

// projectPatch is the PATCH projects/{pid} body.
type projectPatch struct {
	Name           *string         `json:"name"`
	Color          *string         `json:"color"`
	Purpose        *string         `json:"purpose"`
	Outcome        *string         `json:"outcome"`
	CurrentFocus   *string         `json:"currentFocus"`
	NextAction     *string         `json:"nextAction"`
	AltNextActions *[]string       `json:"altNextActions"`
	Status         *string         `json:"status"`
	ResumeContext  *string         `json:"resumeContext"`
	WaitingOn      json.RawMessage `json:"waitingOn"`
	Tags           *[]string       `json:"tags"`

	waitingOn *string // decoded by validate
}

// validate checks every present field and decodes the clearable ones.
func (p *projectPatch) validate() error {
	if p.Name != nil {
		if err := required("name", *p.Name); err != nil {
			return err
		}
	}
	if p.Color != nil {
		if err := oneOf("color", *p.Color, projectColors); err != nil {
			return err
		}
	}
	if p.Status != nil {
		if err := oneOf("status", *p.Status, projectStatuses); err != nil {
			return err
		}
	}
	return decodeNullable("waitingOn", "string", p.WaitingOn, &p.waitingOn)
}

// apply copies the present fields onto the stored project.
func (p *projectPatch) apply(cur *store.Project) error {
	if p.Name != nil {
		cur.Name = *p.Name
	}
	if p.Color != nil {
		cur.Color = *p.Color
	}
	if p.Purpose != nil {
		cur.Purpose = *p.Purpose
	}
	if p.Outcome != nil {
		cur.Outcome = *p.Outcome
	}
	if p.CurrentFocus != nil {
		cur.CurrentFocus = *p.CurrentFocus
	}
	if p.NextAction != nil {
		cur.NextAction = *p.NextAction
	}
	if p.AltNextActions != nil {
		cur.AltNextActions = *p.AltNextActions
	}
	if p.Status != nil {
		cur.Status = *p.Status
	}
	if p.ResumeContext != nil {
		cur.ResumeContext = *p.ResumeContext
	}
	if p.WaitingOn != nil {
		cur.WaitingOn = p.waitingOn
	}
	if p.Tags != nil {
		cur.Tags = *p.Tags
	}
	return nil
}

// activityPatch is the PATCH activities/{aid} body.
type activityPatch struct {
	ProjectID   *string               `json:"projectId"`
	Date        *string               `json:"date"`
	Type        *string               `json:"type"`
	Title       *string               `json:"title"`
	Details     *string               `json:"details"`
	EffortHours json.RawMessage       `json:"effortHours"`
	Source      *string               `json:"source"`
	Tags        *[]string             `json:"tags"`
	Links       *[]store.ActivityLink `json:"links"`
	NextAction  json.RawMessage       `json:"nextAction"`
	Planned     json.RawMessage       `json:"planned"`

	effortHours *float64 // decoded by validate
	nextAction  *string  // decoded by validate
	planned     *bool    // decoded by validate
}

// validate checks every present field and decodes the clearable ones.
func (p *activityPatch) validate() error {
	if p.ProjectID != nil {
		if err := required("projectId", *p.ProjectID); err != nil {
			return err
		}
	}
	if p.Date != nil {
		if err := isoDate("date", *p.Date); err != nil {
			return err
		}
	}
	if p.Type != nil {
		if err := oneOf("type", *p.Type, activityTypes); err != nil {
			return err
		}
	}
	if p.Title != nil {
		if err := required("title", *p.Title); err != nil {
			return err
		}
	}
	if p.Source != nil {
		if err := oneOf("source", *p.Source, activitySources); err != nil {
			return err
		}
	}
	return firstError(
		decodeNullable("effortHours", "number", p.EffortHours, &p.effortHours),
		decodeNullable("nextAction", "string", p.NextAction, &p.nextAction),
		decodeNullable("planned", "boolean", p.Planned, &p.planned),
	)
}

// apply copies the present fields onto the stored activity.
func (p *activityPatch) apply(cur *store.ActivityEntry) error {
	if p.ProjectID != nil {
		cur.ProjectID = *p.ProjectID
	}
	if p.Date != nil {
		cur.Date = *p.Date
	}
	if p.Type != nil {
		cur.Type = *p.Type
	}
	if p.Title != nil {
		cur.Title = *p.Title
	}
	if p.Details != nil {
		cur.Details = *p.Details
	}
	if p.EffortHours != nil {
		cur.EffortHours = p.effortHours
	}
	if p.Source != nil {
		cur.Source = *p.Source
	}
	if p.Tags != nil {
		cur.Tags = *p.Tags
	}
	if p.Links != nil {
		cur.Links = *p.Links
	}
	if p.NextAction != nil {
		cur.NextAction = p.nextAction
	}
	if p.Planned != nil {
		cur.Planned = p.planned
	}
	return nil
}

// taskPatch is the PATCH tasks/{tid} body.
type taskPatch struct {
	ProjectID json.RawMessage `json:"projectId"`
	Title     *string         `json:"title"`
	// Details needs no clearable treatment: it is a plain string whose empty
	// value IS cleared, so "" says everything null would.
	Details   *string         `json:"details"`
	Status    *string         `json:"status"`
	Due       json.RawMessage `json:"due"`
	WaitingOn json.RawMessage `json:"waitingOn"`
	CreatedAt *string         `json:"createdAt"`

	projectID *string // decoded by validate
	due       *string // decoded by validate
	waitingOn *string // decoded by validate
}

// validate checks every present field and decodes the clearable ones.
func (p *taskPatch) validate() error {
	if p.Title != nil {
		if err := required("title", *p.Title); err != nil {
			return err
		}
	}
	if p.Status != nil {
		if err := oneOf("status", *p.Status, taskStatuses); err != nil {
			return err
		}
	}
	if p.CreatedAt != nil {
		if err := isoDate("createdAt", *p.CreatedAt); err != nil {
			return err
		}
	}
	if err := firstError(
		decodeNullable("projectId", "string", p.ProjectID, &p.projectID),
		decodeNullable("due", "string", p.Due, &p.due),
		decodeNullable("waitingOn", "string", p.WaitingOn, &p.waitingOn),
		optionalNonEmpty("projectId", p.projectID),
	); err != nil {
		return err
	}
	if p.due != nil {
		return isoDate("due", *p.due)
	}
	return nil
}

// apply copies the present fields onto the stored task.
func (p *taskPatch) apply(cur *store.TaskItem) error {
	if p.ProjectID != nil {
		cur.ProjectID = p.projectID
	}
	if p.Title != nil {
		cur.Title = *p.Title
	}
	if p.Details != nil {
		cur.Details = *p.Details
	}
	if p.Status != nil {
		cur.Status = *p.Status
	}
	if p.Due != nil {
		cur.Due = p.due
	}
	if p.WaitingOn != nil {
		cur.WaitingOn = p.waitingOn
	}
	if p.CreatedAt != nil {
		cur.CreatedAt = *p.CreatedAt
	}
	return nil
}

// notePatch is the PATCH notes/{nid} body.
//
// body is patchable but, like the create path, is not required to be
// non-empty: a note whose body has been emptied is still a note, and
// refusing that here would make the web UI stricter than the create route
// it mirrors.
type notePatch struct {
	Title     *string         `json:"title"`
	Body      *string         `json:"body"`
	ProjectID json.RawMessage `json:"projectId"`
	CreatedAt *string         `json:"createdAt"`

	projectID *string // decoded by validate
}

// validate checks every present field and decodes the clearable one.
func (p *notePatch) validate() error {
	if p.Title != nil {
		if err := required("title", *p.Title); err != nil {
			return err
		}
	}
	if p.CreatedAt != nil {
		if err := isoDate("createdAt", *p.CreatedAt); err != nil {
			return err
		}
	}
	return firstError(
		decodeNullable("projectId", "string", p.ProjectID, &p.projectID),
		optionalNonEmpty("projectId", p.projectID),
	)
}

// apply copies the present fields onto the stored note.
func (p *notePatch) apply(cur *store.NoteItem) error {
	if p.Title != nil {
		cur.Title = *p.Title
	}
	if p.Body != nil {
		cur.Body = *p.Body
	}
	if p.ProjectID != nil {
		cur.ProjectID = p.projectID
	}
	if p.CreatedAt != nil {
		cur.CreatedAt = *p.CreatedAt
	}
	return nil
}

// reminderPatch is the PATCH reminders/{rid} body.
type reminderPatch struct {
	Text *string `json:"text"`
	// See taskPatch.Details: empty is cleared, so this is not clearable in the
	// json.RawMessage sense.
	Details   *string         `json:"details"`
	RemindAt  *string         `json:"remindAt"`
	ProjectID json.RawMessage `json:"projectId"`
	Done      json.RawMessage `json:"done"`

	projectID *string // decoded by validate
	done      *bool   // decoded by validate
}

// validate checks every present field and decodes the clearable ones.
func (p *reminderPatch) validate() error {
	if p.Text != nil {
		if err := required("text", *p.Text); err != nil {
			return err
		}
	}
	if p.RemindAt != nil {
		if err := isoDateTime("remindAt", *p.RemindAt); err != nil {
			return err
		}
	}
	return firstError(
		decodeNullable("projectId", "string", p.ProjectID, &p.projectID),
		decodeNullable("done", "boolean", p.Done, &p.done),
		optionalNonEmpty("projectId", p.projectID),
	)
}

// apply copies the present fields onto the stored reminder.
func (p *reminderPatch) apply(cur *store.Reminder) error {
	if p.Text != nil {
		cur.Text = *p.Text
	}
	if p.Details != nil {
		cur.Details = *p.Details
	}
	if p.RemindAt != nil {
		cur.RemindAt = *p.RemindAt
	}
	if p.ProjectID != nil {
		cur.ProjectID = p.projectID
	}
	if p.Done != nil {
		cur.Done = p.done
	}
	return nil
}

// inboxPatch is the PATCH inbox/{iid} body.
type inboxPatch struct {
	Raw                *string         `json:"raw"`
	CapturedAt         *string         `json:"capturedAt"`
	SuggestedKind      *string         `json:"suggestedKind"`
	SuggestedProjectID json.RawMessage `json:"suggestedProjectId"`
	Status             *string         `json:"status"`

	suggestedProjectID *string // decoded by validate
}

// validate checks every present field and decodes the clearable ones.
func (p *inboxPatch) validate() error {
	if p.Raw != nil {
		if err := required("raw", *p.Raw); err != nil {
			return err
		}
	}
	if p.CapturedAt != nil {
		if err := isoDateTime("capturedAt", *p.CapturedAt); err != nil {
			return err
		}
	}
	if p.SuggestedKind != nil {
		if err := oneOf("suggestedKind", *p.SuggestedKind, itemKinds); err != nil {
			return err
		}
	}
	if p.Status != nil {
		if err := oneOf("status", *p.Status, inboxStatuses); err != nil {
			return err
		}
	}
	return firstError(
		decodeNullable("suggestedProjectId", "string", p.SuggestedProjectID, &p.suggestedProjectID),
		optionalNonEmpty("suggestedProjectId", p.suggestedProjectID),
	)
}

// apply copies the present fields onto the stored inbox item.
func (p *inboxPatch) apply(cur *store.InboxItem) error {
	if p.Raw != nil {
		cur.Raw = *p.Raw
	}
	if p.CapturedAt != nil {
		cur.CapturedAt = *p.CapturedAt
	}
	if p.SuggestedKind != nil {
		cur.SuggestedKind = *p.SuggestedKind
	}
	if p.SuggestedProjectID != nil {
		cur.SuggestedProjectID = p.suggestedProjectID
	}
	if p.Status != nil {
		cur.Status = *p.Status
	}
	return nil
}

// validateConversion checks a convert request: kind must be a known item
// kind, exactly the matching payload must be present, and the payload
// must pass its create validation.
func validateConversion(c store.Conversion) error {
	if err := oneOf("kind", c.Kind, itemKinds); err != nil {
		return err
	}
	present := map[string]bool{
		"task":     c.Task != nil,
		"note":     c.Note != nil,
		"reminder": c.Reminder != nil,
		"activity": c.Activity != nil,
		"project":  c.Project != nil,
	}
	n := 0
	for _, set := range present {
		if set {
			n++
		}
	}
	if !present[c.Kind] {
		return fmt.Errorf("kind %s requires a %s payload", c.Kind, c.Kind)
	}
	if n > 1 {
		return errors.New("only the payload matching kind may be set")
	}
	switch c.Kind {
	case "task":
		return validateTaskCreate(*c.Task)
	case "note":
		return validateNoteCreate(*c.Note)
	case "reminder":
		return validateReminderCreate(*c.Reminder)
	case "activity":
		return validateActivityCreate(*c.Activity)
	default:
		return validateProjectCreate(*c.Project)
	}
}
