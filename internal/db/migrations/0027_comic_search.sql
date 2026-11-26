-- The Comics page searches like the library (2026-10-20): comics get
-- favorites and their own saved searches, and a gallery's category becomes a
-- tag (category:manga), so it's searched and filtered like any other.
CREATE TABLE comic_favorites (
    comic_id INTEGER PRIMARY KEY REFERENCES comics (id) ON DELETE CASCADE,
    added_at INTEGER NOT NULL           -- Unix ms
) STRICT;

ALTER TABLE saved_searches ADD COLUMN scope TEXT NOT NULL DEFAULT ''; -- '' = the library, 'comics' = the Comics page

INSERT OR IGNORE INTO namespaces (name)
    SELECT 'category' WHERE EXISTS (SELECT 1 FROM comics WHERE site_id IS NOT NULL AND trim(coalesce(category, '')) != '');
INSERT OR IGNORE INTO tags (namespace_id, name)
    SELECT DISTINCT (SELECT id FROM namespaces WHERE name = 'category'), replace(lower(trim(category)), ' ', '_')
    FROM comics WHERE site_id IS NOT NULL AND trim(coalesce(category, '')) != '';
INSERT OR IGNORE INTO comic_tags (comic_id, tag_id, origin)
    SELECT c.id, t.id, c.site_id FROM comics c
    JOIN tags t ON t.namespace_id = (SELECT id FROM namespaces WHERE name = 'category')
               AND t.name = replace(lower(trim(c.category)), ' ', '_')
    WHERE c.site_id IS NOT NULL AND trim(coalesce(c.category, '')) != '';
