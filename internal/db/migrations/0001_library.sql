-- The library: files, tags, parents/siblings, URLs, tombstones.
-- Times are Unix milliseconds (UTC). Downloader tables come in a later migration.

-- Key/value settings kept by the server itself (e.g. the index generation).
CREATE TABLE meta (
    key   TEXT PRIMARY KEY,
    value ANY
) STRICT;
INSERT INTO meta (key, value) VALUES ('generation', 0);

-- Every file gets a dense, sequential ID that is never reused (AUTOINCREMENT),
-- so the in-memory index can address files by position.
CREATE TABLE files (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    sha256      BLOB    NOT NULL UNIQUE,
    md5         BLOB    NOT NULL,
    size        INTEGER NOT NULL,
    type        INTEGER NOT NULL,           -- media.Type
    codec       TEXT,                       -- NULL until probed
    width       INTEGER,
    height      INTEGER,
    duration_ms INTEGER,
    frames      INTEGER,
    has_audio   INTEGER,                    -- 0/1, NULL until probed
    imported_at INTEGER NOT NULL,
    modified_at INTEGER NOT NULL,
    deleted_at  INTEGER,                    -- set = in the undo window, hidden
    site_rating INTEGER,                    -- the site's rating (safe/questionable/explicit), not a tag
    flags       INTEGER NOT NULL DEFAULT 0,
    placeholder BLOB                        -- tiny preview for instant grids
) STRICT;
CREATE INDEX files_md5 ON files (md5);

-- Where a tag or URL came from. Origin 0 means the user; other IDs are sites.
CREATE TABLE sites (
    id   INTEGER PRIMARY KEY,
    name TEXT NOT NULL UNIQUE                -- plugin ID, e.g. "safebooru"
) STRICT;
INSERT INTO sites (id, name) VALUES (0, 'user');

-- Namespace 0 is "no namespace". Site tags are stored verbatim; a namespace
-- exists only where a plugin declares one.
CREATE TABLE namespaces (
    id   INTEGER PRIMARY KEY,
    name TEXT NOT NULL UNIQUE
) STRICT;
INSERT INTO namespaces (id, name) VALUES (0, '');

CREATE TABLE tags (
    id           INTEGER PRIMARY KEY,
    namespace_id INTEGER NOT NULL DEFAULT 0 REFERENCES namespaces (id),
    name         TEXT    NOT NULL,
    UNIQUE (namespace_id, name)
) STRICT;

-- A file has a tag if any origin gives it. Re-syncing a site's tags replaces
-- only that origin's rows. The reverse direction (tag -> files) lives in RAM.
CREATE TABLE file_tags (
    file_id INTEGER NOT NULL REFERENCES files (id) ON DELETE CASCADE,
    tag_id  INTEGER NOT NULL REFERENCES tags (id) ON DELETE CASCADE,
    origin  INTEGER NOT NULL REFERENCES sites (id),
    PRIMARY KEY (file_id, tag_id, origin)
) STRICT, WITHOUT ROWID;
CREATE INDEX file_tags_tag ON file_tags (tag_id);

-- Site tags the user removed, so a re-sync doesn't bring them back.
CREATE TABLE hidden_tags (
    file_id INTEGER NOT NULL REFERENCES files (id) ON DELETE CASCADE,
    tag_id  INTEGER NOT NULL REFERENCES tags (id) ON DELETE CASCADE,
    PRIMARY KEY (file_id, tag_id)
) STRICT, WITHOUT ROWID;

-- A parent is implied by its child (searching the parent finds the child's files).
CREATE TABLE tag_parents (
    child_id  INTEGER NOT NULL REFERENCES tags (id) ON DELETE CASCADE,
    parent_id INTEGER NOT NULL REFERENCES tags (id) ON DELETE CASCADE,
    PRIMARY KEY (child_id, parent_id),
    CHECK (child_id <> parent_id)
) STRICT, WITHOUT ROWID;
CREATE INDEX tag_parents_parent ON tag_parents (parent_id);

-- A sibling is replaced by its ideal tag; each tag has at most one.
CREATE TABLE tag_siblings (
    from_id INTEGER PRIMARY KEY REFERENCES tags (id) ON DELETE CASCADE,
    to_id   INTEGER NOT NULL REFERENCES tags (id) ON DELETE CASCADE,
    CHECK (from_id <> to_id)
) STRICT;
CREATE INDEX tag_siblings_to ON tag_siblings (to_id);

CREATE TABLE urls (
    id  INTEGER PRIMARY KEY,
    url TEXT NOT NULL UNIQUE
) STRICT;

-- kind: 1 = post page, 2 = source (e.g. the artist's original post), 3 = file URL.
CREATE TABLE file_urls (
    file_id INTEGER NOT NULL REFERENCES files (id) ON DELETE CASCADE,
    url_id  INTEGER NOT NULL REFERENCES urls (id),
    kind    INTEGER NOT NULL,
    site_id INTEGER NOT NULL DEFAULT 0 REFERENCES sites (id),
    PRIMARY KEY (file_id, url_id, kind)
) STRICT, WITHOUT ROWID;
CREATE INDEX file_urls_url ON file_urls (url_id);

-- Deleted files are never downloaded or imported again.
CREATE TABLE tombstones (
    sha256     BLOB PRIMARY KEY,
    md5        BLOB,
    deleted_at INTEGER NOT NULL,
    reason     TEXT
) STRICT, WITHOUT ROWID;
CREATE INDEX tombstones_md5 ON tombstones (md5);
