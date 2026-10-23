-- A post's source text was split at spaces, so labels ("Twitter:", "Patreon:") and other words
-- were kept as sources. Only links are sources: drop the rest (2026-10-19).
DELETE FROM file_urls WHERE kind = 2 AND url_id IN (
  SELECT id FROM urls WHERE url NOT LIKE 'http://%' AND url NOT LIKE 'https://%'
);
DELETE FROM urls WHERE url NOT LIKE 'http://%' AND url NOT LIKE 'https://%' AND id NOT IN (SELECT url_id FROM file_urls);
