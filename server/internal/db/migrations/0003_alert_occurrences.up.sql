-- How many times Alertmanager has delivered this alert.
--
-- A silent refresh is invisible today: an alert that has fired once and one
-- that has fired forty times look identical, though they call for different
-- reactions — the second is flapping, or a condition nobody is fixing.
-- Counting them costs one column and turns the dedup we already do into
-- information rather than silence.
ALTER TABLE alerts ADD COLUMN occurrences INTEGER NOT NULL DEFAULT 1;
