package store

// ProjectCascadeResult reports, per table, what one project delete moved to
// the trash. Field names marshal to the wire shape the frontend shows in its
// confirmation aftermath.
//
// It no longer carries detached counts. Deleting a project used to null the
// project link on its reminders and inbox items, because the foreign key
// would not survive the row going away; a trashed project is still there, so
// those references stay intact and come back with a restore. The detach now
// happens only at purge — see purgeWhere in trash.go — where nothing is
// waiting to hear about it.
type ProjectCascadeResult struct {
	// Project is 1: the project row itself.
	Project int64 `json:"project"`
	// Activities is the count of activity rows moved to the trash.
	Activities int64 `json:"activities"`
	// Tasks is the count of task rows moved to the trash.
	Tasks int64 `json:"tasks"`
	// Notes is the count of note rows moved to the trash.
	Notes int64 `json:"notes"`
}
