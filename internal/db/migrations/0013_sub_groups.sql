-- Subscription groups: named by the user, one site each; a subscription is in
-- at most one (deleting a group leaves its subscriptions ungrouped).
CREATE TABLE sub_groups (
    id         INTEGER PRIMARY KEY,
    site_id    INTEGER NOT NULL REFERENCES sites (id),
    name       TEXT NOT NULL,
    interval_s INTEGER, -- the group's usual schedule (new subscriptions in it start with it)
    created_at INTEGER NOT NULL
) STRICT;
CREATE UNIQUE INDEX sub_groups_name ON sub_groups (site_id, name COLLATE NOCASE);

ALTER TABLE subscriptions ADD COLUMN group_id INTEGER REFERENCES sub_groups (id) ON DELETE SET NULL;
