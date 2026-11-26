-- What each site says a tag is (artist, character, ...; "tag" for general
-- ones), learned from posts. Plugins map types to namespaces. A row also means
-- "looked up": the downloader fills in types for files downloaded before they
-- were known, and '' marks a tag whose post the site no longer has.
CREATE TABLE tag_types (
    site_id    INTEGER NOT NULL REFERENCES sites (id),
    name       TEXT    NOT NULL,   -- the verbatim site tag
    type       TEXT    NOT NULL,
    checked_at INTEGER NOT NULL,
    PRIMARY KEY (site_id, name)
) STRICT, WITHOUT ROWID;
