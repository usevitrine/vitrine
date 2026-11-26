-- A gallery's pages in the bell's notices (2026-10-25; they showed 1 file for a gallery of many pages): a gallery
-- site's download counts galleries in new, its pages here.
ALTER TABLE notices ADD COLUMN pages INTEGER NOT NULL DEFAULT 0;
