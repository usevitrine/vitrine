-- Comics (2026-10-20): a comic (a gallery: doujinshi, manga, an artist's CG
-- set) is its ordered pages, each a file in the store (dedupe, thumbnails),
-- with a title and its own tags. Comics live on their own Comics page: a
-- file that is only a comic page has the flag FlagComicOnly (files.flags,
-- 1 << 6) and is never in the library's grid or searches (nor in the search
-- index); downloaded as a library post too, it loses the flag.
CREATE TABLE comics (
    id         INTEGER PRIMARY KEY,
    title      TEXT    NOT NULL,
    title_alt  TEXT,                          -- the original-language title (e.g. Japanese), if the site has one
    site_id    INTEGER REFERENCES sites (id), -- where it came from (NULL: imported from a folder or archive)
    gallery    TEXT,                          -- the site's own key for it, e.g. "123456/0123abcd"
    url        TEXT,                          -- its page on the site
    category   TEXT,                          -- the site's category, e.g. "Manga"
    pages      INTEGER NOT NULL DEFAULT 0,    -- how many pages it has (on the site; or imported)
    cover      INTEGER REFERENCES files (id) ON DELETE SET NULL, -- the first page's file
    posted_at  INTEGER,                       -- when posted on its site
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,              -- pages or tags last changed
    read_page  INTEGER NOT NULL DEFAULT 0,    -- where reading stopped (0-based)
    read_at    INTEGER,                       -- when it was last read (NULL: never opened)
    deleted_at INTEGER,                       -- in the undo window (pages deleted with it)
    UNIQUE (site_id, gallery)
) STRICT;

CREATE TABLE comic_pages (
    comic_id INTEGER NOT NULL REFERENCES comics (id) ON DELETE CASCADE,
    page     INTEGER NOT NULL,                -- 0-based
    file_id  INTEGER NOT NULL REFERENCES files (id) ON DELETE CASCADE,
    PRIMARY KEY (comic_id, page)
) STRICT, WITHOUT ROWID;
CREATE INDEX comic_pages_file ON comic_pages (file_id);

-- Like file_tags: one row per origin (0 = the user, else a site).
CREATE TABLE comic_tags (
    comic_id INTEGER NOT NULL REFERENCES comics (id) ON DELETE CASCADE,
    tag_id   INTEGER NOT NULL REFERENCES tags (id) ON DELETE CASCADE,
    origin   INTEGER NOT NULL REFERENCES sites (id),
    PRIMARY KEY (comic_id, tag_id, origin)
) STRICT, WITHOUT ROWID;
CREATE INDEX comic_tags_tag ON comic_tags (tag_id);
