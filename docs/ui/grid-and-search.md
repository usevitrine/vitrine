# Grid and search

The library's search bar and chips, saved searches, sort and filters, the grid (squares or masonry, virtualised, never a page wall), the fast scroller, new files arriving, hover previews, the selection bar.

## Browsing large results: never a page wall
(hydrus-web loads 256 files at a time; its full-screen viewer jumps back to file 1 at file 256 instead of loading
more, and closing the viewer after swiping to file 8,000 leaves the grid at the top.)
- The server keeps each search's complete sorted result under a **result ID**; the client fetches **ID slices by
  position** ("500,000-500,200 of result X" - jumping to the middle is as fast as the top). Client memory stays
  constant at any library size. Small results (< ~50k) are sent whole in the first response.
- Thumbnails and metadata load in windows around the current position, ahead of the scroll/swipe direction.
- Browsers can't make a page tall enough for 1M thumbnails, so the grid has its own **fast scroller** (drag handle with
  date labels, like Google Photos).
- **Grid and viewer share one list and position**: the grid follows the viewer while swiping, and closing animates the
  file back into its thumbnail (like iOS Photos) with a brief highlight - at any distance.
- The end of the results is the end (wrap-around only as an option).
- The result list is a **snapshot** - new imports don't shift it; a "N new files" pill refreshes it (or, by setting,
  arrivals are taken in automatically while at the top). Files new since "Mark all as seen" come first, marked NEW.
- The URL holds the search and the current file, so a reload or reopening the home-screen app returns to the same place.

## Selection bar and the iPhone's bottom edge (2026-10-21)
- The selection bar: Clear and the count stay at the left; the actions are **square icon buttons** (all the same size,
  a title/label each) in a strip that **scrolls sideways** when it doesn't fit (snapping to buttons, soft edges where
  more is) and sits at the right when it does. Comics add **Add to collection** (a menu: the collections, New
  collection…) - on a collection's page **Remove from collection** instead - and Mark read.
