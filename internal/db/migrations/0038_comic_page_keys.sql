-- A comic page's key on its site (2026-10-23; e-hentai: the key in the page's link, the start of the original
-- picture's SHA-1 - the same in every gallery): a download finds the pages the library has by it (a newer version of
-- a gallery, the same picture in another one) and uses them without asking the site. NULL: not from a site, or from
-- before keys were kept.
ALTER TABLE comic_pages ADD COLUMN site_key TEXT;
CREATE INDEX comic_pages_site_key ON comic_pages (site_key) WHERE site_key IS NOT NULL;
