-- Recurring reminders: a reminder that re-reminds on a schedule until it is
-- marked done.
--
-- The motivating case is a nudge you can act on but keep forgetting ("add a
-- FIDO key to my account") — one reminder is easy to miss, so it should keep
-- coming back at an interval and stop only when the thing is actually done.
--
-- repeat_every / repeat_unit describe the interval. Both NULL is the existing
-- one-shot reminder — the only kind there was — so every row already in the
-- table stays one-shot with no backfill. A set pair (e.g. 1 + 'day') means the
-- dispatcher, having delivered the reminder, re-arms it for the next occurrence
-- instead of marking it finished. unit is 'hour', 'day', or 'week'; day and
-- week advance the wall clock rather than adding a fixed duration, so a daily
-- 2pm reminder stays at 2pm across a daylight-saving change.
ALTER TABLE reminders ADD COLUMN repeat_every INTEGER;
ALTER TABLE reminders ADD COLUMN repeat_unit TEXT;
