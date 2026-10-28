-- Lookups that read whole tables (2026-10-28, a speed audit):
-- A tag looked up by its name alone (a site's plain some_artist is the library's creator:some_artist when that's the
-- only tag by the name) read every tag - for every tag new to the library, nearly every post of a big first download
-- (56k tags on a 40k-file library).
CREATE INDEX tags_name ON tags (name);
-- A file removed for good (the trash emptied) clears its seen posts and favorites-list members: each read the whole
-- table (111k seen posts on that library: 1000 files took 7.9 s under the write lock, 0.19 s with the index).
CREATE INDEX seen_posts_file ON seen_posts (file_id) WHERE file_id IS NOT NULL;
CREATE INDEX fav_members_file ON fav_members (file_id);
