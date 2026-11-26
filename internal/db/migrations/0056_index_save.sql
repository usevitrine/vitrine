-- The saved tags (2026-11-14: index.SaveTags, library.SaveIndex): read at start instead of file_tags only while the
-- token saved with them is in meta. Anything that changes which files a tag has - a link added, changed or removed, a
-- site tag hidden or shown again, a file deleted, put back, purged or turned into a comic page - takes the token away,
-- whatever program does it (the server, the vitrine command, a repair at start). The next start reads file_tags then.
CREATE TRIGGER index_save_ft_insert AFTER INSERT ON file_tags BEGIN DELETE FROM meta WHERE key = 'index_save'; END;
CREATE TRIGGER index_save_ft_update AFTER UPDATE ON file_tags BEGIN DELETE FROM meta WHERE key = 'index_save'; END;
CREATE TRIGGER index_save_ft_delete AFTER DELETE ON file_tags BEGIN DELETE FROM meta WHERE key = 'index_save'; END;
CREATE TRIGGER index_save_hidden_insert AFTER INSERT ON hidden_tags BEGIN DELETE FROM meta WHERE key = 'index_save'; END;
CREATE TRIGGER index_save_hidden_update AFTER UPDATE ON hidden_tags BEGIN DELETE FROM meta WHERE key = 'index_save'; END;
CREATE TRIGGER index_save_hidden_delete AFTER DELETE ON hidden_tags BEGIN DELETE FROM meta WHERE key = 'index_save'; END;
-- (files: only what decides whether a file is in the library - deleted or not, a comic page (flag 64) or not; the other
-- flags change all the time as thumbnails are made)
CREATE TRIGGER index_save_files_deleted AFTER UPDATE OF deleted_at ON files WHEN OLD.deleted_at IS NOT NEW.deleted_at
BEGIN DELETE FROM meta WHERE key = 'index_save'; END;
CREATE TRIGGER index_save_files_comic AFTER UPDATE OF flags ON files WHEN (OLD.flags & 64) != (NEW.flags & 64)
BEGIN DELETE FROM meta WHERE key = 'index_save'; END;
CREATE TRIGGER index_save_files_delete AFTER DELETE ON files BEGIN DELETE FROM meta WHERE key = 'index_save'; END;
