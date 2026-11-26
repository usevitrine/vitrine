-- A tag the user moved to another namespace (2026-10-19): what a site calls it
-- (its namespace there) maps to the user's choice, so a tag refresh keeps it
-- there instead of bringing back the site's namespace as a second tag.
CREATE TABLE ns_overrides (
    from_ns INTEGER NOT NULL REFERENCES namespaces (id),
    name    TEXT    NOT NULL,
    to_ns   INTEGER NOT NULL REFERENCES namespaces (id),
    PRIMARY KEY (from_ns, name)
) STRICT, WITHOUT ROWID;
