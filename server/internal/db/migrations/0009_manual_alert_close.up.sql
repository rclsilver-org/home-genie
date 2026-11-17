-- Snapshot the username so a manual closure remains attributable in history.
-- Additive migration: no table rebuild and no cascading deletion of messages.
ALTER TABLE alerts ADD COLUMN closed_by TEXT NOT NULL DEFAULT '';
-- Keep source identity separate from a reopened occurrence's local time.
ALTER TABLE alerts ADD COLUMN source_started_at TEXT NOT NULL DEFAULT '';
