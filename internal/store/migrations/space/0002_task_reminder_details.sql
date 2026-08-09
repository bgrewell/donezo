-- Tasks and reminders gain an optional details field, so the short form can
-- go back to being short. Activities and notes already had one (details and
-- body); with nowhere to put the long version, task titles and reminder text
-- grew into paragraphs and the lists stopped being scannable.
--
-- NOT NULL DEFAULT '' rather than nullable: an absent detail and an empty one
-- are the same thing, and a nullable column would make every reader decide
-- which it was looking at.

ALTER TABLE tasks ADD COLUMN details TEXT NOT NULL DEFAULT '';

ALTER TABLE reminders ADD COLUMN details TEXT NOT NULL DEFAULT '';
