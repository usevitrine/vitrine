-- (2026-10-21) A failed post tried again (Retry) didn't record the search that met it as the file's "found by"
-- (a search's own download does). Added for the files retried so far; Retry records it from now on.
INSERT OR IGNORE INTO file_sources (file_id, site_id, query, subscription_id, downloaded, first_at)
SELECT sp.file_id, sp.site_id, sp.query,
       (SELECT s.id FROM subscriptions s WHERE s.site_id = sp.site_id AND s.query = sp.query),
       sp.outcome = 1, sp.seen_at
FROM seen_posts sp JOIN files f ON f.id = sp.file_id
WHERE sp.outcome IN (1, 2) AND sp.query IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM file_sources fs WHERE fs.file_id = sp.file_id AND fs.site_id = sp.site_id AND fs.query = sp.query);
