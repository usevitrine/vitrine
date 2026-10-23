-- Preferences shared by every device (default search and sort, video
-- playback): one JSON value per key. Per-device ones (screen spacing) stay in
-- each browser.
CREATE TABLE settings (
    key        TEXT    PRIMARY KEY,
    value      TEXT    NOT NULL,   -- JSON
    updated_at INTEGER NOT NULL
) STRICT, WITHOUT ROWID;
