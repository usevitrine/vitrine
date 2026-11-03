-- The downloader's age limit (2026-11-03): posts skipped as too old are outcome 8, and aren't looked at again while the
-- limit stands. Lowering it forgets them all, so a small index: seen_posts holds millions of rows.
CREATE INDEX seen_posts_too_old ON seen_posts (outcome) WHERE outcome = 8;
