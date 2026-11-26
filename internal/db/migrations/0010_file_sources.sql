-- Which searches and subscriptions met each file ("Found by" in the details).
-- The search's text is kept, so it survives the search being removed.
CREATE TABLE file_sources (
    file_id         INTEGER NOT NULL REFERENCES files (id) ON DELETE CASCADE,
    site_id         INTEGER NOT NULL REFERENCES sites (id),
    query           TEXT    NOT NULL,
    subscription_id INTEGER REFERENCES subscriptions (id) ON DELETE SET NULL,
    downloaded      INTEGER NOT NULL,   -- 1 = this search downloaded it; 0 = it was in the library already
    first_at        INTEGER NOT NULL,
    PRIMARY KEY (file_id, site_id, query)
) STRICT, WITHOUT ROWID;
