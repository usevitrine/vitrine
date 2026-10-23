-- A collection's own order (2026-10-23, the user: "I need to be able to manually order collections"): a comic's place
-- in its collection (0 first); NULL - not placed yet (a collection never reordered, or a comic added since): after
-- the placed ones, by title (naturally: "Vol 2" before "Vol 10").
ALTER TABLE comics ADD COLUMN collection_pos INTEGER;
