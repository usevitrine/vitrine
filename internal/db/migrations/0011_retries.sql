-- Automatic retries of failed searches: how many times in a row it failed,
-- and when it is tried again (NULL = not automatically).
ALTER TABLE searches ADD COLUMN attempts INTEGER NOT NULL DEFAULT 0;
ALTER TABLE searches ADD COLUMN retry_at INTEGER;
