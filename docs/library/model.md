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
  written at delete time and removed on undo. Since 2026-10-26 it also notes the **sites** the file came from
  (`tombstones.sites`: `,a,b,` plugin IDs, `''` none; migration 0042 filled it in for what was still in the trash -
  older records whose files were gone stay NULL: they can't be traced), so wiping a site can take its records too (below).
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
  **2026-10-28 (the user: slow to load "considering how few there are"; the library showing a moment; files staying
  until all were gone)**: the trash's index read only its files' *links* but every tag (170k, with trigrams), every
  file's sites and the day-only post order over all of seen_posts - ~1.9 s, and built again after nearly every write
  while a download ran. Now each step reads only the deleted files' rows (`Index.only`: tags those files or relations
  use, sites, found-by, favorites, climaxes, hidden tags; no post order, no `ORDER BY id`, which made SQLite read the
  whole table instead of migration 0043's `files_deleted` index): ~9 ms on that library. Switching pages: the store
  records which index the list on screen came from (`results.shown`); another page's list is `stale` for the grid -
  what was on screen fades out where it is (the whole grid at once, 180 ms, a slight shrink about the middle of the
  view - first done with copies of each tile leaving, which the user's recording showed coming back a moment later at
  full strength, moved down by the trash's header, over the trash's own tiles), then this page's tiles appear one
  after another (`appearAll`) - never before the fade has ended (they wait hidden) - Library and Comics, also arriving from another screen (the old
  `switching` hid the grid only after a frame, and not at all from another screen). Removing for good goes in turns of
  50 (`TURN`, lib/trash): Delete forever in the list's order, Empty trash with `{limit}` (`EmptyTrashSome`: the last
  deleted first, `left` counted) - the list refreshed after each, so the files leave as they go. Tests:
  `TestEmptyTrashSome`; e2e "trash: going there" checks every frame for a file not in the trash.
- **Wiping a removed site (2026-10-26)** (the user: removing a site can "completely wipe their server of any hint that
  they ever had it enabled. That means deleting all files/metadata/deleted files history/any entry in every menu such
  as the sites filter"): after Remove, when anything of it is left, a second dialog asks whether to erase every trace
  (`GET /api/plugins/{id}/traces` counts it: `Library.SiteTraces`) - a list of what goes, **Keep them** or **Erase
  everything…** (a plain question first since 2026-10-28, the user: the word to type came with no asking whether to
  erase at all; the dialogs' other button can be named: `ConfirmOptions.cancel`); then the word "erase" to type and
  "Also delete older backups (they still contain it)", off by default (the user's choice). Keep them, or Cancel at the
  word, removes only the site. `Library.WipeSite` (in the background, a notice that doesn't name it when done): its comics go for good; files
  only it gave (links, tags or sources from no other site - the trash's too) are deleted, removed for good and their
  records go; files another site gave too **stay**, without its tags, links and sources (not an edit: modified
  stays - the user's choice "Keep, strip this site"); then its downloads, subscriptions, groups, history
  (`seen_posts`), tag types, notices, pauses, deletion records naming only it (it's taken off the others), its `sites`
  row, `system:site=` in saved searches (one that only searched it goes) and the pages' start searches
  (`search.DropSite`), `siteCheckDepths`/`sitesOff`, its tags nothing else has (`index.ForgetTags`) and links no file
  has. The server then forgets its credentials, site icon, resized copies, bell notices (open pages read the list again:
  notices-state's `forgets`) and log lines naming it (`logring.Forget`, memory and files), deletes the older backups
  when ticked (a fresh one made after), and compacts meta.db and thumbs.db (VACUUM: nothing left in free pages). Not
  reachable: the container's own output (`docker logs`), copies outside the data folder, and the built-in site's ID in
  `removedSites` (needed to keep it removed). A site that goes back to its built-in plugin isn't asked. Tests:
  `TestWipeSite`, `TestDropSite`, `TestForget`, `TestRemovePluginWipe` (Go), "removing a site with files of it asks
  whether to erase every trace" (e2e).
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
  from the default); a `sort:` typed into the search moves into the menu. Default sort is a setting (Settings > Library; Newest uploaded unless picked - since 2026-10-23, the user; was Newest imported -
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
- **Emptying shows (2026-10-25)** (the user: "when clearing trash theres no indication that its deleting anything"): the
  red button spins (`trash.emptying`, the icon and a dark spinner crossing in place; disabled, aria-busy, "Emptying the
  trash" for screen readers) from the confirmation until the emptied page is back - Media's and Comics' alike.
- **No last page on the way (2026-10-25)** (the user: "if i change pages from the media library to the trash, it for a
  short time still shows the media library images"): the library and its trash (and Favorites; the Comics page's
  likewise, not a collection's own morph) are one screen, so the old files stayed until the new search answered. A
  page switch now hides the grid at once (`switching`: opacity 0) and fades the new page's files in when they come.
  Test: "trash: going there shows none of the library meanwhile, and Empty trash spins while it empties".
