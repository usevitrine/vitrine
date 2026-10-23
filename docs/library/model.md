# Library model

Files, tags (one tag set, origins, namespaces), parents and siblings, delete/undo/tombstones, upload times.

## Library model (decided 2026-10-14)
- **Single user**: one account with a login; no per-user tags or permissions, but keep an owner/user ID on sessions
  and tokens so more users aren't impossible later.
- **One tag set, each file-tag link marked by origin**: a site (from a plugin) or `user` (added by hand). Re-syncing a
  post's site tags replaces only that site's tags on the file - never user tags. A user removal of a site tag is
  remembered (`hidden_tags`) so a re-sync doesn't bring it back.
- **Site tags stored verbatim.** Only namespaces the plugin declares are namespaces (Hydrus split every tag with a
  colon - `9:16` became namespace `9`); a site's rating (explicit/questionable/...) is a field, not a tag.
- **Tag parents and siblings are core**: editable in the web client, applied in search and display (a parent tag
  finds its children's files), cycles rejected. Parents are often used as **group tags** (e.g. a parent standing for a
  set of artists), so the parent editor should work well for managing such groups.
- **Tag refresh for existing posts** (a top priority): sites keep adding tags after download, so Vitrine
  re-fetches tags for posts it already has - subscriptions re-check their posts on a schedule (newest often, older
  rarely), plus "refresh tags" for a file, a search result or a whole subscription. Cheap where the API returns tags
  in bulk (e.g. 100 posts per request); sites that need a request per post get a slower schedule.
  Refresh traffic obeys the plugin's rate limits and runs below downloads.
  **As built (2026-10-14)**: every post from a site (not only subscriptions') is refreshed one request at a time on an
  age-based schedule (a quarter of its age, 1 day .. 6 months) while the site is idle; gelbooru 0.2 sites have no batch
  lookup by post ID, so the "100 per request" idea only applies to re-running a search (which also refreshes what it meets).
  "Refresh tags" exists for a file and a selection. Details: `docs/downloader/tag-refresh.md`.
- **Same file from several sites = one file**: deduped by SHA-256 (md5 checked before download); keeps every site's
  post link, source links and tags (each marked with its site).
- **URLs**: the post URL and the post's **source** links (often the artist's original post on X etc.) are stored
  separately and shown as "Post" and "Source".
- **Delete with a short undo window**: the file leaves every search immediately and an "Undo" appears; the original
  stays hidden for a configurable time (default 7 days), then is purged. The tombstone (never download again) is
  written at delete time and removed on undo.
- **The trash (2026-10-23)** (the user: "a way to restore individual deleted media and clear the bin"): found then that
  nothing ever purged - every deleted file (thousands, gigabytes, in a real library) was still in the store. Now: the
  library's **Trash** page (`/trash`, Menu > Library > Trash) - the library's own search bar and grid on the trash's
  index (`index.LoadTrash`: the deleted files and their tags; its "modified" is when each was deleted, the page's
  order "Deleted", last first; built again after writes, small - only the deleted files' tag links are read). A
  tile's menu, the selection bar and the viewer offer **Restore** (`POST /api/undelete`) and **Delete forever**
  (`POST /api/trash/purge {ids}`, asked first - focus on Cancel); **Empty trash** (`POST /api/trash/empty`; a red icon
  button left of the search bar, the user's choice) asks for a typed word ("delete": ConfirmDialog's `typed`). A line
  under the search bar: how many files, their size, how long they're kept. Deleted files' originals still open
  (`FileBySHA` finds them) until removed. Settings > Library > "Empty the trash": never (the default - nothing goes by
  itself unless chosen), or after 7/30/90/365 days (setting `trashDays`; the server looks every hour:
  `Library.Purge`). Removed for good: rows, tags, thumbnails and originals go; the tombstone stays (never downloaded
  again). In batches of 500 (`purgeBatch`, 2026-10-23), each its own transaction - a big trash (or a whole section
  deleted) no longer holds every other change up (a setting and a notice once waited 11 s); each batch's originals go
  after its rows are committed. Tests: `TestTrash`, `TestEmptyTrashInBatches` (Go), "trash: its own page" (e2e).
- **Left out** (by design): user ratings (a favorite heart instead, see "Favorites"), inbox/archive, PTR, Hydrus import. Notes and view counts: low
  priority. Duplicate detection (perceptual hashes + review screen): built 2026-10-16, see `docs/library/duplicates.md`.

## Upload time (2026-10-14)
- Each file keeps `posted_at`: when it was posted on its source site (gelbooru 0.2 `created_at`, e.g. "Wed Oct 14
  13:00:17 +0200 2026"; migration `0004_posted`). With several sites the earliest wins. Unknown (files imported from
  disk) = NULL.
- Search: `sort:uploaded` (aliases `posted`, `upload`, `source_time`), `_asc` for oldest first; filter
  `system:uploaded<7d` / `>=2026-01-01`. Files with no upload time use their import time there, so every file
  still has a place in the order.
- Web client: a sort menu in the top bar (above the search bar) - Newest/Oldest imported, Newest/Oldest uploaded. The sort is kept
  apart from the search (2026-10-14): never a chip; in the URL as its own `?sort=` (only when it differs
  from the default); a `sort:` typed into the search moves into the menu. Default sort is a setting (Settings > Library,
  per device for now). The client adds `sort:x` to the query only when calling the API. (Superseded: the menu first
  edited a `sort:` chip.) The details panel shows the upload date.
- **`system:site=` (2026-10-15)**: files that came from a site (`=` / `!=`; plugin ID or the site's address, any
  case: `system:site=danbooru.donmai.us`), from `file_urls` (a file the same on several sites is in each). The index keeps a
  bitmap per site (loaded at start, added to by `SetSitePost`); sites are named by their plugin ID and, once the
  downloader starts, their address. Not sortable. Several sites (2026-10-19): `system:site=a,b` = from any of them, `system:site!=a,b` =
  from none of them (the union of their bitmaps).
- **Trash on its own, with comics (2026-10-23)** (the user: "Trash should be outside the media section and have comic
  support"): Menu > **Trash** (its own entry after Duplicates) > Media (`/trash`) and Comics (`/comics/trash`). The
  comics trash is the Comics page on the deleted comics (`index.LoadComicsTrash`, scope `comics_trash`; "updated" =
  when deleted, last first; no collection tiles), with the same Restore (`/api/comics/undelete`), Delete forever
  (`POST /api/trash/purge-comics {ids}`: the comics and the page files only they had) and Empty trash
  (`POST /api/trash/empty-comics`, a typed word). `/api/trash` also says `comics` and `comic_bytes`; the automatic
  emptying (`trashDays`) covers comics too. A deleted comic still opens in the reader from there.
- **Emptying fast (2026-10-23)**: the first real Empty trash (well over a thousand files) took 5 minutes - thumbnails were deleted one
  file at a time, two commits each. Now one transaction for all of them (`thumbs.Delete(ids...)`).
