-- When the file was posted on its source site (the earliest, if several
-- sites have it). NULL = unknown (e.g. imported from disk).
ALTER TABLE files ADD COLUMN posted_at INTEGER;
