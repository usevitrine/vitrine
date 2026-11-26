-- Browsers that get push notifications (one row per device that turned them on).
CREATE TABLE push_subscriptions (
    endpoint   TEXT PRIMARY KEY, -- the push service URL for this browser
    p256dh     BLOB NOT NULL,    -- the browser's public key (65 bytes)
    auth       BLOB NOT NULL,    -- its auth secret (16 bytes)
    label      TEXT NOT NULL,    -- e.g. "Safari on iPhone"
    created_at INTEGER NOT NULL,
    last_ok_at INTEGER           -- the last time a message was accepted
) STRICT;
