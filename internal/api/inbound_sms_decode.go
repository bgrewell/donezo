package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/bgrewell/donezo/internal/llm"
	"github.com/bgrewell/donezo/internal/store"
)

// decodedSMS is the JSON the decode-sms prompt returns.
type decodedSMS struct {
	Action   string                `json:"action"`
	Title    string                `json:"title"`
	Project  string                `json:"project"`
	RemindAt string                `json:"remind_at"`
	Due      string                `json:"due"`
	Repeat   *store.ReminderRepeat `json:"repeat"`
}

// projectRef is a project the decoder may name, with the space it lives in.
type projectRef struct {
	id      string
	name    string
	spaceID string
}

// decodeInboundSMS tries to turn a texted message into a reminder or task via
// the model. It returns a confirmation reply and true when it created
// something; ("", false) means "couldn't — capture the raw text instead".
//
// Any model, parse, or validation problem is a quiet false: an inbound text
// must never fail loudly or act on a half-understood command, so the worst case
// is always the safe fallback of capturing it to the inbox verbatim.
func (s *Server) decodeInboundSMS(ctx context.Context, user store.User, body string) (string, *pendingClarify, bool) {
	client := s.llmClient()
	if _, disabled := client.(llm.Disabled); disabled {
		return "", nil, false
	}
	// Cap what we send the model, matching the polish path. An over-long body
	// (a real SMS is far shorter) just falls back to raw capture.
	if err := llm.CheckInput(body); err != nil {
		return "", nil, false
	}
	prompt, ok := s.promptSet().ByID("decode-sms")
	if !ok {
		return "", nil, false
	}

	spaces, err := s.core.SpacesForUser(ctx, user.ID)
	if err != nil || len(spaces) == 0 {
		return "", nil, false
	}
	var refs []projectRef
	for _, sp := range spaces {
		projects, err := s.spaces.ListProjects(ctx, sp.ID)
		if err != nil {
			continue
		}
		for _, p := range projects {
			if p.Catchall {
				continue // reached via "no project", not by name
			}
			refs = append(refs, projectRef{id: p.ID, name: p.Name, spaceID: sp.ID})
		}
	}

	loc := s.userLocation(ctx, user.ID)
	input := buildDecodeInput(s.clock().In(loc), loc, refs, body)

	cctx, cancel := context.WithTimeout(ctx, llm.DefaultTimeout)
	defer cancel()
	raw, err := client.Complete(cctx, prompt.System(), input)
	if err != nil {
		s.logger.Printf("inbound sms: decode: %v", err)
		return "", nil, false
	}

	var d decodedSMS
	if err := json.Unmarshal([]byte(extractJSON(raw)), &d); err != nil {
		return "", nil, false
	}
	return s.actOnDecoded(ctx, user, loc, d, refs, body)
}

// actOnDecoded turns a decoded message into an action. It returns:
//   - (confirmation, nil, true)   — created something;
//   - (question, pending, true)   — needs the sender to say which project;
//   - ("", nil, false)            — couldn't; fall back to raw inbox capture.
//
// It never guesses: a reminder with no valid time, or a named project that
// doesn't exist and can't be clarified, defers to the fallback.
func (s *Server) actOnDecoded(
	ctx context.Context, user store.User, loc *time.Location,
	d decodedSMS, refs []projectRef, body string,
) (string, *pendingClarify, bool) {
	// Snooze targets the reminder they most recently received, not a project, so
	// it is resolved before the project/space machinery below.
	if d.Action == "snooze" {
		at, ok := normalizeRemindAt(d.RemindAt)
		if !ok {
			return "", nil, false
		}
		target, spaceID, ok := s.latestDeliveredReminder(ctx, user.ID)
		if !ok {
			return "", nil, false // nothing to snooze — capture raw instead
		}
		if err := s.spaces.RescheduleReminder(ctx, spaceID, target.ID, at); err != nil {
			s.logger.Printf("inbound sms: snooze: %v", err)
			return "", nil, false
		}
		return "Snoozed — I'll remind you again " + reminderWhen(at) + ": " + target.Text, nil, true
	}

	if d.Title = strings.TrimSpace(d.Title); d.Title == "" {
		d.Title = body
	}

	// Resolve a named project to one we actually have; don't invent one. When it
	// names one we lack but could offer alternatives, ask instead of guessing.
	var proj *projectRef
	if name := strings.TrimSpace(d.Project); name != "" {
		for i := range refs {
			if strings.EqualFold(refs[i].name, name) {
				proj = &refs[i]
				break
			}
		}
		if proj == nil {
			if len(refs) > 0 && (d.Action == "reminder" || d.Action == "task") {
				pending := &pendingClarify{userID: user.ID, action: d, options: refs, createdAt: s.clock()}
				return clarifyQuestion(d.Title, refs), pending, true
			}
			return "", nil, false
		}
	}

	reply, ok := s.completeAction(ctx, user, loc, proj, d)
	if !ok {
		return "", nil, false
	}
	return reply, nil, true
}

