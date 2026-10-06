-- When things were finished.
--
-- Until now a task was "done" with no record of when, and a project was
-- "completed" with only updated_at to go on — which moves on every later edit.
-- That is enough to draw a list but not to answer "what did I get done last
-- week?", which is what work summaries need.
--
-- completed_at is an RFC 3339 UTC instant. The store stamps it the moment a
-- task's status becomes done (or a project's becomes completed) and clears it
-- if the item is reopened, so no caller has to remember to.
--
-- completed_source says how much to trust it:
--   recorded  stamped by the store at the moment of completion
--   inferred  reconstructed for rows completed before this column existed
--             (see the Go step that runs with this migration, backfill.go)
--   manual    set or corrected by the person
-- It is NULL exactly when completed_at is.
ALTER TABLE tasks    ADD COLUMN completed_at     TEXT;
ALTER TABLE tasks    ADD COLUMN completed_source TEXT;
ALTER TABLE projects ADD COLUMN completed_at     TEXT;
ALTER TABLE projects ADD COLUMN completed_source TEXT;

-- The task an activity records the completion of, when it was logged from a
-- check-off. A soft reference like inbox.suggested_project_id: a task can be
-- purged while the activity — the record of the work — lives on.
ALTER TABLE activities ADD COLUMN task_id TEXT;

CREATE INDEX idx_tasks_completed_at ON tasks (completed_at);
CREATE INDEX idx_activities_task_id ON activities (task_id);

-- Every project status change, as it happens. Append-only and written by the
-- store; there is no history before this migration and none is invented, so
-- the log is exactly as complete as it claims to be. It is what lets a
-- summary say a project was blocked for part of a period, which the current
-- status alone cannot.
CREATE TABLE project_status_changes (
    id          INTEGER PRIMARY KEY,
    project_id  TEXT NOT NULL REFERENCES projects (id),
    from_status TEXT,
    to_status   TEXT NOT NULL,
    changed_at  TEXT NOT NULL
);

CREATE INDEX idx_project_status_changes_project_id ON project_status_changes (project_id);
