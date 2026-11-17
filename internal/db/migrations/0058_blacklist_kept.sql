-- Files kept despite the blacklist (2026-11-17, the user: "what if an image has a blacklisted tag but i want to keep
-- it" -> "Yes, add Keep"): the blacklist's clean-up and its notice leave them alone; system:kept finds them.
CREATE TABLE blacklist_kept (
    file_id  INTEGER PRIMARY KEY REFERENCES files (id) ON DELETE CASCADE,
    added_at INTEGER NOT NULL           -- Unix ms
) STRICT;