// completeAction creates the reminder or task described by d in the space that
// follows proj (or the user's default space when proj is nil), returning a
// confirmation. It is the shared tail of both the direct decode and a resolved
// clarification. false means the create could not be made and the caller should
// fall back.
func (s *Server) completeAction(
	ctx context.Context, user store.User, loc *time.Location, proj *projectRef, d decodedSMS,
) (string, bool) {
	var spaceID string
	var projectID *string
	if proj != nil {
		spaceID, projectID = proj.spaceID, &proj.id
	} else {
		def, err := s.core.FirstLiveSpace(ctx, user.ID)
		if err != nil {
			return "", false
		}
		spaceID = def.ID
	}

	switch d.Action {
	case "reminder":
		at, ok := normalizeRemindAt(d.RemindAt)
		if !ok {
			return "", false
		}
		id, err := store.NewID("rem")
		if err != nil {
			return "", false
		}
		rem := store.Reminder{ID: id, Text: d.Title, RemindAt: at, ProjectID: projectID}
		if d.Repeat != nil && d.Repeat.Validate() == nil {
			rem.Repeat = d.Repeat
		}
		if _, err := s.spaces.CreateReminder(ctx, spaceID, rem); err != nil {
			s.logger.Printf("inbound sms: create reminder: %v", err)
			return "", false
		}
		reply := "Reminder set — " + reminderWhen(at) + projectSuffix(proj) + ": " + d.Title
		if rem.Repeat != nil {
			reply += " (repeats)"
		}
		return reply, true

	case "task":
		var due *string
		if d.Due != "" {
			if _, err := time.Parse("2006-01-02", d.Due); err != nil {
				return "", false
			}
			v := d.Due
			due = &v
		}
		id, err := store.NewID("tsk")
		if err != nil {
			return "", false
		}
		t := store.TaskItem{
			ID: id, ProjectID: projectID, Title: d.Title, Status: "open",
			Due: due, CreatedAt: s.clock().In(loc).Format("2006-01-02"),
		}
		if _, err := s.spaces.CreateTask(ctx, spaceID, t); err != nil {
			s.logger.Printf("inbound sms: create task: %v", err)
			return "", false
		}
		reply := "Task added" + projectSuffix(proj) + ": " + d.Title
		if due != nil {
			reply += " (due " + *due + ")"
		}
		return reply, true

	default:
		return "", false // note / none — capture raw instead
	}
}

// latestDeliveredReminder finds, across a user's spaces, the reminder most
// recently delivered to them that is still live — the target of a snooze reply.
// notified_at is an ISO instant, so a lexicographic max is a chronological max.
func (s *Server) latestDeliveredReminder(ctx context.Context, userID int64) (store.Reminder, string, bool) {
	spaces, err := s.core.SpacesForUser(ctx, userID)
	if err != nil {
		return store.Reminder{}, "", false
	}
	var best store.Reminder
	var bestSpace, bestNotified string
	found := false
	for _, sp := range spaces {
		r, notifiedAt, ok, err := s.spaces.LatestDeliveredReminder(ctx, sp.ID)
		if err != nil || !ok {
			continue
		}
		if !found || notifiedAt > bestNotified {
			best, bestSpace, bestNotified, found = r, sp.ID, notifiedAt, true
		}
	}
	return best, bestSpace, found
}

// userLocation is the zone the message's times are read in: the user's own if
// set, otherwise the instance fallback.
func (s *Server) userLocation(ctx context.Context, userID int64) *time.Location {
	if settings, err := s.core.GetUserSettings(ctx, userID); err == nil && settings.Timezone != "" {
		if loc, err := time.LoadLocation(settings.Timezone); err == nil {
			return loc
		}
	}
	return s.location
}

// buildDecodeInput is the user-message half of the decode call: the context the
// model needs to resolve a relative time and match a project name.
func buildDecodeInput(now time.Time, loc *time.Location, refs []projectRef, body string) string {
	names := make([]string, 0, len(refs))
	for _, r := range refs {
		names = append(names, r.name)
	}
	projects := "none"
	if len(names) > 0 {
		projects = strings.Join(names, "; ")
	}
	return fmt.Sprintf("Current time: %s (%s), timezone %s.\nProjects: %s.\nMessage: %s",
		now.Format("2006-01-02T15:04"), now.Format("Mon"), loc.String(), projects, body)
}

// extractJSON pulls the JSON object out of a model reply, tolerating stray
// prose or ``` fences by taking the span from the first { to the last }.
func extractJSON(raw string) string {
	i := strings.IndexByte(raw, '{')
	j := strings.LastIndexByte(raw, '}')
	if i < 0 || j < i {
		return strings.TrimSpace(raw)
	}
	return raw[i : j+1]
}

// normalizeRemindAt parses the model's date-time (with or without seconds) and
// returns it in donezo's canonical naive-local form, or false if unparseable.
func normalizeRemindAt(s string) (string, bool) {
	s = strings.TrimSpace(s)
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("2006-01-02T15:04:05"), true
		}
	}
	return "", false
}

// reminderWhen renders a stored remind_at for the confirmation reply.
func reminderWhen(at string) string {
	if t, err := time.Parse("2006-01-02T15:04:05", at); err == nil {
		return t.Format("Mon Jan 2, 3:04pm")
	}
	return at
}

// projectSuffix names the destination project in a reply, or nothing.
func projectSuffix(p *projectRef) string {
	if p == nil {
		return ""
	}
	return " · " + p.name
}
