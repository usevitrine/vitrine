-- "I came" in the comic reader (2026-10-23): a press there names the comic and its page (0-based) as well as the
-- page's file, so the Rankings page can show that exact page.
ALTER TABLE climaxes ADD COLUMN comic_id INTEGER REFERENCES comics (id) ON DELETE CASCADE;
ALTER TABLE climaxes ADD COLUMN page INTEGER;
CREATE INDEX climaxes_comic ON climaxes (comic_id, page) WHERE comic_id IS NOT NULL;
