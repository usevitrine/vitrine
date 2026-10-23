-- Check depth (2026-10-20): how many posts a subscription's check goes on
-- past the newest one the last check saw (posts tagged after they were seen
-- are found that way). Set on a subscription, else its group, else its site
-- (a setting), else for all (a setting); NULL = not set here. A check keeps
-- the depth it started with.
ALTER TABLE subscriptions ADD COLUMN check_depth INTEGER;
ALTER TABLE sub_groups ADD COLUMN check_depth INTEGER;
ALTER TABLE searches ADD COLUMN stop_depth INTEGER NOT NULL DEFAULT 0;
