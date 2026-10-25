-- A gallery's pages in the bell's notices (2026-10-25, the user: "the downloader and the notifications still only show
-- 1 file when downloading a multi page gallery"): a gallery site's download counts galleries in new, its pages here.
ALTER TABLE notices ADD COLUMN pages INTEGER NOT NULL DEFAULT 0;
