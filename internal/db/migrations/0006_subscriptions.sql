-- Subscriptions: a search on a site checked again on a schedule for posts
-- newer than the last check. Each check is an ordinary search in the site's
-- queue (searches.subscription_id) that stops at the subscription's high_id;
-- when it's done its counts move here and the search row is removed.
CREATE TABLE subscriptions (
    id            INTEGER PRIMARY KEY,
    site_id       INTEGER NOT NULL REFERENCES sites (id),
    query         TEXT    NOT NULL,
    interval_s    INTEGER NOT NULL,                 -- time between checks
    paused        INTEGER NOT NULL DEFAULT 0,
    high_id       INTEGER NOT NULL DEFAULT 0,       -- newest post a finished check reached; 0 = never checked
    initial_max   INTEGER NOT NULL DEFAULT 0,       -- the first check stops after this many posts; 0 = all
    checks        INTEGER NOT NULL DEFAULT 0,       -- finished checks
    downloaded    INTEGER NOT NULL DEFAULT 0,       -- files downloaded, all checks
    last_new      INTEGER NOT NULL DEFAULT 0,       -- files downloaded by the last finished check
    error         TEXT,                             -- why the last check failed
    created_at    INTEGER NOT NULL,
    last_check_at INTEGER,                          -- when the last check finished (or failed)
    next_check_at INTEGER NOT NULL,                 -- when the next check is due
    UNIQUE (site_id, query)
) STRICT;

ALTER TABLE searches ADD COLUMN subscription_id INTEGER REFERENCES subscriptions (id) ON DELETE CASCADE;
ALTER TABLE searches ADD COLUMN stop_id INTEGER NOT NULL DEFAULT 0; -- done on reaching a post at or below this ID
CREATE INDEX searches_subscription ON searches (subscription_id);
