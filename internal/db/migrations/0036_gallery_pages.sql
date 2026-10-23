-- Pages a download (or a subscription, over its checks) brought in for galleries: a gallery site's counts are by
-- page for one gallery, by gallery for a search of many.
ALTER TABLE searches ADD COLUMN pages INTEGER NOT NULL DEFAULT 0;
ALTER TABLE subscriptions ADD COLUMN pages INTEGER NOT NULL DEFAULT 0;
