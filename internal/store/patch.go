package store

import (
	"context"
	"fmt"
)

// This file holds the read-modify-write mutations behind the PATCH and
// convert API endpoints. Each Patch* method loads the row, applies the
// caller's mutation, and rewrites it inside one transaction; combined with
// the single-connection pool per space file, concurrent patches serialize
// instead of clobbering each other's fields.

// EnsureSpace opens — creating and migrating if needed — the space's
// database file, so a newly registered space exists on disk immediately
// rather than on first content write.
func (s *SpaceStore) EnsureSpace(ctx context.Context, spaceID string) error {
	_, err := s.db(ctx, spaceID)
	return err
}

// PatchProject atomically applies apply to an existing project and
// rewrites it, refreshing UpdatedAt. The load, mutation, and write run in
// one transaction. Returns ErrNotFound if the id does not exist; a non-nil
// error from apply aborts the patch and is returned unchanged.
func (s *SpaceStore) PatchProject(ctx context.Context, spaceID, id string, apply func(*Project) error) (Project, error) {
	if err := requireID("project", id); err != nil {
		return Project{}, err
	}
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return Project{}, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Project{}, fmt.Errorf("store: patch project %q: begin: %w", id, err)
	}
	defer rollbackQuietly(tx)
	p, err := getProjectRow(ctx, tx, id)
	if err != nil {
		return Project{}, err
	}
	if err := apply(&p); err != nil {
		return Project{}, err
	}
	p.ID = id
	if p.AltNextActions == nil {
		p.AltNextActions = []string{}
	}
	if p.Tags == nil {
		p.Tags = []string{}
	}
	p.UpdatedAt = s.opts.now()
	if _, err := execUpdateProject(ctx, tx, p); err != nil {
		return Project{}, fmt.Errorf("store: patch project %q: %w", id, classifyConstraint(err))
	}
	if err := tx.Commit(); err != nil {
		return Project{}, fmt.Errorf("store: patch project %q: commit: %w", id, err)
	}
	return p, nil
}

// PatchActivity atomically applies apply to an existing activity and
// rewrites it, refreshing UpdatedAt. The load, mutation, and write run in
// one transaction. Returns ErrNotFound if the id does not exist; a non-nil
// error from apply aborts the patch and is returned unchanged.
func (s *SpaceStore) PatchActivity(ctx context.Context, spaceID, id string, apply func(*ActivityEntry) error) (ActivityEntry, error) {
	if err := requireID("activity", id); err != nil {
		return ActivityEntry{}, err
	}
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return ActivityEntry{}, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return ActivityEntry{}, fmt.Errorf("store: patch activity %q: begin: %w", id, err)
	}
	defer rollbackQuietly(tx)
	a, err := getActivityRow(ctx, tx, id)
	if err != nil {
		return ActivityEntry{}, err
	}
	if err := apply(&a); err != nil {
		return ActivityEntry{}, err
	}
	a.ID = id
	if a.Tags == nil {
		a.Tags = []string{}
	}
	if a.Links == nil {
		a.Links = []ActivityLink{}
	}
	a.UpdatedAt = s.opts.now()
	if _, err := execUpdateActivity(ctx, tx, a); err != nil {
		return ActivityEntry{}, fmt.Errorf("store: patch activity %q: %w", id, classifyConstraint(err))
	}
	if err := tx.Commit(); err != nil {
		return ActivityEntry{}, fmt.Errorf("store: patch activity %q: commit: %w", id, err)
	}
	return a, nil
}

// PatchTask atomically applies apply to an existing task and rewrites it.
// The load, mutation, and write run in one transaction. Returns
// ErrNotFound if the id does not exist; a non-nil error from apply aborts
// the patch and is returned unchanged.
func (s *SpaceStore) PatchTask(ctx context.Context, spaceID, id string, apply func(*TaskItem) error) (TaskItem, error) {
	if err := requireID("task", id); err != nil {
		return TaskItem{}, err
	}
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return TaskItem{}, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return TaskItem{}, fmt.Errorf("store: patch task %q: begin: %w", id, err)
	}
	defer rollbackQuietly(tx)
	t, err := getTaskRow(ctx, tx, id)
	if err != nil {
		return TaskItem{}, err
	}
	if err := apply(&t); err != nil {
		return TaskItem{}, err
	}
	t.ID = id
	if _, err := execUpdateTask(ctx, tx, t); err != nil {
		return TaskItem{}, fmt.Errorf("store: patch task %q: %w", id, classifyConstraint(err))
	}
	if err := tx.Commit(); err != nil {
		return TaskItem{}, fmt.Errorf("store: patch task %q: commit: %w", id, err)
	}
	return t, nil
}

// PatchReminder atomically applies apply to an existing reminder and
// rewrites it. The load, mutation, and write run in one transaction.
// Returns ErrNotFound if the id does not exist; a non-nil error from apply
// aborts the patch and is returned unchanged.
func (s *SpaceStore) PatchReminder(ctx context.Context, spaceID, id string, apply func(*Reminder) error) (Reminder, error) {
	if err := requireID("reminder", id); err != nil {
		return Reminder{}, err
	}
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return Reminder{}, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Reminder{}, fmt.Errorf("store: patch reminder %q: begin: %w", id, err)
	}
	defer rollbackQuietly(tx)
	r, err := getReminderRow(ctx, tx, id)
	if err != nil {
		return Reminder{}, err
	}
	if err := apply(&r); err != nil {
		return Reminder{}, err
	}
	r.ID = id
	if _, err := execUpdateReminder(ctx, tx, r); err != nil {
		return Reminder{}, fmt.Errorf("store: patch reminder %q: %w", id, classifyConstraint(err))
	}
	if err := tx.Commit(); err != nil {
		return Reminder{}, fmt.Errorf("store: patch reminder %q: commit: %w", id, err)
	}
	return r, nil
}

