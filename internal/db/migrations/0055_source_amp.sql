-- Source links kept with an HTML code in them: a site's JSON API sends a post's source text as the site stores it
-- ("...?a=1&amp;s=46"), its XML API decoded ("...?a=1&s=46"), so a tag refresh (JSON) added a link the download (XML)
-- had already given, once more (2026-11-13). Each such link becomes its decoded form, merged with that one if the file
-- has it already; the coded rows go.
INSERT OR IGNORE INTO urls (url)
  SELECT DISTINCT replace(u.url, '&amp;', '&')
  FROM urls u JOIN file_urls fu ON fu.url_id = u.id AND fu.kind = 2
  WHERE u.url LIKE '%&amp;%';
INSERT OR IGNORE INTO file_urls (file_id, url_id, kind, site_id)
  SELECT fu.file_id, c.id, 2, fu.site_id
  FROM file_urls fu JOIN urls u ON u.id = fu.url_id JOIN urls c ON c.url = replace(u.url, '&amp;', '&')
  WHERE fu.kind = 2 AND u.url LIKE '%&amp;%';
DELETE FROM file_urls WHERE kind = 2 AND url_id IN (SELECT id FROM urls WHERE url LIKE '%&amp;%');
DELETE FROM urls WHERE url LIKE '%&amp;%' AND id NOT IN (SELECT url_id FROM file_urls);
