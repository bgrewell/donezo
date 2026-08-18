-- A known catch-all project gives small chores a home.
--
-- Activities must belong to a project — an activity is a fact *about* something
-- — but ordinary small work (chores, errands, one-off fixes) belongs to no
-- thread worth calling a project. Rather than weaken that rule or make every
-- user invent a "Chores" project, donezo offers one it is aware of: a real
-- project flagged catchall = 1. It is created lazily on first use, so a space
-- that never needs one never shows an empty project.
--
-- The partial unique index enforces at most one *live* catch-all per space:
-- ordinary projects (catchall = 0) are unconstrained, and a trashed catch-all
-- (deleted_at set) does not block creating a fresh one. Existing rows default
-- to 0, so no space gains a catch-all until something is filed with no project.
ALTER TABLE projects ADD COLUMN catchall INTEGER NOT NULL DEFAULT 0;

CREATE UNIQUE INDEX idx_projects_catchall
  ON projects (catchall)
  WHERE catchall = 1 AND deleted_at IS NULL;
