-- Kept over restarts (2026-10-21): the bell's notices (the downloader's events: the last 100) and the sites paused
-- because they kept failing to answer (their next try and how many tries failed).
CREATE TABLE notices (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    at           INTEGER NOT NULL, -- Unix ms
    kind         TEXT    NOT NULL,
    site         TEXT    NOT NULL DEFAULT '',
    query        TEXT    NOT NULL DEFAULT '',
    subscription INTEGER NOT NULL DEFAULT 0,
    new          INTEGER NOT NULL DEFAULT 0,
    error        TEXT    NOT NULL DEFAULT '',
    tag          TEXT    NOT NULL DEFAULT '',
    first        INTEGER NOT NULL DEFAULT 0
) STRICT;

CREATE TABLE site_pauses (
    site   TEXT    PRIMARY KEY, -- plugin ID
    since  INTEGER NOT NULL,    -- Unix ms
    until  INTEGER NOT NULL,    -- the next try, Unix ms
    reason TEXT    NOT NULL,
    tries  INTEGER NOT NULL
) STRICT;
