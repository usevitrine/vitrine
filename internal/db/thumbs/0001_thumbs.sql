-- Thumbnails, one per file, in their own database: small blobs read faster
-- from SQLite than from separate files, and this file can be deleted and
-- regenerated at any time (it is not backed up).
CREATE TABLE thumbs (
    file_id INTEGER PRIMARY KEY,
    version INTEGER NOT NULL,   -- media.ThumbVersion that made it
    width   INTEGER NOT NULL,
    height  INTEGER NOT NULL,
    data    BLOB    NOT NULL    -- WebP
) STRICT;
