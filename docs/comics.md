# Comics

The comics library (galleries, comic tags, read state, custom titles, cover crops, collections) and the Comics page and reader.

## Comics as built (2026-10-20; stage 1 of 3)
- **A comic** (a gallery: doujinshi, manga, an artist's CG set) = `comics` row (title, alt title, site + gallery key,
  URL, category, page count, cover, posted/created/updated, reading place) + `comic_pages` (comic, 0-based page, file, the page's key on its site - downloader/engines.md)
  + `comic_tags` (like file_tags: one row per origin, 0 = the user). Migration 0026.
- **Comics live only on the Comics page**: a page's file is stored like any file (dedupe, thumbnails, renditions) but
  carries `FlagComicOnly` (files.flags 1<<6) and is **never in the search index** (index load skips it; it's never
  AddFile'd), so it never shows in the library's grid, searches, counts, stats, duplicates or "new" pill. The same
  file imported or downloaded for the library (ImportAs, or SetSitePost after FindMD5 found it) **joins the library**
  (flag cleared, AddFile). Deleting such a shared file from the library while a live comic has it only takes it out
  of the library again (flag set, out of the index, no tombstone).
- Delete: `DeleteComics` marks the comic and its pages that only it has (other comics' or the library's stay) with
  the same deleted_at; `UndeleteComics` brings them back; `Purge` drops comics past the undo window first, then their
  page files go with the other deleted files. A library tombstone doesn't block a comic page (a deleted file in its
  undo window comes back as a comic page only).
- **Search: a second search index** (2026-10-20, `index.LoadComics`; the old in-memory `ComicQuery` only feeds
  `GET /api/comics` now): the library's index structure over comics (a comic's ID is its "file" ID), so the Comics page
  searches with the library's syntax - chips, OR groups, wildcards, filters, every order, slices by position. Its
  tags are the comics' own (`comic_tags`; never the library's), parents and siblings are shared, `system:site=` is
  where it came from. Columns: size = its pages' bytes, width/height = its cover's (masonry), imported = added,
  modified = updated, uploaded = posted on its site; and its own (`comicColumns`, nil in the library's index):
  pages, read state, last read, title order. New search fields: `system:pages>20` / `sort:pages`,
  `system:unread` / `system:reading` (opened, not finished) / `system:read` (to the last page) and `sort:read` (last
  read), `sort:title` (A to Z = `title_asc`). **A word that isn't a comic tag searches the titles** (`blue_sky` = "blue
  sky", also the original-language title). **`/words` searches the titles only** (2026-10-21; `search.KindTitle`,
  never a tag; `-/words` excludes, `*` = anything between; in the library's index it stays the tag "/words"). The
  Comics search box takes a `/` search with spaces ("/some title", Enter ends it; no tag suggestions) and shows it as
  a chip in quotes. The index is small and built whole: `Library.ComicIndex` rebuilds it on
  the first search after any write (`writes` counter bumped by every `write` and by `SetComicRead`).
- **The user's own title and a cover crop** (migration 0028): `comics.custom_title` (shown as the title everywhere, the
  site's `title` kept and still searched; a refresh updates only the site's), `comics.cover_crop` ("x,y,w,h" fractions;
  the comics index gives the cover the crop's shape for masonry). `POST /api/comics/{id}` {title, crop}.
- **The comics being read come first** in every comics search (`Index.PinReading`: opened, not finished; last read first).
- **Category** (the site's, e.g. Manga) is also a tag `category:manga` (migration 0027 for existing comics), so it's
  searched and filtered like the others; the language is the site's `language:` tags.
- **Favorites**: `comic_favorites` (migration 0027); `system:favorite` / `sort:favorite` on the comics index.
- **Saved searches**: `saved_searches.scope` ('' library, 'comics'; migration 0027) - each page its own list.
- **Selections** (`POST /api/comics/do/{favorite|read|tags|tags-of|refresh}` {ids, ...}): favorites, mark read /
  unread, the user's tags (add; remove takes a tag off whatever gave it - a site's comes back when its tags are read
  again), and refresh = read each comic's gallery from its site now (title, page count, tags; `Downloader.RefreshComics`;
  pages come with a download of the gallery).
- Import: `vitrine import-comic <folder | zip/cbz>...` (`ImportComic`): pictures in natural name order (p2 before p10),
  other files skipped, title = folder/archive name or `-title`.
