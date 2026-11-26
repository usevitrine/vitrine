-- Recent searches: the last searches run on each page (the library, Comics, Comics > Browse), newest first, kept on
-- the server so every device has the same ones. One row per search and order: running it again moves it to the top.
CREATE TABLE recent_searches (
    scope   TEXT    NOT NULL,      -- '' = the library, 'comics', 'browse' (as saved_searches)
    query   TEXT    NOT NULL,      -- as the search bar wrote it (without sort)
    sort    TEXT    NOT NULL,      -- '' = the page's default order
    used_at INTEGER NOT NULL,      -- unix ms, last run
    PRIMARY KEY (scope, query, sort)
) STRICT;
CREATE INDEX recent_searches_used ON recent_searches (scope, used_at DESC);
