-- A file opened in the viewer is seen: never new again, whenever it came (the index's isNew).
ALTER TABLE files ADD COLUMN seen INTEGER NOT NULL DEFAULT 0;
