-- A failed post keeps why it failed and the search that met it (2026-10-20), so
-- Downloads can list failed files and try them again one by one.
ALTER TABLE seen_posts ADD COLUMN error TEXT;
ALTER TABLE seen_posts ADD COLUMN query TEXT;
CREATE INDEX seen_posts_failed ON seen_posts (seen_at) WHERE outcome = 4;
