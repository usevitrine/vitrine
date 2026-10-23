-- The viewer's "I came" button (opt-in, Settings > Viewing; 2026-10-20): each
-- press is a row - the Rankings page counts them by period, file, tag.
CREATE TABLE climaxes (
    id      INTEGER PRIMARY KEY,
    file_id INTEGER NOT NULL REFERENCES files (id) ON DELETE CASCADE,
    at      INTEGER NOT NULL      -- Unix ms
) STRICT;
CREATE INDEX climaxes_at ON climaxes (at);
CREATE INDEX climaxes_file ON climaxes (file_id);