// PatchInboxItem atomically applies apply to an existing inbox item and
// rewrites it. The load, mutation, and write run in one transaction.
// Returns ErrNotFound if the id does not exist; a non-nil error from apply
// aborts the patch and is returned unchanged.
func (s *SpaceStore) PatchInboxItem(ctx context.Context, spaceID, id string, apply func(*InboxItem) error) (InboxItem, error) {
	if err := requireID("inbox item", id); err != nil {
		return InboxItem{}, err
	}
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return InboxItem{}, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return InboxItem{}, fmt.Errorf("store: patch inbox item %q: begin: %w", id, err)
	}
	defer rollbackQuietly(tx)
	it, err := getInboxRow(ctx, tx, id)
	if err != nil {
		return InboxItem{}, err
	}
	if err := apply(&it); err != nil {
		return InboxItem{}, err
	}
	it.ID = id
	if _, err := execUpdateInbox(ctx, tx, it); err != nil {
		return InboxItem{}, fmt.Errorf("store: patch inbox item %q: %w", id, classifyConstraint(err))
	}
	if err := tx.Commit(); err != nil {
		return InboxItem{}, fmt.Errorf("store: patch inbox item %q: commit: %w", id, err)
	}
	return it, nil
}

// Conversion describes the structured item an inbox capture becomes,
// mirroring the frontend CONVERT_INBOX action: Kind names the target
// entity and exactly the matching payload pointer must be set.
type Conversion struct {
	// Kind is the target entity: task, note, reminder, activity, or
	// project.
	Kind string
	// Task is the created task when Kind is "task".
	Task *TaskItem
	// Note is the created note when Kind is "note".
	Note *NoteItem
	// Reminder is the created reminder when Kind is "reminder".
	Reminder *Reminder
	// Activity is the created activity when Kind is "activity".
	Activity *ActivityEntry
	// Project is the created project when Kind is "project".
	Project *Project
}

// validate checks that Kind names a known item kind and that exactly the
// matching payload is set.
func (c Conversion) validate() error {
	present := map[string]bool{
		"task":     c.Task != nil,
		"note":     c.Note != nil,
		"reminder": c.Reminder != nil,
		"activity": c.Activity != nil,
		"project":  c.Project != nil,
	}
	matched, known := present[c.Kind]
	if !known {
		return fmt.Errorf("store: unknown conversion kind %q", c.Kind)
	}
	if !matched {
		return fmt.Errorf("store: conversion kind %q requires a matching payload", c.Kind)
	}
	n := 0
	for _, set := range present {
		if set {
			n++
		}
	}
	if n > 1 {
		return fmt.Errorf("store: conversion kind %q carries an extra payload of another kind", c.Kind)
	}
	return nil
}

// ConvertInboxItem atomically converts an inbox capture into a structured
// item, mirroring the frontend CONVERT_INBOX action: in one transaction
// the inbox item's status becomes "converted" and the item described by
// conv is inserted. Any failure — unknown inbox id, duplicate target id,
// broken project reference — rolls the whole conversion back. Returns the
// updated inbox item, or ErrNotFound if the inbox id does not exist.
func (s *SpaceStore) ConvertInboxItem(ctx context.Context, spaceID, id string, conv Conversion) (InboxItem, error) {
	if err := requireID("inbox item", id); err != nil {
		return InboxItem{}, err
	}
	if err := conv.validate(); err != nil {
		return InboxItem{}, err
	}
	db, err := s.db(ctx, spaceID)
	if err != nil {
		return InboxItem{}, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return InboxItem{}, fmt.Errorf("store: convert inbox item %q: begin: %w", id, err)
	}
	defer rollbackQuietly(tx)
	it, err := getInboxRow(ctx, tx, id)
	if err != nil {
		return InboxItem{}, err
	}
	it.Status = "converted"
	if _, err := execUpdateInbox(ctx, tx, it); err != nil {
		return InboxItem{}, fmt.Errorf("store: convert inbox item %q: %w", id, classifyConstraint(err))
	}
	switch conv.Kind {
	case "task":
		_, err = insertTask(ctx, tx, *conv.Task)
	case "note":
		_, err = insertNote(ctx, tx, *conv.Note)
	case "reminder":
		_, err = insertReminder(ctx, tx, *conv.Reminder)
	case "activity":
		_, err = s.insertActivity(ctx, tx, *conv.Activity)
	case "project":
		_, err = s.insertProject(ctx, tx, *conv.Project)
	}
	if err != nil {
		return InboxItem{}, err
	}
	if err := tx.Commit(); err != nil {
		return InboxItem{}, fmt.Errorf("store: convert inbox item %q: commit: %w", id, err)
	}
	return it, nil
}
