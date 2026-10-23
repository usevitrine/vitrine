-- A download or subscription can add the files it brings in to the library's
-- favorites (2026-10-20): your own favorites on a site (fav:me) become
-- favorites here too. A subscription's checks copy its setting.
ALTER TABLE searches ADD COLUMN favorite INTEGER NOT NULL DEFAULT 0;
ALTER TABLE subscriptions ADD COLUMN favorite INTEGER NOT NULL DEFAULT 0;
