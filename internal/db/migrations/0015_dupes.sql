-- Near-duplicates (internal/dupes): each file's perceptual hash (NULL = none:
-- not hashed yet, or a flat picture; flag FlagPHash = tried).
ALTER TABLE files ADD COLUMN phash INTEGER;

-- Pairs of files that look alike (a < b), found when the newer one was hashed.
-- status: 0 = to review, 1 = not duplicates (both kept). A pair whose file was
-- deleted (e.g. kept the other one) just stops being listed.
CREATE TABLE dupe_pairs (
    a        INTEGER NOT NULL REFERENCES files (id) ON DELETE CASCADE,
    b        INTEGER NOT NULL REFERENCES files (id) ON DELETE CASCADE,
    distance INTEGER NOT NULL,
    status   INTEGER NOT NULL DEFAULT 0,
    found_at INTEGER NOT NULL,
    merged   TEXT,                       -- what keeping one copied onto it (JSON, for undo)
    PRIMARY KEY (a, b)
) STRICT, WITHOUT ROWID;
CREATE INDEX dupe_pairs_b ON dupe_pairs (b);
