# Tag refresh

Periodic tag refresh of posts the library has (sites keep editing tags). How it pauses with its site: downloader/reliability.md.

## As built (from step 1, 2026-10-14 on)
- **Tag refresh (built 2026-10-14)** (`internal/download/refresh.go`): every post the library has from a site is read
  again on a schedule and its file gets the site's current tags (with namespaces), links and rating. A post is due a
  quarter of its age after it was last read (`seen_posts.seen_at`, which downloads, searches meeting the post again
  and refreshes all set), clamped to 1 day .. ~6 months; age counts from the upload (or the import). Each site has a
  background loop for it **beside its queue** (2026-10-28, the user: it "gets stuck behind huge initial downloads" -
  until then it ran only while the queue was empty, days behind a first check of 700k posts; 99 refreshes were due on
  the author's rule34), its requests `plugin.Idle`: they take the site's turn only when no search or check waits for
  one (the rate stays the same), after the tag-type lookups, most overdue first, 6 s between posts, 15 min pause
  after an error. One request per post: the engine's `Lookup` uses the JSON API (`id:N`, with `tag_info`; an empty
  reply = the site no longer has the post: its tags stay, `seen_at` moves on). Changed tags leave the file's
  modified time alone (2026-10-21; it used to change, as in Hydrus, and "modified in the last N days" filled up with
  retagged files - see docs/library/search.md). "Refresh tags" in the
  details panel reads a file's posts at once (`POST /api/files/{id}/refresh`); on a selection it makes their posts
  due now (`POST /api/refresh {ids}`, `seen_at = 0`). `GET /api/downloads` has `refresh: [{site, posts, due}]`,
  shown as a Tag refresh card. Steady state for a ~90k-file library: roughly 500-1000 lookups a day.
