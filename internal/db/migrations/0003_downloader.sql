-- The downloader: searches (a query on one site, worked through newest post
-- first) and a record of every post handled.

-- A search resumes exactly: low_id is the lowest post ID fully handled, and
-- the next request asks the site for posts below it (gelbooru's id:<N), so
-- new uploads can't shift pages and a crash re-fetches at most one file.
CREATE TABLE searches (
    id          INTEGER PRIMARY KEY,
    site_id     INTEGER NOT NULL REFERENCES sites (id),
    query       TEXT    NOT NULL,
    state       TEXT    NOT NULL DEFAULT 'queued',  -- queued, running, paused, done, failed
    position    INTEGER NOT NULL,                   -- queue order within the site
    max_posts   INTEGER NOT NULL DEFAULT 0,         -- stop after this many posts; 0 = all
    low_id      INTEGER NOT NULL DEFAULT 0,         -- resume below this post ID; 0 = from the newest
    high_id     INTEGER NOT NULL DEFAULT 0,         -- newest post ID seen (for subscriptions)
    site_total  INTEGER NOT NULL DEFAULT -1,        -- matches on the site when the search started
    handled     INTEGER NOT NULL DEFAULT 0,         -- posts handled, any outcome
    downloaded  INTEGER NOT NULL DEFAULT 0,
    already     INTEGER NOT NULL DEFAULT 0,         -- in the library already (maybe from another site)
    deleted     INTEGER NOT NULL DEFAULT 0,         -- deleted before: not downloaded again
    skipped     INTEGER NOT NULL DEFAULT 0,         -- unsupported file type
    failed      INTEGER NOT NULL DEFAULT 0,
    error       TEXT,                               -- why the search stopped, if it failed
    created_at  INTEGER NOT NULL,
    started_at  INTEGER,
    finished_at INTEGER
) STRICT;
CREATE INDEX searches_queue ON searches (site_id, state, position);

-- Written after every post, never only at the end of a run.
CREATE TABLE seen_posts (
    site_id INTEGER NOT NULL REFERENCES sites (id),
    post_id INTEGER NOT NULL,
    file_id INTEGER REFERENCES files (id) ON DELETE SET NULL,
    outcome INTEGER NOT NULL,   -- 1 downloaded, 2 already had, 3 deleted before, 4 failed, 5 skipped
    seen_at INTEGER NOT NULL,
    PRIMARY KEY (site_id, post_id)
) STRICT, WITHOUT ROWID;
