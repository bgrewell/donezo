package api

import (
	"errors"
	"net/http"

	"github.com/bgrewell/donezo/internal/store"
)

// This file implements the space-content mutation endpoints. Every
// handler resolves {id} to a space the requester owns first (foreign and
// unknown spaces both read as 404) and requires it to be unarchived
// (writes into an archived space answer 409), and every response body is
// the stored entity in the same wire shape GET /api/spaces/{id}/state
// serves.

// writeStoreError maps store errors from entity mutations onto the API's
// canonical responses: unknown ids answer 404, duplicate ids 409, and
// broken project references a calm 400. Anything else is an internal
// fault: logged, and answered with an opaque 500.
func (s *Server) writeStoreError(w http.ResponseWriter, kind string, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, kind+" not found")
	case errors.Is(err, store.ErrDuplicateID):
		writeError(w, http.StatusConflict, kind+" id already exists")
	case errors.Is(err, store.ErrInvalidReference):
		writeError(w, http.StatusBadRequest, "projectId does not match an existing project")
	default:
		s.logger.Printf("%s mutation: %v", kind, err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

// ─── Projects ───────────────────────────────────────────────────────────

// handleCreateProject creates a project from a full frontend Project
// body (client-generated id included).
func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	sp, ok := s.ownedLiveSpace(w, r)
	if !ok {
		return
	}
	var p store.Project
	if !s.decodeBody(w, r, &p) {
		return
	}
	if err := validateProjectCreate(p); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	created, err := s.spaces.CreateProject(r.Context(), sp.ID, p)
	if err != nil {
		s.writeStoreError(w, "project", err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

// handlePatchProject applies a partial update to a project. Any subset
// of the mutable fields is accepted — including nextAction,
// altNextActions, resumeContext, status, and waitingOn, which the
// next-action lifecycle drives.
func (s *Server) handlePatchProject(w http.ResponseWriter, r *http.Request) {
	sp, ok := s.ownedLiveSpace(w, r)
	if !ok {
		return
	}
	var p projectPatch
	if !s.decodeBody(w, r, &p) {
		return
	}
	if err := p.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := s.spaces.PatchProject(r.Context(), sp.ID, r.PathValue("pid"), p.apply)
	if err != nil {
		s.writeStoreError(w, "project", err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// handleDeleteProject moves a project and everything it owns to the trash
// in one transaction, reporting the per-table counts the frontend shows in
// its confirmation aftermath.
//
// Since #16 nothing is destroyed here: the project and its activities,
// tasks and notes are marked with one delete batch, so restoring brings
// back exactly this delete and not a task the person had removed
// separately. Reminders and inbox items are left alone entirely — they
// reference the project rather than belong to it, and a trashed project is
// still there for the reference to point at, so they simply read as unfiled
// until it is restored. The detach those rows used to get happens at purge,
// which is the only point anything really goes.
func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	sp, ok := s.ownedLiveSpace(w, r)
	if !ok {
		return
	}
	deleted, err := s.spaces.SoftDeleteProject(r.Context(), sp.ID, r.PathValue("pid"))
	if err != nil {
		s.writeStoreError(w, "project", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]store.ProjectCascadeResult{"deleted": deleted})
}

// ─── Activities ─────────────────────────────────────────────────────────

// handleCreateActivity creates an activity entry.
func (s *Server) handleCreateActivity(w http.ResponseWriter, r *http.Request) {
	sp, ok := s.ownedLiveSpace(w, r)
	if !ok {
		return
	}
	var a store.ActivityEntry
	if !s.decodeBody(w, r, &a) {
		return
	}
	if err := validateActivityCreate(a); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	created, err := s.spaces.CreateActivity(r.Context(), sp.ID, a)
	if err != nil {
		s.writeStoreError(w, "activity", err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

// handlePatchActivity applies a partial update to an activity.
func (s *Server) handlePatchActivity(w http.ResponseWriter, r *http.Request) {
	sp, ok := s.ownedLiveSpace(w, r)
	if !ok {
		return
	}
	var p activityPatch
	if !s.decodeBody(w, r, &p) {
		return
	}
	if err := p.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := s.spaces.PatchActivity(r.Context(), sp.ID, r.PathValue("aid"), p.apply)
	if err != nil {
		s.writeStoreError(w, "activity", err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// handleDeleteActivity removes an activity.
func (s *Server) handleDeleteActivity(w http.ResponseWriter, r *http.Request) {
	sp, ok := s.ownedLiveSpace(w, r)
	if !ok {
		return
	}
	if err := s.spaces.DeleteActivity(r.Context(), sp.ID, r.PathValue("aid")); err != nil {
		s.writeStoreError(w, "activity", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ─── Tasks ──────────────────────────────────────────────────────────────

// handleCreateTask creates a task.
func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	sp, ok := s.ownedLiveSpace(w, r)
	if !ok {
		return
	}
	var t store.TaskItem
	if !s.decodeBody(w, r, &t) {
		return
	}
	if err := validateTaskCreate(t); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	created, err := s.spaces.CreateTask(r.Context(), sp.ID, t)
	if err != nil {
		s.writeStoreError(w, "task", err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

// handlePatchTask applies a partial update to a task.
func (s *Server) handlePatchTask(w http.ResponseWriter, r *http.Request) {
	sp, ok := s.ownedLiveSpace(w, r)
	if !ok {
		return
	}
	var p taskPatch
	if !s.decodeBody(w, r, &p) {
		return
	}
	if err := p.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := s.spaces.PatchTask(r.Context(), sp.ID, r.PathValue("tid"), p.apply)
	if err != nil {
		s.writeStoreError(w, "task", err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// ─── Notes ──────────────────────────────────────────────────────────────

// handleCreateNote creates a note.
func (s *Server) handleCreateNote(w http.ResponseWriter, r *http.Request) {
	sp, ok := s.ownedLiveSpace(w, r)
	if !ok {
		return
	}
	var n store.NoteItem
	if !s.decodeBody(w, r, &n) {
		return
	}
	if err := validateNoteCreate(n); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	created, err := s.spaces.CreateNote(r.Context(), sp.ID, n)
	if err != nil {
		s.writeStoreError(w, "note", err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

// handlePatchNote applies a partial update to a note.
func (s *Server) handlePatchNote(w http.ResponseWriter, r *http.Request) {
	sp, ok := s.ownedLiveSpace(w, r)
	if !ok {
		return
	}
	var p notePatch
	if !s.decodeBody(w, r, &p) {
		return
	}
	if err := p.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := s.spaces.PatchNote(r.Context(), sp.ID, r.PathValue("nid"), p.apply)
	if err != nil {
		s.writeStoreError(w, "note", err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// handleDeleteNote removes a note. A note owns nothing, so this is a plain
// delete rather than the typed-name cascade confirmation a project needs.
func (s *Server) handleDeleteNote(w http.ResponseWriter, r *http.Request) {
	sp, ok := s.ownedLiveSpace(w, r)
	if !ok {
		return
	}
	if err := s.spaces.DeleteNote(r.Context(), sp.ID, r.PathValue("nid")); err != nil {
		s.writeStoreError(w, "note", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ─── Reminders ──────────────────────────────────────────────────────────

// handleCreateReminder creates a reminder.
func (s *Server) handleCreateReminder(w http.ResponseWriter, r *http.Request) {
	sp, ok := s.ownedLiveSpace(w, r)
	if !ok {
		return
	}
	var rem store.Reminder
	if !s.decodeBody(w, r, &rem) {
		return
	}
	if err := validateReminderCreate(rem); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	created, err := s.spaces.CreateReminder(r.Context(), sp.ID, rem)
	if err != nil {
		s.writeStoreError(w, "reminder", err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

// handlePatchReminder applies a partial update to a reminder.
func (s *Server) handlePatchReminder(w http.ResponseWriter, r *http.Request) {
	sp, ok := s.ownedLiveSpace(w, r)
	if !ok {
		return
	}
	var p reminderPatch
	if !s.decodeBody(w, r, &p) {
		return
	}
	if err := p.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := s.spaces.PatchReminder(r.Context(), sp.ID, r.PathValue("rid"), p.apply)
	if err != nil {
		s.writeStoreError(w, "reminder", err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// ─── Inbox ──────────────────────────────────────────────────────────────

// handleCreateInboxItem captures a raw inbox item. This is also the
// cross-space capture path: the target is whichever owned space {id}
// names, not just the active one.
func (s *Server) handleCreateInboxItem(w http.ResponseWriter, r *http.Request) {
	sp, ok := s.ownedLiveSpace(w, r)
	if !ok {
		return
	}
	var it store.InboxItem
	if !s.decodeBody(w, r, &it) {
		return
	}
	if err := validateInboxCreate(it); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	created, err := s.spaces.CreateInboxItem(r.Context(), sp.ID, it)
	if err != nil {
		s.writeStoreError(w, "inbox item", err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

// handlePatchInboxItem applies a partial update to an inbox item.
func (s *Server) handlePatchInboxItem(w http.ResponseWriter, r *http.Request) {
	sp, ok := s.ownedLiveSpace(w, r)
	if !ok {
		return
	}
	var p inboxPatch
	if !s.decodeBody(w, r, &p) {
		return
	}
	if err := p.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := s.spaces.PatchInboxItem(r.Context(), sp.ID, r.PathValue("iid"), p.apply)
	if err != nil {
		s.writeStoreError(w, "inbox item", err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// handleConvertInboxItem atomically converts an inbox capture into a
// structured item, mirroring the frontend CONVERT_INBOX action: the mark
// and the insert commit together or not at all. The response carries the
// updated inbox item plus the created entity keyed by its kind.
func (s *Server) handleConvertInboxItem(w http.ResponseWriter, r *http.Request) {
	sp, ok := s.ownedLiveSpace(w, r)
	if !ok {
		return
	}
	var req struct {
		Kind     string               `json:"kind"`
		Task     *store.TaskItem      `json:"task"`
		Note     *store.NoteItem      `json:"note"`
		Reminder *store.Reminder      `json:"reminder"`
		Activity *store.ActivityEntry `json:"activity"`
		Project  *store.Project       `json:"project"`
	}
	if !s.decodeBody(w, r, &req) {
		return
	}
	conv := store.Conversion{
		Kind:     req.Kind,
		Task:     req.Task,
		Note:     req.Note,
		Reminder: req.Reminder,
		Activity: req.Activity,
		Project:  req.Project,
	}
	if err := validateConversion(conv); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	it, err := s.spaces.ConvertInboxItem(r.Context(), sp.ID, r.PathValue("iid"), conv)
	if err != nil {
		// A duplicate here is the created entity's id, not the inbox
		// item's; name the right one in the conflict message.
		if errors.Is(err, store.ErrDuplicateID) {
			writeError(w, http.StatusConflict, conv.Kind+" id already exists")
			return
		}
		s.writeStoreError(w, "inbox item", err)
		return
	}
	writeJSON(w, http.StatusOK, convertResponse(it, conv))
}

// convertResponse assembles the convert reply: the updated inbox item
// plus the created entity keyed by kind, so the frontend can reconcile
// both sides of the action from one response.
func convertResponse(it store.InboxItem, c store.Conversion) map[string]any {
	resp := map[string]any{"inbox": it}
	switch c.Kind {
	case "task":
		resp["task"] = *c.Task
	case "note":
		resp["note"] = *c.Note
	case "reminder":
		resp["reminder"] = *c.Reminder
	case "activity":
		resp["activity"] = *c.Activity
	case "project":
		resp["project"] = *c.Project
	}
	return resp
}

// noteTargetKinds are the kinds a note may become.
//
// Narrower than itemKinds on purpose. Note-to-note is an edit dressed up as a
// conversion, and note-to-project is not a sensible target — a note is a piece
// of content, not a stream of work, and someone wanting a project almost
// certainly wants a project with this note attached to it.
var noteTargetKinds = []string{"task", "reminder", "activity"}

// handleConvertNote turns a note into a task, reminder, or activity.
//
// Unlike the inbox route the source does not survive: an inbox capture is a
// log of what was captured and stays behind re-statused, whereas leaving the
// note would mean the same content exists twice. The store does both halves in
// one transaction, so a failure here leaves the note exactly where it was.
func (s *Server) handleConvertNote(w http.ResponseWriter, r *http.Request) {
	sp, ok := s.ownedLiveSpace(w, r)
	if !ok {
		return
	}
	var req struct {
		Kind     string               `json:"kind"`
		Task     *store.TaskItem      `json:"task"`
		Reminder *store.Reminder      `json:"reminder"`
		Activity *store.ActivityEntry `json:"activity"`
	}
	if !s.decodeBody(w, r, &req) {
		return
	}
	// Check the kind against the narrower set before the shared validator,
	// so "note" and "project" get a message naming what a note can become
	// rather than the generic unknown-kind one.
	if err := oneOf("kind", req.Kind, noteTargetKinds); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	conv := store.Conversion{
		Kind:     req.Kind,
		Task:     req.Task,
		Reminder: req.Reminder,
		Activity: req.Activity,
	}
	if err := validateConversion(conv); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	note, err := s.spaces.ConvertNote(r.Context(), sp.ID, r.PathValue("nid"), conv)
	if err != nil {
		// A duplicate here is the created entity's id, not the note's.
		if errors.Is(err, store.ErrDuplicateID) {
			writeError(w, http.StatusConflict, conv.Kind+" id already exists")
			return
		}
		s.writeStoreError(w, "note", err)
		return
	}
	resp := map[string]any{"note": note}
	switch conv.Kind {
	case "task":
		resp["task"] = *conv.Task
	case "reminder":
		resp["reminder"] = *conv.Reminder
	case "activity":
		resp["activity"] = *conv.Activity
	}
	writeJSON(w, http.StatusOK, resp)
}
