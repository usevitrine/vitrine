-- A subscription's newest files (the Downloads page's pictures on its card) were found by joining every file it met
-- to its file for the date the site posted it, then sorting - all of a subscription's files, for each subscription:
-- 6-7 s on a library of ~600k files and 260 subscriptions (2026-10-31, the user: the Downloads page "with a large
-- amount of subs takes ages to load"). Now the date to sort by - the file's posted_at, else when the search met it,
-- as the query had it - is kept beside each source (sort_at) in an index per subscription, newest first: the newest
-- four are the first four live ones in it. Triggers keep it: set when a source is added, and again whenever a file's
-- posted_at changes (from the site, a merged duplicate...).
ALTER TABLE file_sources ADD COLUMN sort_at INTEGER NOT NULL DEFAULT 0;
UPDATE file_sources SET sort_at = coalesce((SELECT f.posted_at FROM files f WHERE f.id = file_sources.file_id), first_at);
CREATE INDEX file_sources_sub_recent ON file_sources (subscription_id, sort_at DESC, file_id DESC) WHERE subscription_id IS NOT NULL;

CREATE TRIGGER file_sources_sort_at AFTER INSERT ON file_sources BEGIN
    UPDATE file_sources SET sort_at = coalesce((SELECT f.posted_at FROM files f WHERE f.id = NEW.file_id), NEW.first_at)
    WHERE file_id = NEW.file_id AND site_id = NEW.site_id AND query = NEW.query;
END;

CREATE TRIGGER files_posted_sort_at AFTER UPDATE OF posted_at ON files BEGIN
    UPDATE file_sources SET sort_at = coalesce(NEW.posted_at, first_at) WHERE file_id = NEW.id;
END;
