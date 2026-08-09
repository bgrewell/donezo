-- Soft delete (#16). Deleting stops being irreversible: a row is marked and
-- disappears from every read, and can be restored or purged from a trash.
--
-- deleted_at is the marker and the clock. NULL means live, and it is the only
-- state a row has had until now, so every existing row is already correct.
--
-- delete_batch groups the rows one cascade removed together. Without it,
-- restoring a project could not tell its own children from a task the person
-- had deleted separately a week earlier — restoring would quietly resurrect
-- that too. Rows deleted on their own get a batch of their own, so restore is
-- the same operation either way.
--
-- Note what is NOT here: the detachment bookkeeping a soft cascade would seem
-- to need. Deleting a project used to null the project_id on its reminders and
-- inbox items, purely because the foreign key would not survive the row going
-- away. A marked project is still there, so those references stay valid and
-- restore is just unmarking the batch. The detach still happens, but only at
-- purge, when the row really does go.

ALTER TABLE projects   ADD COLUMN deleted_at   TEXT;
ALTER TABLE projects   ADD COLUMN delete_batch TEXT;
ALTER TABLE activities ADD COLUMN deleted_at   TEXT;
ALTER TABLE activities ADD COLUMN delete_batch TEXT;
ALTER TABLE tasks      ADD COLUMN deleted_at   TEXT;
ALTER TABLE tasks      ADD COLUMN delete_batch TEXT;
ALTER TABLE notes      ADD COLUMN deleted_at   TEXT;
ALTER TABLE notes      ADD COLUMN delete_batch TEXT;
ALTER TABLE reminders  ADD COLUMN deleted_at   TEXT;
ALTER TABLE reminders  ADD COLUMN delete_batch TEXT;
ALTER TABLE inbox      ADD COLUMN deleted_at   TEXT;
ALTER TABLE inbox      ADD COLUMN delete_batch TEXT;

-- Every list read filters on deleted_at, so it is worth an index on the
-- tables that carry the most rows.
CREATE INDEX idx_activities_deleted_at ON activities (deleted_at);
CREATE INDEX idx_tasks_deleted_at      ON tasks (deleted_at);
CREATE INDEX idx_notes_deleted_at      ON notes (deleted_at);
