-- Comics (2026-10-20): the user's own title (a site's title is often badly
-- formatted; the site's stays in title, and a refresh never touches this one)
-- and a crop of the cover (fractions of the cover: "x,y,w,h"; NULL = all of it).
ALTER TABLE comics ADD COLUMN custom_title TEXT;
ALTER TABLE comics ADD COLUMN cover_crop TEXT;
