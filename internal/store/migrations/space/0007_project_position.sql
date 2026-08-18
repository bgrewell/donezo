-- Manual project ordering.
--
-- position drives the order projects appear in the Projects list and the
-- timeline rail; ties fall back to rowid (insertion order), so before anyone
-- reorders, projects keep the order they were created in. Existing rows are
-- seeded from rowid to preserve their current order exactly; new projects are
-- appended past the current maximum (COALESCE(MAX(position)+1, 0)) by
-- CreateProject, and a drag rewrites the affected rows' positions.
ALTER TABLE projects ADD COLUMN position INTEGER NOT NULL DEFAULT 0;

UPDATE projects SET position = rowid;
