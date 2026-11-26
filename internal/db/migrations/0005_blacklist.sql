-- The download blacklist: a post with any of these tags is not downloaded
-- (and is remembered as seen, so running a search again doesn't fetch it).
CREATE TABLE blacklist (
    pattern  TEXT    PRIMARY KEY,   -- a tag, or a pattern with '*' (e.g. ai_*)
    added_at INTEGER NOT NULL
) STRICT, WITHOUT ROWID;

-- Posts a search skipped because of the blacklist (seen_posts.outcome 6).
ALTER TABLE searches ADD COLUMN blacklisted INTEGER NOT NULL DEFAULT 0;
