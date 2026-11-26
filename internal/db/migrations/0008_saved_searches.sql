-- Saved searches (favorites): a name, a query and a result order, in the
-- user's own order. Kept on the server so every device has the same ones.
CREATE TABLE saved_searches (
    id         INTEGER PRIMARY KEY,
    name       TEXT    NOT NULL,
    query      TEXT    NOT NULL,      -- as typed into the search (without sort)
    sort       TEXT    NOT NULL DEFAULT '',  -- the sort menu's value; '' = the default order
    position   INTEGER NOT NULL,
    created_at INTEGER NOT NULL
) STRICT;
