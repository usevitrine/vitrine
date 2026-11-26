-- Where a notice leads (2026-11-12; tapping one opens what it's about): the download and the subscription it is about
-- (0: none).
ALTER TABLE notices ADD COLUMN search_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE notices ADD COLUMN sub_id INTEGER NOT NULL DEFAULT 0;
