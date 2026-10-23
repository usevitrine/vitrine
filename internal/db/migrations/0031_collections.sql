-- Collections (2026-10-21): comics that belong together (a series, its volumes), named by the user. A comic is in
-- one collection at most; on the Comics page a collection is one stacked tile, opening to its comics.
CREATE TABLE collections (
    id         INTEGER PRIMARY KEY,
    name       TEXT    NOT NULL,
    created_at INTEGER NOT NULL
) STRICT;
ALTER TABLE comics ADD COLUMN collection_id INTEGER REFERENCES collections (id) ON DELETE SET NULL;
CREATE INDEX comics_collection ON comics (collection_id) WHERE collection_id IS NOT NULL;
