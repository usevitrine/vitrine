-- The sites a deleted file came from (2026-10-26, the user: removing a site can "completely wipe their server of any
-- hint that they ever had it enabled", deleted files' history too): ',a,b,' (plugin IDs), '' when it came from none
-- (imported), NULL when deleted before this was kept and its file is gone (it can't be told any more).
ALTER TABLE tombstones ADD COLUMN sites TEXT;

-- What's still in the trash can be told now.
UPDATE tombstones SET sites = coalesce((
    SELECT ',' || group_concat(s.name, ',') || ',' FROM sites s WHERE s.id != 0 AND s.id IN (
        SELECT fu.site_id FROM file_urls fu JOIN files f ON f.id = fu.file_id WHERE f.sha256 = tombstones.sha256
        UNION SELECT ft.origin FROM file_tags ft JOIN files f ON f.id = ft.file_id WHERE f.sha256 = tombstones.sha256
        UNION SELECT fs.site_id FROM file_sources fs JOIN files f ON f.id = fs.file_id WHERE f.sha256 = tombstones.sha256)
), '')
WHERE EXISTS (SELECT 1 FROM files f WHERE f.sha256 = tombstones.sha256);
