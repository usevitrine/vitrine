-- (2026-10-21) A plain site tag ("3d" from a site without tag types) goes to the one namespaced tag of that
-- name (meta:3d). Comics' tags share the tags table, and a gallery site's other:3d made the name look
-- ambiguous: a plain "3d" was made, and every file that site re-synced moved from meta:3d to it (its
-- modified time with it). Lookups now leave comic-only tags out; this puts the split tags back together:
-- a plain tag whose name has exactly one namespaced tag in the library and a comic-only twin. Files keep
-- their modified times (nothing changed for them in the end).
CREATE TEMP TABLE split AS
SELECT p.id AS plain_id, l.id AS lib_id
FROM tags p JOIN tags l ON l.name = p.name AND l.namespace_id != 0
WHERE p.namespace_id = 0
  AND EXISTS (SELECT 1 FROM file_tags WHERE tag_id = l.id)
  AND (SELECT count(*) FROM tags l2 WHERE l2.name = p.name AND l2.namespace_id != 0
       AND EXISTS (SELECT 1 FROM file_tags WHERE tag_id = l2.id)) = 1
  AND EXISTS (SELECT 1 FROM tags c WHERE c.name = p.name AND c.namespace_id != 0
       AND EXISTS (SELECT 1 FROM comic_tags WHERE tag_id = c.id)
       AND NOT EXISTS (SELECT 1 FROM file_tags WHERE tag_id = c.id))
  -- (one with parents or siblings of its own is left alone: the user may have meant it)
  AND NOT EXISTS (SELECT 1 FROM tag_parents WHERE child_id = p.id OR parent_id = p.id)
  AND NOT EXISTS (SELECT 1 FROM tag_siblings WHERE from_id = p.id OR to_id = p.id);

INSERT OR IGNORE INTO file_tags (file_id, tag_id, origin)
SELECT ft.file_id, s.lib_id, ft.origin FROM file_tags ft JOIN split s ON s.plain_id = ft.tag_id;
INSERT OR IGNORE INTO hidden_tags (file_id, tag_id)
SELECT h.file_id, s.lib_id FROM hidden_tags h JOIN split s ON s.plain_id = h.tag_id;
INSERT OR IGNORE INTO comic_tags (comic_id, tag_id, origin)
SELECT ct.comic_id, s.lib_id, ct.origin FROM comic_tags ct JOIN split s ON s.plain_id = ct.tag_id;
DELETE FROM tags WHERE id IN (SELECT plain_id FROM split);
DROP TABLE split;
