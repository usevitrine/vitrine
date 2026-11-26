-- Collections whose comics were all deleted for good stayed behind (2026-10-23; switching comics off left them too):
-- gone now, and from now on when their last comic is purged.
DELETE FROM collections WHERE NOT EXISTS (SELECT 1 FROM comics c WHERE c.collection_id = collections.id);
