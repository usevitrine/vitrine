-- Favorites: a file and when it became one (sort:favorite orders by that).
CREATE TABLE favorites (
    file_id  INTEGER PRIMARY KEY REFERENCES files (id) ON DELETE CASCADE,
    added_at INTEGER NOT NULL           -- Unix ms
) STRICT;
