-- When each saved search or group (parent) tag was last opened from the saved
-- searches menu (2026-10-19): its NEW pill counts the files imported since, and
-- opening it marks those files NEW in the grid. key: "saved:<id>" or "tag:<tag>".
CREATE TABLE seen_marks (
    key     TEXT PRIMARY KEY,
    seen_at INTEGER NOT NULL    -- Unix ms
) STRICT;
