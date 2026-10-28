-- Settings > Stats counted the tag links you added and the trash by scanning whole tables (16M tag links: 1.3 s).
-- Both are small parts of their tables: partial indexes make them instant.
CREATE INDEX file_tags_user ON file_tags (file_id) WHERE origin = 0;
CREATE INDEX files_deleted ON files (deleted_at) WHERE deleted_at IS NOT NULL;
