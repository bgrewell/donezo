-- Space database initial schema. Mirrors web/src/domain/types.ts; columns
-- are snake_case versions of the frontend field names. JSON-array fields
-- (tags, links, alt_next_actions) are stored as JSON text. There is no
-- space_id column anywhere: the database file IS the space boundary.

CREATE TABLE projects (
    id               TEXT PRIMARY KEY,
    name             TEXT NOT NULL,
    color            TEXT NOT NULL,
    purpose          TEXT NOT NULL,
    outcome          TEXT NOT NULL,
    current_focus    TEXT NOT NULL,
    next_action      TEXT NOT NULL,
    alt_next_actions TEXT NOT NULL DEFAULT '[]',
    status           TEXT NOT NULL,
    resume_context   TEXT NOT NULL,
    waiting_on       TEXT,
    tags             TEXT NOT NULL DEFAULT '[]',
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL
);

CREATE TABLE activities (
    id           TEXT PRIMARY KEY,
    project_id   TEXT NOT NULL REFERENCES projects (id),
    date         TEXT NOT NULL,
    type         TEXT NOT NULL,
    title        TEXT NOT NULL,
    details      TEXT NOT NULL,
    effort_hours REAL,
    source       TEXT NOT NULL,
    tags         TEXT NOT NULL DEFAULT '[]',
    links        TEXT NOT NULL DEFAULT '[]',
    next_action  TEXT,
    planned      INTEGER,
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL
);

CREATE INDEX idx_activities_project_id ON activities (project_id);
CREATE INDEX idx_activities_date ON activities (date);

CREATE TABLE tasks (
    id         TEXT PRIMARY KEY,
    project_id TEXT REFERENCES projects (id),
    title      TEXT NOT NULL,
    status     TEXT NOT NULL,
    due        TEXT,
    waiting_on TEXT,
    created_at TEXT NOT NULL
);

CREATE INDEX idx_tasks_project_id ON tasks (project_id);

CREATE TABLE notes (
    id         TEXT PRIMARY KEY,
    project_id TEXT REFERENCES projects (id),
    title      TEXT NOT NULL,
    body       TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX idx_notes_project_id ON notes (project_id);

CREATE TABLE reminders (
    id         TEXT PRIMARY KEY,
    text       TEXT NOT NULL,
    remind_at  TEXT NOT NULL,
    project_id TEXT REFERENCES projects (id),
    done       INTEGER
);

CREATE INDEX idx_reminders_project_id ON reminders (project_id);

-- Inbox suggestions are soft references: a suggested project may not exist
-- yet (or ever), so suggested_project_id carries no foreign key.
CREATE TABLE inbox (
    id                   TEXT PRIMARY KEY,
    raw                  TEXT NOT NULL,
    captured_at          TEXT NOT NULL,
    suggested_kind       TEXT NOT NULL,
    suggested_project_id TEXT,
    status               TEXT NOT NULL
);

CREATE TABLE meta (
    key   TEXT PRIMARY KEY,
    value TEXT
);