- API: the library's search endpoints with `scope=comics` (`/api/search`, `/api/new`, `/api/results/{id}/dates`,
  `/api/search/sites`, `/api/autocomplete`, `/api/namespaces` - the comics' own namespaces), `GET /api/comics/info?ids=`
  (details for a result's IDs, with the cover's thumbnail, size and URL), `GET /api/comics/{id}` (comic + its page
  files), `POST /api/comics/{id}` {read_page, tags (replace the user's), add_tags}, `POST /api/comics/delete|undelete`
  {ids}, `POST /api/comics/do/...` (above), saved searches with `?scope=comics`.
- Next: stage 2 is done with it (the Comics page and reader, "Comics page and reader" below); stage 3 = the
  e-hentai/ExHentai engine (tag searches, a pasted gallery link = one comic, subscriptions, the login) -
  `docs/downloader/engines.md` when built.

## Comics page and reader (2026-10-20)
- **Comics** in the menu after Library (`/comics`; the reader at `/comics/<id>`, so Back closes it and a reload
  reopens it). **It looks and works like the library** (2026-10-20): the library's own components, handed the page's
  set through Svelte context (`lib/scope.svelte.ts`: `useScope()` gives a component its page's `Results`,
  `Selection` and `Saved`; the library's without one). The search bar (chips, OR groups, editable chips, its own saved
  searches; suggestions from the comics' tags), the filters menu with the comics' filters (Added / Uploaded /
  Updated; Unread, Reading, Read, Number of pages, Category and Language - picked from the comics' own tags with
  counts; Favorite; Number of tags; Limit; From = only the sites comics came from) and orders (Added, Uploaded,
  Updated, Last read, Title, Number of pages, Favorited, Number of tags, Random), the count in the top bar, the grid
  **always masonry** (each cover in its own shape - or its crop's; 2 across on phones, ~260 px tiles wider) with the fast
  scroller, hover zoom (at once on comics, no rest needed; the card stays while the mouse is on it; on comics only 15% bigger than
  the tile, in its shape - their tiles are large already), selection by drag /
  long-press / Ctrl / Shift and the selection bar (Tags with comics' words, Refresh from the site, Mark as read /
  unread, Favorite, Delete with undo). **A tile has nothing to press** (2026-10-20): a status badge top left (read: a
  book with a check on green; being read: a book with a turning arrow on mauve), the heart top right, and at its foot
  over a shade a row of chips (its facts: pages for now; new ones go there too, above the title), then the title (one line; with a mouse it scrolls sideways while the mouse is on the tile when it doesn't
  fit; on touch screens two lines) - the pages chip: a pages icon and the count ("12/75" while it comes in); a mauve line
  along the bottom = how far it's read. Nothing is hidden by the hover card (the overlays sit above its picture). A
  tap opens the reader - **at the cover**, or a comic being read **where it stopped** - the cover growing from its tile
  into the reader and **turning into the page it opens at on the way** (hero `into`: a second picture crossfading
  mid-flight, each "cover"-fitted in the moving frame at its own shape; the frame lands at the page's shape - its size
  and thumbnail from the reader's own fetch, started as it opens and waited for at most 150 ms, then handed to the
  reader: `preload`); then it fades into the reader's page (`fade` 0.38 s, held until it's there: `hold`; `above`:
  over the reader). Closing: the page shown goes back into its tile, **turning into the tile's cover on the way**
  (cropped as the tile shows it: `HeroPicture.crop`) - it lands as the tile, nothing swaps (`lib/hero`, as the
  library's viewer). After a swipe down the backdrop goes on from the reader's faded one (no flash); while either
  animation plays the grid opens no hover cards (`Grid` `noHover`; an open one snaps back) - the mouse is suddenly over
  the grid as the reader hides. **The tile is what flies**: hidden while its animation plays (`Grid` `hideId`), so a
  hovered card grows into the reader (no tile snapping back under it); its page is looked up first (at most 150 ms,
  the card staying as it is), then the animation starts from the card. A comic has a read state only once left past its
  cover (a one-page comic: read there); opening it isn't reading - its place is saved once a page is turned. The menu
  (right-click / long-press): Select, Read, Details, Favorite, **Mark as** > Read / Unread (more than one page - one
  page is read by opening it; its state on the right, a small page of choices sliding in, the menu's place (`lib/pages`, as every menu with pages; the width stays): `FileMenu` `choices`;
  `markComicsRead`, shared with the selection bar), **Crop cover**, Delete.
  **The comics being read come first** in every search (`Index.PinReading`, last read first) - there's no separate
  "Continue reading" row any more. Live updates while a gallery comes in: at most every 5 s, the details reloaded in
  place (`Results.reloadDetails`) - before, every page cleared them and the grid flashed. **Details = the library's details panel** (2026-10-20,
  consistency): the same frame (`DetailsPanel`: a column on the right on wide screens - the page makes room - a sheet
  on phones; header with the heart, delete and close), the same body layout (`ComicPanel`, styles shared with FilePanel in
  `lib/details.css`): cover, titles and Read / Continue at page N / Read from the start; a facts card (pages, progress,
  category, language, uploaded, added, updated, comic ID); From (the gallery page: open, copy; Download again); and the
  same Tags section (`TagsSection`, shared with FilePanel: a tag's menu, pick several, the pencil, refresh from the site).
  **Details open over the reader only** (like a file's over the viewer - never over the grid): Details in a tile's menu
  opens its reader with them; the reader's ⓘ; closing the reader closes them; a tag's search closes both, then
  searches. **Rename** (the pencil by the title): a pop-up (`RenameDialog`) with the title, Save, and "Original title"
  (an undo to the site's, once there's one of the user's own); the notice has Undo; a refresh never touches it.
  **Crop** (under the cover, or Crop cover in a tile's menu): `CropDialog` - the cover with a frame, drag it to move, a corner to
  resize (any shape), All of it / Save; the grid shows only the crop, in its shape. The page stays mounted once opened (its search and place stay, like the
  library's). The comics' namespaces (female, male, language, category, ... from a gallery site) are namespaces on
  the page (`namespaces.comics`): colored (female pink, male sapphire, language teal, category yellow) and grouped.
- **Reader** (`components/ComicReader.svelte`): full screen over the app (z 45: notices and menus stay above).
  Layouts (device setting): **One page**, **Two pages** (the cover alone, then pairs; a wide page alone), **Scroll down**
  (webtoon: the pages one under another, at most 900 px wide, lazy beyond the next few). **Direction** per comic (Left to
  right / Right to left (manga)): taps, swipes, arrow keys and the slider all follow it. Turning: a swipe (a quarter of
  the width or a flick), a tap in the outer 30 % at either side, arrow keys / Space / PageUp-Down / Home / End, the
  wheel (as the viewer's: as fast as it turns - a mouse wheel's click is a page, a trackpad's small steps add up to one). As the library's viewer: **a tap or a key turns at once**; **a swipe slides** (the page follows the finger, then
  slides on; a short swipe springs back). Slides are keyed by page, so nothing reloads after a turn. A tap in the middle shows/hides the controls.
  **Looks and handles like the viewer** (2026-10-20): the viewer's background (crust); floating controls, each on a
  small blurred dark backing (no band): close ✕, "n of N" chip, the title chip (not under 480 px), then ♥ (L),
  full screen (F), ⓘ (i; mauve when open) and the Reading menu (layout, direction); the slider in a blurred pill at
  the bottom; arrows at the sides on wide screens. The controls fade after 3 s without anything happening (not while
  pointed at or with the details open); the mouse moving brings them back (the cursor hides with them). **Swipe down
  closes** (axis from the first 8 px, as the viewer's: the page follows the finger and shrinks a little, the background
  and controls fade; past 110 px it closes, the page going back into its tile; phones: the details sheet goes first);
  **swipe up** shows the details. Details: a 340 px column beside the page on wide screens (the page makes room,
  0.26 s), a sheet on phones; the page keys still work with them open; Esc closes them first. Delete asks, with undo.
  The details' header has delete and close (the heart is on the reader's bar, as the viewer's).
  A page is sized from its slide (container units: the height, or the width - a spread: half - at its shape); with
  `max-width` and `aspect-ratio` Safari made it wider than a phone's screen, cut off (2026-10-20).
  **Zoom as the viewer's** (2026-10-20): a double tap (a mouse: a click in the middle - the sides turn at once) goes to
  the page's own pixels (100 %; twice the fit if it's smaller) at the spot and back; pinch, Ctrl + wheel, and the wheel
  once zoomed (around the pointer; the spin that zoomed out doesn't turn on). Limits: at least 4x or enough for 1.5x its
  pixels (at most 16x); a pinch past them resists and springs back. Zoom steps animate, pinch and pan follow the finger;
  a flicked pan glides; kept inside the screen at the page's fitted size. Zoomed in (> 1.15x): the full-size pictures
  load in the background and replace the screen-size ones. The top bar shows the zoom level ("75 %" of the page's own
  pixels) and the viewer's spinner while the picture wanted is loading. Middle click closes; middle-drag pans a zoomed
  page; zoom-in/out cursors; a tap with the details open closes them; a swipe back from the first page resists. The
  slider sits 28 px above the safe area on touch screens (clear of the home bar).
  Each page shows its thumbnail at once until the screen-size picture arrives; the next four pages (and the one
  before) load ahead. The place is saved 0.8 s after a turn and on closing (the shelf takes it from the reader at
  once); past the last page the end page, a page of its own in the track (turned to and swiped back from like any page;
  Back to Comics / Read again; in a collection, **Up next**: the next comic A to Z, its cover and title, opening in the
  reader's place - from its start if it was read to the end; Back still closes the reader). Esc closes (or zooms out).

### Collections (2026-10-21)
- A comic belongs to at most one **collection** (e.g. a series; table `collections`, `comics.collection_id`,
  migration 0031). On the Comics page a collection is **one tile**: a mosaic of its first comics' covers (its order; two side by side, three as one big and two small, four as 2x2; the tile in its tallest cover's shape (as tall as that
  cover's own tile; `tileShapes`), each cover - the user's crop when it has one - filling its cell, cropped evenly
  around its middle, never stretched (mosaic entries carry the cover's size; the frame is sized with container units)),
  the edges of two more covers drawn along its top (a deck, inside the tile: tiles have `contain: strict`), a chip
  "n comics", and the finished badge once every comic in it is read. Searches match its comics; the tile stands in
  where the first matching one would be (a pseudo ID `1<<31 + collection id`, so slices and the grid work as usual). The count in the top
  bar counts the comics found, each of a collection's too (`/api/search` `comics`; `total` is the tiles).
- A comic's menu: **Add to collection** (a page of choices: the collections, ticked where it is, and "New
  collection…", which asks the name in a small dialog - `lib/prompt`). The tile's menu: Open, Rename collection,
  Take apart (its comics stay, on their own again), **Delete collection** (red; asked first, "Its n comics go to the
  trash", Undo in the toast - `POST /api/comics/collections/{id}/delete` -> `{ids}`, `DeleteCollectionComics`). An emptied
  collection goes away. **Deleted comics** (2026-10-23, the user: "disabling comics doesn't delete collections and
  there's no other way to delete collections"): a collection whose comics are all in the trash isn't listed (one
  restored brings it back - the comics keep `collection_id`); once they're purged it goes (`purgeEmptyCollections` in
  `PurgeComics`/`purgeComics`, so switching Comics off with "delete everything" takes the collections too; migration
  0039 removed the ones already left behind).
- **Its own order** (2026-10-23, the user: "I need to be able to manually order collections"): Reorder in its menus
  opens a list of its comics (`CollectionOrderDialog`: drag by the grip, or arrow keys on it; A to Z puts them back by
  title; Save) -> `POST /api/comics/collections/{id}/order {ids}` (`comics.collection_pos`, migration 0040). Placed comics
  first in their places, the rest after them by title, naturally ("Vol 2" before "Vol 10"); a comic added later goes
  at the end. Its page, its tile's covers and the reader's Up next follow it (`/api/search?collection=` returns the
  comics in that order - no "continue reading" pinning there).
- Its page `/comics/collection/<id>`: Back (to where it was opened from), its name, "x of n read", the same menu;
  its comics in its order, no search bar. Opening it, its comics fly out of the
  tile to their places (the other tiles fade away); Back, they fly into it again and the list is where it was
  (`results.morph`, played by the grid's list-change animation); a comic's menu there has
  **Remove from collection**. API: `GET/POST /api/comics/collections`, `POST /api/comics/collections/{id}` (rename),
  `POST .../{id}/remove`, and the comics action `collection`; the search takes `collection=<id>`.
- **Closing flies to where the comic will be (2026-10-21)**: reading moves a comic to the front (being read: first),
  and the list used to change only after the flight - the cover landed on its old place, then the tile slid away.
  Closing now leaves a still copy of the page where it was let go, waits for the reader to go (its last place is
  saved then) and, if reading changed, for the list again (at most 0.6 s), then flies to the comic's new place. A
  collection's hover card keeps its mosaic (no single cover over it).
- **Pages fade in (2026-10-21)**: each page (one at a time, two, or scrolling) fades in over its thumbnail stand-in
  once it has arrived (0.25 s; the stand-in goes when it has) - it used to swap at once, the page snapping from
  soft to sharp, which made opening look like it had no animation at its end.

## Browse (2026-10-22)
- **Comics > Browse** (its own entry under Comics in the side menu, `/browse`): a gallery site's own search
  (engines that can show page thumbnails - `plugin.BrowseEngine`: e-hentai, ExHentai), drawn by the library's own
  search bar and grid (the user: "it doesn't look like the other libraries" - the first version was a page of its
  own). `lib/browse` (`BrowseResults`, a `Results` of its own in the `browse` scope) dresses each gallery as a comic
  (cover, title, pages; `comic.mark` have/queued -> the round corner badge read comics use: a green check "In
  library", a peach download "Downloading"). No drag-select, long-press or right-click menu: a press that moves a
  little, or a slow tap, still opens the gallery (they started a hidden drag-select / long press - "some comics
  aren't clickable"). **Several at once** (2026-10-23, the user: "multi select for comic browse for bulk downloads"):
  a Select button (checks icon) beside the count in the top bar puts the grid in picking mode (`Selection.picking`:
  a tap selects with nothing selected yet; ✕ in the bar ends it), and with a mouse Ctrl/Shift-click selects without
  it. Galleries the library has or whose download waits are dimmed and can't be picked (Select all leaves them out).
  The selection bar has Select all and **Download**: each gallery its own one-time download (`addDownload`, named by
  its title), marked "Downloading" at once; a toast counts them (and any left out). Another search, site or order
  clears the picks (picking goes on); leaving Browse ends it. The top bar: the gallery count, or a spinner (the site: the filter menu's From row only - the user: "why is the dropdown still in the header"; remembered).
- **Search bar** (2026-10-22, the user: filter and sort, "/" searches, the site's own tags): the library's bar with
  its filter menu (`SystemMenu` in a browse mode) - no saved searches. `siteQuery` turns the chips into the site's
  syntax: tags via `tagQuery`, "/title words" (and `title:`) as plain words, and the menu's filters, written as the
  library writes filters (so they get its chip labels and editing) - `category:x`, `language:x` (the site's
  categories and common languages, not the library's), `system:pages>N` (-> `pages:from-to`), `system:stars>=N`
  (-> `rating:N`), `system:expunged`, `system:torrent` -> the server's filter terms, which `ehSearch` turns into
  the advanced search's fields (`f_cats`, `f_srdd`, `f_spf`/`f_spt`, `f_sh`, `f_sto`, `advsearch=1`); library
  filters the site can't do are dropped. Sort: the site's newest first, or one of its own lists (`Sorts`/`Ranked`:
  `/popular`, e-hentai.org's `toplist.php` tl=15/13/12/11 - yesterday, month, year, all time, 50 a page) - those
  have no search, so picking one clears it ("Clears the search" shown only when there is one) and a search goes
  back to newest. The menu's From row picks the site (with several). Suggestions while typing come from the site
  (`GET /api/browse/tags` -> the API's `tagsuggest`, best first; 250 ms after typing, no counts), and their
  namespaces join the comics' ones (`namespaces.addComics`) so they're colored and grouped before any comic of the
  site is in the library.
- **Tags**: each in the namespace a download gives it (`galleryInfo`: the plugin's mapping - artist/parody/
  character -> creator/series/character - else the site's own: female, male, language, group, other...), each with
  its own namespace (`Gallery.SiteTags`: one name can be in two - female:glasses, male:glasses; `TagTypes`, by name,
  collapsed them into one tag twice, and the repeated key stopped the details panel from showing at all - "still
  unclickable comics"); the panel lists each tag once. Downloads too (`galleryInfo`, 2026-10-22): a comic keeps both;
  comics downloaded before get theirs on a refresh (selection > Refresh tags).
- **Loading** (v2 was "buggy, laggy": it paged on its own forever and its covers waited behind downloads):
  - the next site page only when the visible end (`focus.to`, checked 50 ms after the grid moves - the grid sets
    focus after `ensure`) is within 12 tiles of the last, on the Browse screen only - never from the overscan range;
    a round "loading" pill at the bottom while it comes; checked again whenever a page arrives (`checkNear` in
    `settle`: the end may be on screen already, or a page asked for meanwhile was dropped);
  - a new search keeps the old tiles until the new page's first 30 covers are decoded (at most 1.5 s), then swaps
    them all at once (no tiles changing one by one); a spinner in the top bar meanwhile; the cursor resets;
  - covers and thumbnail sprites through `Client.Picture` (its own 16 slots, never the download slots, image/* only,
    at most 4 MB) and a 64 MB in-memory LRU on the server (`pictureCache`), sent as `immutable` for 30 days.
- A tile opens the app's **details panel** (`GalleryPanel` in `DetailsPanel`, laid out like a comic's): cover and
  titles, facts (pages, category - a tap searches it, languages, uploaded, rating, uploader), where it's from (the
  gallery page: open, copy link), the same Tags section read-only (no pencil; the tag menu without namespace/remove:
  search here, a new download...), the pages' thumbnails (the site's sprites - nothing against its image limit; a
  chevron for the next thumbnail page, pulsing placeholders while loading). The panel's header: Download (an icon;
  the gallery's link as a download), then a peach "Downloading" icon; Read (book) when the library has it.
- Server: `GET /api/browse/sites` -> `{sites, sorts}`, `GET /api/browse?site=&q=&before=&sort=` ->
  `{galleries, total, next}` (each with `comic` = the library's comic for its gallery key, `queued` = a download of
  its link waits or runs; with `sort`, `before`/`next` are the list's page), `GET /api/browse/tags?site=&q=`,
  `GET /api/browse/gallery?site=&id=&token=&p=` -> `{thumbs: [{page, url, x, y, w, h}], pages}`,
  `GET /api/browse/image?site=&u=` (a cover or sprite from the plugin's own addresses, with its login).
  The engine reads covers/rating/uploader from the gdata API (`thumb`, `rating`, `uploader`) and thumbnails from
  the gallery page's `#gdt` boxes (`parseThumbs`; logged in, each box sits inside another - the first version only
  read the logged-out page and found no thumbnails).
- **Browse: the pages as a carousel (2026-10-22)** (the user: "the pages should be a horizontal carousel"): the
  details panel's Pages are one row of thumbnails (150 px tall, each in its page's shape, snapping as it's swiped;
  no scrollbar), the site's next thumbnail page loading as the row's end comes near (no "More pages" button), a few
  pulsing placeholders at the end meanwhile. With a mouse: round arrows at its ends ("Earlier pages" / "Later
  pages", only where there's more), and the wheel moves it sideways until an end (then the panel scrolls on).
- **Browse: faster, and a new search interrupts (2026-10-22)** (the user: "too slow and sluggish", "a new tag should
  interrupt the current search"). Measured on the server: a search ~1.3 s (the list page ~0.7 s through the VPN, the
  gdata API after the 1/s turn), the first after a start ~16 s (the login via FlareSolverr), each cover ~0.7 s cold
  / ~0.45 s on a kept connection. Now:
  - a new search aborts the last one's request (and its next page, and its covers' preload - fetches, not Images);
    on the server the request's context ends, and `waitTurn` gives a turn back that a called-off request never used
    (it held up the next request a whole second); results of an older search are never shown;
  - search pages are kept 3 minutes (`browseState`: back, the same search again - at once; ≤ 100);
  - a search's covers are fetched by the server as soon as it has them (`pictureCache.load`: one fetch per picture
    however many ask - the browser joins; a new search calls the last one's prefetch off, a wanted one is fetched
    again);
  - 16 idle connections per host (was 8: the 16 covers at once each made a new TLS handshake);
  - Browse opening (`/api/browse/sites`) logs in ahead (`BrowseEngine.Warm`);
  - the browser waits for the first 24 covers for at most 1.2 s (was 30 / 1.5 s) before swapping the results.
- **Browse first, ahead, remembered (2026-10-22)** (the user: "yes" to Browse going ahead of downloads; "other ways
  to speed up the thumbnails"; "when downloading a comic, choose whether to subscribe"; "a gallery subscription
  should show the comic's title, not the link"):
  - the site's 1/s turns: `waitTurn` is a small scheduler now - a request marked `plugin.Foreground` (Browse's
    search, toplists, thumbnails, tag suggestions) takes the next turn before background ones (downloads,
    subscription checks); the rate is the same; a called-off request never takes a turn;
  - after each page it sends, the server loads the next one in the background (background turns, called off by a
    newer search) with its covers - scrolling on finds both ready;
  - gallery details (gdata) kept 30 minutes per gallery (`ehentai.metas`): a foreground request needs no API call
    when every gallery on the page was seen lately (paging, a search again, back); downloads and refreshes always
    ask (and keep them fresh);
  - the cover cache is 192 MB in memory (was 64: ~6,000 covers);
  - Download in a gallery's panel opens the New download dialog at "once or subscribe" (`openSiteDownload`: the
    gallery's link, its title shown); a subscription to a gallery brings its newer versions in;
  - searches and subscriptions have a `label` (migration 0035): the dialog sends the gallery's title, the Downloads
    page shows it (the link on hover; filter and name order use it too), toasts name it; without one, a gallery
    link the library has a comic for shows the comic's title (`rtrim(comics.url,'/') = rtrim(query,'/')`), a
    subscription's check its subscription's.
- **Covers' overlays and marks (2026-10-23)** (the user: "the overlay ... doesn't animate into view in some situations",
  "the downloaded/viewed icons on comic posters are too big"): a cover's overlays (title and chips, the read/in-library/
  waiting mark, the progress line) stay hidden until its picture has loaded and fade in with it - every time, a picture
  the browser already had too (before, only after the reader closed, and only within 400 ms). The marks are 22 px
  (were 30) with 13 px icons.
- **Browse: broken covers, old covers, endless spinners (2026-10-25)** (the user: "searching comics, just spinners
  forever", "broken thumbnails, thumbnails having old titles on them", desktop only). Measured on the main instance:
  searches answered in 0.1-2.5 s and covers in ~60 ms - but in bursts every picture fetch from ExHentai's picture host
  failed with a TLS handshake timeout (598 requests in one hour, many sharing one failed fetch). Fixed:
  - `Client.Picture` tries a network failure again twice (0.5 s, 1 s) and fetches at most 8 at once per site (was 16:
    a page of covers opened as many handshakes at once, through the VPN);
  - the grid retries a failed thumbnail four times (1, 2, 4, 8 s; was three, 1-3 s), then stops its spinner
    (`img.failed`) - before, a thumbnail that never came spun for good;
  - "old titles": tiles are kept by place, so a new search gave a tile's `<img>` another address - Chrome keeps the
    last picture shown until the new one has come (Safari blanks it: desktop only), for good when it fails. Each
    thumbnail is a new element now (`{#key}` on its address; a collection's mosaic covers likewise) - the library's grid
    after a new search too. Test: "grid: a new search shows none of the last one's pictures".
- **Browse: a new search clears at once (2026-10-25)** (the user: "when entering a new tag it should clear the results"): `BrowseResults.search` empties the list as it starts (the page's spinner until the site answers; before, the last search's galleries stayed until the new covers had come). Test: "Browse - a new search clears the last one's galleries at once".
- **Browse: the pages as they come (2026-10-25)** (the user: "the pages in the details panel has to load every single
  one of the thumbnails before it shows them, it should show them as they are available"): the site gives a page's
  thumbnails as one sheet (measured on ExHentai: 20 to a WebP sheet of ~180 KB; WebP can't draw part of itself), so a
  sheet's thumbnails come together - but every place shows at once with its number, pulsing, and each sheet's
  thumbnails fade in as it comes (`sheets` in GalleryPanel), a long gallery sheet by sheet. The server starts each
  sheet the moment it has read the gallery's page (`prefetchSheets`; the browser's request joins it) and reads the
  next thumbnail page ahead (kept a few minutes: `browseState.thumbs`), so the strip's scrolling finds it ready.
  Download in the panel's header is the accent filled (the user: "the download button should have a purple
  background"). Test: "Browse - the details panel's pages show as each sheet of them comes".
- **Browse: the blacklist applies (2026-10-25)** (the user: "blacklisted tags should apply the browse page if it isnt" - it wasn't): the server leaves out a gallery with a blacklisted tag (`blacklisted` in browse.go: the site's own tags, as a download matches them, and each as the library names it - its name, and ns:name), counting them (`hidden`); the cursor still moves past them. A page left with nothing goes straight on to the next (the page's spinner meanwhile, never "No galleries match" while the site has more). Tests: `TestBrowseBlacklisted`, "Browse - a page whose galleries are all blacklisted goes on to the next".