- **Labels where they fit** (2026-10-23, the user: "the buttons ... need labels and since there's no overflow the extra
  space doesn't matter"; "on the left side, the first button is always under the gradient"): each button shows a
  short word beside its icon (Select all, Tags, Refresh, Collection, Read/Unread, Favorite, Delete...) - on phones too
  ("the buttons need labels on mobile too"): the strip scrolls sideways when they don't fit.
  The strip is right-aligned by an auto margin on its first button (a spacer before them made the strip settle a few
  pixels in, the first button under the left soft edge).
- **What can't be picked** is dimmed while selecting and left out of drags and Select all: a collection's tile
  (comics go into a collection, never a collection into another - the user found the bar offered it), and on Browse a
  gallery the library has or whose download waits.
- The home-screen app on iOS runs to the screen's rounded corners and the home indicator, yet without
  viewport-fit=cover iOS reports no bottom inset: `html.standalone` on touch screens sets `--home-bar` (20px, below the
  selection bar and the video controls, and the toasts above the bar) and `--safe-corner` (10px more at their sides).
  Sheets and the reader's slider keep their own room.

## As built (web client, from 2026-10-14 on)
- **Search bar**: booru text -> chips (a finished word becomes a chip on space and searches at once); chips colored
  by kind (system blue, sort/limit peach, wildcard yellow, excluded red, OR groups tinted mauve); tap a chip to edit
  in place (text with autocomplete, exclude toggle, add OR alternative, split a group, remove); Backspace on an empty
  input pulls the last chip back into the text. Autocomplete (counts, siblings shown "-> ideal") on the word being
  typed.
- **Grid**: virtualised (only visible rows + margin in the DOM), square tiles (3+ columns on phones, ~170 px on
  desktop), the 4x4 placeholder behind each thumbnail, duration badges, late thumbnails filled in. IDs come in
  500-ID slices of the server-side result and file details in batches around the visible rows (a screen behind, two
  ahead).
- **Search chips have an ×**: each chip in the search bar has an × on its right that takes
  it out of the search at once; tapping the chip still opens the editor (OR alternatives, include/exclude, split),
  which no longer has a "Remove chip" button.
- **Saved searches (built 2026-10-14)**: a star at the right end of the search bar (filled mauve when the current
  search and order are saved) opens a panel under the search bar (under the star on desktop, full width on phones):
  save the current search with a name (the chips are the default name), or see "This search is saved as X"; the list
  shows each saved search's name, its query as colored chips, a non-default order ("Newest uploaded") and how many
  files it finds now; one tap runs it with its order. Edit: rename in place, drag (or arrow keys on the handle) to
  reorder, delete with Undo. **Update after a change** (2026-10-23, the user: "pressing edit and then update and then
  done is silly, the edit menu should be for renaming and reordering"): the saved search last run (or matched -
  `Saved.lastRun`) - changed since, the panel opens on "Changed from X" with **Update** (the main button) and the save
  box as "Save as new"; Edit has no Update any more. **Long searches** ("take up too much room"): a saved search's chips
  show two lines at most, "+N" over the end of the second for the rest (`clampChips`: the chips past the edge counted);
  its order sits on the name line. The e2e test of the panel looks only at its own saved searches (one another test left on
  the same worker's server made it fail now and then). Esc / a press outside closes (Undo in a toast doesn't);
  arrow keys move between saved searches. Stored on the server (`saved_searches`, migration `0008`; API
  `GET/POST /api/saved`, `POST /api/saved/{id}`, `/api/saved/{id}/remove`, `/api/saved/reorder`), so every device
  has the same list - the first server-side setting.
- **Fast scroller (built 2026-10-14)** (`FastScroller.svelte`): for results taller than three screens the grid's native
  scrollbar is hidden and a handle sits on the right edge - a tab with a grip, slid off the edge but for a 9 px sliver
  in the grid's side gap while idle (a thin pill until 2026-10-20; only the sliver takes presses then), sliding in
  while scrolling, hovered or dragged. Drag it to jump anywhere (pressing the track did until 2026-10-20); while
  dragging a bubble shows the date of the files there (month and year; the day for spans under two months) or, for
  orders that aren't by date, "12,345 of 87,000", and year marks appear along the track. Dates come from
  `GET /api/results/{id}/dates?field=imported|uploaded&n=200` (200 evenly spaced samples of the result, by the
  current order), so labels are instant where nothing has loaded. While it's dragged, thumbnails load only where it
  pauses (120 ms). Not done: the browser's maximum element height (~33M px) for results of hundreds of thousands of
  files on a phone (90k files at 3 per row is ~3.6M px, fine).
- **Search box border** follows the text field only (`:has(input:focus)`), so pressing the star or a chip no longer
  flashes it.
- **"N new files" pill (built 2026-10-14)**: the grid's result list is a snapshot - downloads never shift it. Every
  15 s while the app is on screen (and when it comes back to the screen) the client asks `GET /api/new?q=&since=`
  (`since` = the server's time of the result, returned by `/api/search` as `now`) how many files matching the search
  were imported since; a mauve pill at the top of the grid says "↑ 12 new files" (a + for oldest-first orders).
  Tapping it runs the search again and, for newest-first orders, scrolls to the top.
- **New files first (built 2026-10-15)**: one shared setting `seenAt` (Unix ms, set by "Mark all as seen"; the
  first sync sets it to now, so an existing library starts with nothing new). Files imported after it are **new**:
  `/api/search?new_since=&pin=1&new_sort=` puts the matching new files first (`Index.PinNew`, in the "New files
  order" setting: newest imported / oldest imported / newest uploaded), the rest keep the chosen order; the reply has
  `new` and `pinned`. The grid gives the New files their own rows, then a thin mauve divider, then the rest (index
  <-> cell mapping skips the empty cells after the New rows); new tiles carry a small mauve **NEW** mark. A bar under
  the search says "12 new files" with **Mark all as seen** (seenAt = the result's server time, so files arriving
  meanwhile stay new) and a fold arrow: folded (per device, `newFolded`) the new files go back to their usual place,
  still marked. Setting **When new files arrive**: Automatically (the list takes arrivals in at once, but only while
  the grid is at the top - further down the "N new files" pill instead, so nothing moves under the finger) or Show a
  button (default). **App icon count** (Badging API: iPhone home-screen apps, installed desktop apps): the number of
  new files, set by the app and by the service worker when a push arrives. A "new files" notification opens
  `/?new=1`: the whole library (no search, no default search), new files first. The service worker keeps saved
  searches under their address without the new-file parameters (they change whenever files are marked as seen).
- **Site picker in the library**: a pill on the status line ("1,234 files · ... [All sites
  ▾]") picks which site's files to show. Its menu lists All sites and each site by address with how many of the
  current search's files came from it (`GET /api/search/sites?q=` - the search without its site chip). Picking a site
  puts a `system:site=<id>` chip in the search (shown as "from danbooru.donmai.us"; replaces an earlier site chip), so it
  combines with everything, shows in the URL and in saved searches; All sites removes it. The pill turns mauve when a
  site is picked.
- **Hover previews (built 2026-10-15; a card since 2026-10-19; the tile itself since 2026-10-20)**: with a mouse
  (`(hover: hover) and (pointer: fine)`), the mouse resting still 200 ms on a tile zooms that tile itself (no copy over
  it): its place and size transition to the card's (CSS transitions on transform, width and height; 340 ms, quint
  out), above its neighbors, the crop opening up to the file's own shape, corners rounding and a shadow deepening;
  moving the mouse shrinks it back (200 ms; `.shrinking` keeps it on top and lets the mouse through). The card:
  about 1.5 tiles across by area (2.2 until 2026-10-20: too big), centered on the tile and kept inside the
  grid. The thumbnail shows at once; the video or bigger picture loads only once the card has finished growing
  (decoding a high-resolution one meanwhile made the animation stutter); a video plays in it muted and looping (a 3 px mauve line on a dark track shows
  how far), a picture loads its 1600 px copy (`previewURL`), a GIF its original. Any move of the mouse (more than
  a twitch) closes it - resting again opens the one under it (found by position, `tileAt`: the tile shrinking back
  may still cover others). A click anywhere on it opens the viewer from the card (`tileRect` returns the zoomed
  tile's rect; it snaps back, without its animation, once the open animation has taken over), not from the small tile.
  Waits for the tiles' own slides (the first row's gaps closing, files arriving, a re-order) to finish before it
  opens: opened mid-slide it started from the tile's new place while the tile was drawn on its way (a jump to the side).
  Also ends when the grid scrolls (a wheel scroll never opens one) or a button is pressed; never while selecting or
  with the file menu open. Phones: none.
  Shared setting `hoverPreview` (Settings > Videos > "Play videos on hover", on by default): off, a video's card
  shows its thumbnail, grown (the card itself always opens; until 2026-10-21 the setting turned cards off too).
- **Filter menu (built 2026-10-15; like hydrus-web's system predicate menu, but
  only what Vitrine has)**: a filter button in the search bar (left of the star) opens a list grouped When (Imported,
  Uploaded, Modified) / File (Type, Length, File size, Width, Height, Has audio, No audio) / Other (Number of tags,
  Rating, Site, Limit); two columns on desktop. Picking one opens a short form (time: In the last N units / More than
  N units ago / Since / Before / On a date; numbers: a comparison in words + value + unit; Type: Is/Is not + several
  choices; Rating, Site: Is/Is not + one) with a live preview of the chip, then "Add filter"; Has/No audio are added
  at once. Tapping a filter chip opens its form filled in ("Update" replaces it). Filter chips read in plain words
  ("imported in the last 5 days", "longer than 30 seconds", "larger than 1 MB", "no audio", "videos or GIF", "first
  1000"); the text syntax is unchanged. Code: `lib/system.ts` (definitions, build, read, describe),
  `components/SystemMenu.svelte`. Hydrus-style typed phrases ("system:has audio") are deliberately not translated.
  The search bar's typing box now drops to its own line when chips leave less than 7em (it used to run under the
  buttons on phones).
- **Sort moved into the filter menu (2026-10-15)**: the menu is now "Sort and filters": a Sort
  section on top (the four orders as choices, the default marked, a typed `sort:` shown too; picking one applies it
  and closes the menu); the filter button gets a mauve dot when the order isn't the default. The top bar shows the
  file count ("26 files", "Searching…") where the sort button was; the search time ("under 1 ms") is gone. The line
  under the search keeps errors, "No tag ..." and the site picker. Settings > Library > Default sort is unchanged.
- **Sort row redesigned (2026-10-15: options mustn't take more space as they grow, no layers of
  dropdowns)**: Sort = one field dropdown ("Sort by": Imported, Uploaded; the default marked; a typed `sort:size` etc.
  is listed with its own words) + a two-way direction switch whose words fit the field (Newest/Oldest; ready:
  Largest/Smallest, Longest/Shortest, Most/Fewest; none for random). Both apply at once and the menu stays open; a
  press outside the menu and search bar closes it (also new for the whole menu); "Reset to default" next to the
  heading when the order isn't the default. Adding a field later = `sortFields` in `lib/system.ts`.
- **Site picker moved into Sort and filters (2026-10-15)**: a **From** row under Sort - one dropdown ("Show files
  from": All sites, then each site by address with how many of the current search's files came from it, from
  `/api/search/sites`); picking puts or replaces the `system:site=` chip, All sites takes it out; applies at once, the
  menu stays open. The "Site" entry left the Other group (tapping a site chip still opens its form, e.g. for "is
  not"). `SitePicker.svelte` is gone; the line under the search only appears for offline / errors / "No tag ...",
  and the header keeps the same gap above the grid either way.
- **Drag a chip onto another to make an OR group (built 2026-10-15)**: in the search bar, drag a chip (mouse: past
  6 px; touch: press and hold 350 ms, then slide - a quick tap still edits, a swipe still scrolls) onto another chip:
  its alternatives join the target as an OR group in the target's place (duplicates dropped; groups combine too). The
  chip lifts (dimmed in place) and a floating copy follows the pointer - on touch 52 px above the finger so the target
  stays visible; it lives on `<body>`, above the top bar; the chip under it gets a dashed mauve outline and "OR". A drop
  elsewhere cancels; the drop's click doesn't open the editor. Limit and sort chips don't combine. While a held chip
  moves, a non-passive `touchmove` stops the page scrolling. E2E: mouse drag on all projects, a synthetic
  press-and-hold on the iPhone project.
- **New files without a section (2026-10-15, replaced the New bar/rows/fold)**: new files are simply first in the main
  grid (same rows as the rest) with their NEW mark; a mauve **"5 new ×"** pill beside the file count in the top bar
  marks them all as seen (seenAt = the result's time).
- **"Bulk downloads count as new" (2026-10-16)**: files remember whether they came in bulk (`files.bulk`, migration
  0014; set on import: one-off downloads, a subscription's first check and `vitrine import` are bulk, a subscription's
  regular check - one that stops at the last check's newest post - is not). Setting `newFromBulk` (default on, Settings
  > Library): off sends `new_bulk=0` to `/api/search` and `/api/new`, so only files from regular checks are pinned
  first, marked NEW, counted in the "N new ×" pill and the app icon. The NEW mark follows the server (the first
  `pinned` files).
- **Search bar (2026-10-16)**: chips and the typing box on **one line** that scrolls sideways (no scrollbar, soft
  fades at an edge with more chips beyond - overlays, not a CSS mask, which hid the suggestions inside it; the typing
  box is a small target at the end, 2.5em plus what is typed, filling the line only when the chips leave room; a tap
  on empty space there focuses it; typing keeps it in view). The clear ✕ replaces the magnifier at the front when there is something
  to clear; on the right only Sort and filters + Saved searches, 34 px wide. Autocomplete suggestions line up with the
  whole search box (TagInput `anchor`, position: fixed, so the sideways scroller doesn't clip them).
- **Search chips show whole tags, without their type (2026-10-16)**: no width cap (a long tag makes the chip as wide
  as it needs; the row scrolls), text left-aligned; a tag's namespace (creator:, character:, ...) isn't written on the
  chip or in saved searches - the color says it - while the hover text, the editor and the search keep the full tag.
  Only real tag types are hidden (`splitTag`), so a site tag like rating:general keeps its colon.
- **More sort orders (2026-10-16)**: the Sort menu's "Sort by" and the Default sort setting offer Imported, Uploaded,
  Modified, File size, Length, Width, Height, Number of tags (each with its own direction words: Largest/Smallest,
  Longest/Shortest, ...) and Random (no direction; a refresh keeps the same shuffle - `random_<seed>`). Dropdown menus
  cap their height (60vh) and scroll.
- **Sort as pages (2026-10-16, replaced the field dropdown + direction switch)**: in Sort and filters the order is one
  full-width button ("Uploaded  Newest first  >"). It opens a **Sort by** page (the orders; the current one in mauve
  with its direction, "default" on the default; a back arrow), and an order with two directions opens its own page
  (Newest first / Oldest first, a check on the current one, a back arrow). Picking applies it and returns to the menu;
  Random (no direction) applies straight from the list. The From site picker is full width too.
- **Arriving files never move the screen (2026-10-18, user)**: no "N new files" button and no setting for it -
  arrivals (checked every 15 s and after downloads add files) are always taken in (`results.merge()` returns how many
  came in ahead of the old first file; new files always go first). The grid adds them as **whole rows** above what's
  on screen: it keeps `lead` empty cells at the start of the first row, which the next arrivals fill, and moves the
  scroll position down by the rows added (after a tick, so it isn't clamped) - every tile stays in its place. A new
  search or order closes the gaps. The viewer stays on its file (its index follows it); a tap opens the file the tile
  showed when pressed (`indexOf`), even if the list changed meanwhile. Fixes: with newest-first and downloads
  running, taps opened the neighboring file and the open file changed by itself.
- **Arrivals without hiccups (2026-10-18)**: `results.merge(keep, onShift)` swaps in the new list, moves the viewer
  to its file and adds the grid's rows in one step (a search hook: `prepare` fetches the IDs around the open file's
  new position first, `commit` runs before anything renders), so the details panel never sees another file for a
  moment (it reloaded and jumped to the top). The viewer's panel also keeps the last file while the next one's info
  loads. The grid's top-row gaps (left so nothing on screen moves) close once that row scrolls into view - tiles
  slide into place, one changing rows fades in.
- **Fast scroller on either side (2026-10-19)**: on touch screens each touch on the grid decides the scroller's side
  (left half: left, mirrored; right half: right; not while the scroller itself is held) - for a left-hand thumb.
  (A touch that stops a scroll still gliding isn't reported to the page on iOS, so the side only changes on a
  touch after the glide - tried first move and lift-off too in 2026-10-19, no difference; kept simple.)
  Mouse: always on the right.
- **Suggestion taps on phones (2026-10-19)**: `TagInput` handles a tap on a suggestion itself (touchstart/touchend;
  moving > 10 px = scrolling the list, not a tap): the faked mouse events phones send afterwards don't come if the
  finger moved a little and arrive after the box lost focus (keyboard closing, list moving away), so taps were
  missed. touchend's preventDefault keeps the focus and the keyboard; the pick is the suggestion touched, even if
  the list changed meanwhile.
- **Search line follows added chips (2026-10-19)**: a chip added from elsewhere (a tag's "Add to search" in the
  details) scrolls the search line to its end - the new chip and the typing box in view. A search filled in from
  nothing (the page opening) still reads from its start.
- **Thumbnails on their way (2026-10-19)**: a tile whose thumbnail hasn't loaded shows a small spinner over its
  color preview after 0.5 s (never for a normal load); a thumbnail that failed (many asked at once after a long jump)
  is asked again up to 3 times, 1-3 s apart.
- **Suggestions above the keyboard (2026-10-19)**: the suggestion list's height is capped at the room between the
  box and the bottom of the visual viewport (above the on-screen keyboard and the bar iOS puts on it), kept up to
  date as the keyboard opens.
- **Search chip menu (2026-10-19)**: a tapped search chip opens a menu like a tag's in the details
  (`ChipEditor.svelte`): the tag (namespace color and name) with its text editable in place (and its OR
  alternatives), then Exclude/Include again, Add alternative (OR), Split into chips (a group), New search (only this
  chip), New download (a single included tag: the Downloads form, written the chosen site's way - see
  `search.tag_query`; an OR group of included tags: "Download any of them", the site's OR search - `newDownload(tags,
  { any: true })`, only sites that have one; added 2026-10-22), Copy (with and without the namespace), Remove from search. Under its chip on wider screens
  (340 px, kept inside the bar), an edge-to-edge sheet on phones (drag down or tap outside to close); Escape closes
  it from anywhere (an open suggestion list first). Typed changes apply on Enter and on closing; with a mouse the
  text is ready to type in, on touch screens no keyboard pops up. Each opening is a new menu (`{#key}` outside
  `{#if}`); a closing one is inert and aria-hidden while it fades.
- **Filter menu on phones, several sites (2026-10-19)**: on phones the sort-and-filter menu is a bottom sheet like
  the other menus (edge to edge, grab bar, drag down or tap the dimmed page to close; its forms' dropdowns float
  and open upwards when there's more room there). From opens a page of its own: All sites and each site with how
  many of the search's files came from it, ticked sites in mauve; several at once (one `system:site=a,b` chip, read
  "from a or b"; the row says "All sites", one or two names, or "3 sites"); applies at once and the page stays.
  A site filter chip's form takes several sites too. (Dropdown fix: a floating list measures its whole height, not
  the room it was first given, so near the bottom of the screen it opens upwards.)
- **New files at the top follow you up (2026-10-19)**: scrolled to the very top, arriving files come into view -
  the grid stays at the top, the new files fade in at the start and the others slide along to make room.
  Scrolled down, as before: they go in above without moving anything on screen.
- **Grid width after another screen (2026-10-20)**: the fast scroller used to drop while the library was hidden (no
  height), so the browser's scrollbar came back for a moment and the grid kept a width without its room - the tiles
  stopped ~15 px short of the right edge (seen in Brave with a big library). It now stays as it was while hidden, and
  the width is measured again whenever the scroller (and so the scrollbar) comes or goes. e2e/scrollbars.spec.ts runs
  with real scrollbars (the other tests' browser hides them).
- **Fast scroller at the page edge (2026-10-20)**: on desktop the library's fast scroller (the grabber) sits against
  the window's right edge (left, when moved there), past the grid's 12 px side gap (`--grid-gutter`).
- **Search bar as wide as the grid; download form labels above (2026-10-19)**: on desktop the search bar has the
  grid's side gaps (12 px), so its edges line up with the thumbnails. The New download form's options (First check /
  Files, Repeat, Group) have their labels above them on every screen; a long group name is cut short with … (the
  whole name on hover - every Vitrine dropdown does this now) instead of running over the Subscribe button.
- **Grid changes animate (2026-10-19)**: when the list changes (a delete, a bulk tag edit that drops or brings in
  files, an undo - or another search that shares files on screen with the last one, e.g. a chip removed or added),
  the grid moves by file rather than by position (FLIP, `playFlip` in
  Grid.svelte): just before the new list renders, the visible tiles' files and places are noted (and copied);
  afterwards tiles slide from their old place to the new one, removed files' copies fade out (shrinking) where they
  were, and newcomers fade in; old places are taken from the screen (the scroll may change: a shorter list), and a
  tile coming from more than 1.5 screens away fades in instead of sweeping across. Nothing on screen changed (details
  arriving, scrolling): no animation; another search with nothing in common on screen: none either; files arriving at the top keep their own animation (`addAtTop`). Every new
  list brings its on-screen part with it (`results.focus`), so tiles deep in the list don't go blank.
- **Hover opens at once, with intent (2026-10-21, user: "remove the delay"; then "shifting zoom animations")**: no 200 ms
  rest any more; the card stays while the mouse is anywhere on it (like Comics), onto another tile that one opens. A mouse
  sweeping across the grid (> 0.35 px/ms, smoothed) opens nothing on the way - each tile passed over used to pop a card
  out and back - and opens 90 ms after it stops; slowing over a tile opens it at once. **Not while the grid still
  scrolls** (a wheel's glide; user: the shift was "more common after a scroll"): a card opened in the glide was closed
  by the next scroll step and opened again; now a move during it waits until the grid has been still 150 ms, then
  opens the tile under the mouse (a mouse resting through a scroll opens nothing).
- **Opening from a playing card** (user: lag with a high-res video): the card's video is handed over - the viewer's
  carries on from its time (`lib/videoHandoff`) - and let go of at once (paused, file dropped), so two copies of a big
  video don't decode during the opening; hover videos are let go of when their card closes too (`release`).
- **The hover card is never smaller than its tile either way (2026-10-21, user: "still has the wobble while
  zooming")**: same-area sizing made a wide picture's card shorter than its square tile (a tall one's narrower), so the
  picture filling the card shrank while the card grew. The card is scaled up to at least the tile in both directions
  (then fitted to the screen, which can still shorten extreme shapes).
- **No browser scrollbar with a mouse (2026-10-22, user: tiles shifted sideways after a long list gave way to a short
  one)**: the fast scroller showed from three screens on and the browser's scrollbar below that, which came and went with
  the list's length - the grid's width changed by its room and every tile moved. With a mouse the browser's scrollbar
  never shows and the fast scroller is there whenever the list scrolls; touch screens (overlaid scrollbars, no room
  taken) keep the three-screen rule and their own indicator.
- **One motion for every change (2026-10-21, user: "inconsistent and clunky")**: every path that changes what's on
  screen - a refresh or new search (`playFlip`), files arriving at the top (the list-change animation now, no copy of its
  own), the first row's gaps closing (`closeGaps`: a snapshot, then the same) - goes through `animateFrom` with one set
  of timings (`MOTION`): tiles that stay slide (340 ms, the standard ease; into another row diagonally; rows tiles
  that change size grow or shrink from where they were); one from more than a screen and a half away leaves and appears;
  new tiles appear (320 ms, fade + grow from 0.92) one after another in reading order (18 ms apart, at most 12 steps);
  removed ones leave (200 ms, ease-in, fade + shrink to 0.92). A new search with nothing in common now animates the same
  way (it used to swap at once); the first results just show (thumbnails fade in as they load). Tiles off screen get no
  motion. **The hover card is left out** (open or shrinking back: user, it "suddenly zooms in even more then returns" -
  a list refresh measured it at card size against its small tile and grew it by that much more). Collections opening/closing keep their own fly (`playMorph`). Reduced motion: none.
  **Pages shown again** (user: back from Comics the thumbnails just showed): the library and Comics stay mounted while
  hidden; a grid shown again (its height back from 0) - or Comics' first results, opened later in the visit - has the
  tiles on screen appear (`appearAll`); only the app's very first results just show.
- **Rows is the default layout (2026-10-24, the user)**: a device that never picked starts on Rows (listed first
  in Settings and the first steps; a device that has used the app keeps the layout it saved). The end-to-end tests
  start on Squares (e2e/fixtures.ts) unless a test picks one - most were written for squares.
- **Masonry is now called Rows** in Settings (2026-10-21; the stored value stays `masonry`).
- **Masonry is justified rows (2026-10-21, user: the order "is insane")**: the column version (file n in column n mod
  cols, each column stacking) drifted apart - columns of different heights put file 32 beside 36 and 39. Now
  `lib/masonry.ts` lays out rows: tiles left to right at the aimed height (the old column width) until they fill the
  width, then the row is scaled to fit it exactly (all its tiles one height, each as wide as its shape; the last row keeps
  the aimed height, not stretched). `visible`/`indexAt` go by rows; tiles have their own width (`layout.w`). The
  notes below about columns are history.
- **Masonry grid (2026-10-20)**: Settings > Viewing > Grid > Tiles: Squares (default) or Masonry, per device
  (`settings.gridLayout`). With masonry the search asks for `shapes=1`: every file's width/height x 1024 in result
  order (uint16 little-endian, base64; ~2 bytes per file, 1024 = square while a file isn't probed yet). The client
  lays the whole result out at once (`lib/masonry.ts`: equal columns - phones 2+ of ~180 px (squares: 3+), desktop ~236 px (squares ~170); gaps 10 px desktop / 6 px phones (squares 2 px), rounded corners (8 / 6 px), and on phones a side margin as wide as a gap, each tile into the next column, left to right, row by row (2026-10-20: "masonry should be a row-based left-to-right layout"; before, the shortest column), height
  capped at 0.45-2x the width; ~1 ms for 100k files) and keeps each column's tiles for a binary search of what's on
  screen. Grid.svelte goes through `place(slot)` for every position (tiles, reveal, the viewer's open/close rect,
  FLIP, hit testing); new files at the top: no first-row gaps in masonry - the first tile on screen keeps its place
  (`masonryAddAtTop`), at the very top the FLIP animation slides the rest along. Turning it on re-runs the search
  for the shapes (tiles slide into the new layout).
- **Hover card: no wobble (2026-10-21)**: any mouse move of 4 px closed the card (it shrinks back; resting opens
  it again) - a hand drifting a few px while it grew made it shrink and grow again, swinging to and fro at the
  grid's edge (where it grows off to one side). While it's still growing, moves under 16 px don't close it; once
  open, 4 px as before.
- **Fast scrolling in big libraries (2026-10-21)**: measured on a fake 50,000-file library (Chromium, phone size,
  CPU 4x slower, a 20 ms network): a sustained fast scroll (~8,000 px/s) ran at ~48 fps (p95 33 ms) with hundreds
  of thumbnails loading on the way; now 60 fps (p95 17 ms). Over 2.5 px/ms (`trackSpeed`; not the grid moving
  itself) tiles coming into view show their color preview only, thumbnails seen before still show, and the rest
  load 80 ms after it slows down; file details load six screens ahead the way it goes (two when not flicking); once
  slow, the next half screen's thumbnails are fetched and decoded ahead. The service worker saves new thumbnails in
  batches (100, or 1.5 s after the last) and skips ones it saved this session (it looked each one up in its store
  first). A slowing flick (a phone's momentum) was already smooth and still is. WebKit on Windows can't stand in for
  an iPhone (30 ms a frame even for a tiny scroll with no new tiles).
- **Tiles ready further ahead (2026-10-21)**: the tiles that exist are the screen plus a screen and a half the way
  it scrolls and half a screen the other way (never less than 3 rows; it was 3 rows each way). iOS moves the page
  itself, ahead of the app, so with 3 rows the empty background showed at the bottom while scrolling, even at an
  easy pace; tiles beyond the screen show their color preview at least.
- **No sound from the move animation's copies (2026-10-21, user: audio started on the grid as downloads came in)**:
  the list changing animates by copies of the tiles on screen (`cloneNode`); a copy of the open hover card kept its
  video's address and autoplay but not its muting (a property), and the copies of tiles that stay are never put on the
  page - one played off the page with sound (masonry arrivals, any refresh under a resting mouse). Copies lose their
  videos the moment they're made (`still`).
- **Only new tiles drawn as it scrolls (2026-10-21)**: a tile whose file and place are unchanged keeps its object, so a
  scroll step draws only the tiles coming in (new objects re-drew all of them: ~280 on a 2560 px screen). Measured
  with real middle-button autoscroll in a visible Chromium (120 Hz, 2560x1300 at 1.5x, realistic 52 KB thumbnails,
  80 ms API delay), ~40,000 px/s: script 2.0 s -> 0.84 s per 4 s, main thread busy ~95% -> ~73%. At such speeds
  the tiles show their color previews and the thumbnails come in as it slows (by design).
- **A suggestion picked over a highlighted word (2026-10-22)** (the user: "if I select a tag in the autocomplete
  while the text is highlighted it doesn't become a chip"): the word being completed was found from the selection's
  start, so the pick went in front of the highlighted word ("blue_sky blu") and stayed typed text; TagInput now
  works from the selection's end (the highlighted word is replaced; the pick becomes a chip as usual).
- **Suggestions found inside names, marked, namespaces as chips (2026-10-22)** (the user: "modern_warfare should
  match call_of_duty_modern_warfare", "show the search highlighted in the tags", "the namespace should be a chip"):
  the index (`matchText`) finds the text anywhere in a name from two characters on - names starting with it first,
  then names with a word starting with it (after `_`, `-`, `(`...), then inside a word, each most used first (one
  character: the start only); the comics' site tags (`ComicSiteTags`, the Downloads form for gallery sites) the same
  way in SQL. Each suggestion shows its namespace as a small chip in its color, then the name with what was typed in
  a `<mark>` (TagInput `marked`). ~2 ms over 85k tags (BenchmarkAutocomplete). An anchored suggestion list lives at
  the top of the page (over dialogs: the New download dialog's search box), and opens upwards when there's little
  room below and more above (a phone sheet's box near the keyboard). **In a phone sheet nothing floats** (2026-10-23; the user, three times: the suggestions
  "still float" on an iPhone with the keyboard up - placed over the page by the page's coordinates they hung far above
  the box, and a fix that only held on one browser is no fix: "this project isn't just for me"): TagInput `above` puts
  the list in the sheet itself, over the box (`.wrap.above`: column-reverse, the list in the flow), so the sheet -
  anchored above the keyboard - grows up with it, up to the top of the screen; the list's height is what the sheet can
  still grow by, measured within the sheet (its max-height less the rest). The New download sheet uses it; a card on
  wider screens keeps the floating list.
