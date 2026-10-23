-- A subscription to a user's favorites remembers which of its posts are in the
-- library (2026-10-20): a complete check that no longer finds one marks it gone
-- (gone_at), and Downloads asks whether to delete those files or keep them.
CREATE TABLE fav_members (
    subscription_id INTEGER NOT NULL REFERENCES subscriptions (id) ON DELETE CASCADE,
    post_id         INTEGER NOT NULL,
    file_id         INTEGER NOT NULL REFERENCES files (id) ON DELETE CASCADE,
    gone_at         INTEGER, -- no longer in the favorites (Unix ms; NULL = still there)
    PRIMARY KEY (subscription_id, post_id)
) STRICT, WITHOUT ROWID;
CREATE INDEX fav_members_gone ON fav_members (subscription_id) WHERE gone_at IS NOT NULL;
