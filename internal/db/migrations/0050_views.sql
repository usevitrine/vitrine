-- View counts (2026-11-01): a file shown in the viewer (an image for 2 s, a video played for 3 s) is a view - one row
-- each, with how long it lasted, so counts can be taken for any period. Coming back to a file within 30 minutes adds
-- to the same view (its ms) instead of making another.
CREATE TABLE views (
    id      INTEGER PRIMARY KEY,
    file_id INTEGER NOT NULL REFERENCES files (id) ON DELETE CASCADE,
    at      INTEGER NOT NULL,           -- when it began (Unix ms)
    ms      INTEGER NOT NULL DEFAULT 0  -- how long: on screen (an image), played (a video)
) STRICT;
CREATE INDEX views_file ON views (file_id, at);
CREATE INDEX views_at ON views (at);
