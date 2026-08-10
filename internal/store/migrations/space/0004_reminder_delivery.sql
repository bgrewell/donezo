-- Delivery bookkeeping for reminders (#52).
--
-- notified_at is what makes delivery at-most-once. The dispatcher wakes every
-- minute and asks for reminders that are due; without a mark, every one of
-- them would be due again a minute later, and a reminder to put the bins out
-- would arrive once a minute until somebody ticked it off.
--
-- It is set only after a channel actually accepted the message, so a relay
-- that is refusing connections leaves the reminder unsent and it goes out on
-- a later pass rather than being silently swallowed. That ordering is why
-- attempts exists too: something permanently undeliverable (a number that was
-- correct in the settings form and is not a phone) would otherwise be retried
-- every minute forever, and the log would be nothing else.
--
-- NULL means "not delivered", which is what every existing reminder is —
-- including ones already in the past. That is deliberate and it is why the
-- dispatcher has a lateness bound: switching this on must not mail out a
-- year of history.
ALTER TABLE reminders ADD COLUMN notified_at TEXT;
ALTER TABLE reminders ADD COLUMN notify_attempts INTEGER NOT NULL DEFAULT 0;

-- The dispatcher's own query: undelivered reminders, every pass.
CREATE INDEX idx_reminders_notified_at ON reminders (notified_at);
