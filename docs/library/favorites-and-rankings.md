# Favorites and rankings

Favorites (the heart), NEW pills in saved searches, view counts, the opt-in "I came" button and the Rankings page.

## View counts (2026-11-01)
The user asked how view counts should work, then: "Everything you suggest sounds good". On by default (Settings >
Viewing, "Count views", shared setting `countViews`; nothing counted, filtered or shown in Safe for work mode).
- **A view**: a file in the viewer for 2 s on screen (an image) or 3 s of play (a video - buffering and paused time
  don't count); swiping past doesn't. Hover previews and tiles aren't views. Comics' pages aren't (later, if wanted).
  How long goes with it (on screen; a video: played) - "most watched" says more than "most opened".
- **One view per visit**: a view starting within 30 minutes of the file's last ending adds its time to that one
  (swiping back and forth, the app put away and back, two devices) - decided on the server.
- **Stored**: one row per view (`views`: file, start, ms; migration 0050), so any period can be counted later (a
  most-viewed section on the Rankings page: not yet - the sort covers it). The index keeps per file the count, the
  total time and the last view's start (`system:views`, `system:viewtime`, `system:viewed`; sorts of the same names).
- **Sent** (`lib/views.ts`): when the file goes out of view (the next one, the viewer closing, the app put away - then
  `fetch` with keepalive, so it isn't lost; not `sendBeacon`: the server takes JSON only). Views that can't be sent
  wait on the device (localStorage) for the next. `POST /api/views {views: [{file, at, ms}]}` -> each file's counts;
  `POST /api/views/clear` (Settings > Viewing, Clear all views; asks first).
- **Seen in**: the details (Viewed "12 times · 8 minutes", Last viewed "Yesterday at 21:04"; not on tiles); the
  filter menu's Views group - Last viewed (in the last / not in the last - never viewed counts as not -, since, before,
  on), Never viewed (`system:views=0`), Times viewed; the orders Last viewed, Times viewed, Time viewed.

## Favorites (2026-10-19)
The one user mark on a file (still no ratings): a heart. Stored in `favorites` (file, when it became one; migration
0018) and held in the index as a bitmap plus a "favorited at" column, so `system:favorite` (also favourite, fav,
favorites; `-system:favorite` for the rest) and `sort:favorite` (newest favorite first; `_asc` oldest) are as fast
as any filter. The bitmap keeps deleted files, so undoing a delete keeps the heart; every search is limited to live
files at the end (this also fixed `system:has_audio` alone showing files in the undo window). Merging duplicates
carries a favorite over to the kept file (and undoing the merge takes it back). Marked from the viewer (a heart in
the top bar, red when on; key L), the file menu (right-click / long-press), and the selection bar (one heart for
the lot: removes them when all are favorites already); `POST /api/favorite {ids, on}`; files report `favorite`.
Favorites show a small red heart at the bottom left of their tile. **Library** is a heading in the menu with two
pages: **Everything** (`/`, the default) and **Favorites** (`/favorites`): the same screen, its searches limited to
favorites (`results.scope`; not a chip - the search bar holds only what was typed, placeholder "Favorites tag
search"), with its own default order (Settings > Library > Favorites sort, default newest favorite first; the filter
button's "not the default order" dot, its tooltip and the sort menu's "default" and Reset all use that page's default). Each page keeps its own search, order and
open file in its URL, so switching between them (the menu, Back) comes back to each as it was. Unfavoriting a file
where the list depends on favorites (the Favorites page, a favorite search or order) takes it off the list like a
delete: the viewer moves on to the next one (the last one closes it), and the notice offers Undo (it becomes a
favorite again, as of then). The filter menu (Other) has Favorite / Not a favorite, the sort menu
"Favorited"; Settings > Stats counts them.

## NEW pills in the saved searches menu (2026-10-19)
Each saved search and each group (parent) tag remembers when it was last opened from the saved searches menu
(`seen_marks`, migration 0019; key `saved:<id>` / `tag:<tag>`; a new entry starts as "opened now", so nothing is new
at first). The menu shows a mauve "N new" pill beside an entry's name for the files imported since then
(`GET /api/news`: the same new-file rule as the library - `new_bulk=0` follows "Bulk downloads count as new"); group
tags are listed below the saved searches (with how many tags they gather) and open the parent tag's search. The
star button shows a dot while any entry has new files (refreshed with the library's 15 s check and when the menu
opens). Opening an entry (`POST /api/seen` - returns when it was last opened before) runs its search with the NEW
pills on its files since then (`results.sinceOverride`), and its own pill goes; the top bar's "N new" broom then
clears only that search's NEW files. Any other search goes back to the usual NEW files (since "Mark all as seen").

## "I came" button and Rankings (2026-10-20; opt-in)
Off by default (Settings > Viewing, shared setting `cameButton`). On: the viewer's top bar has a drop button (key C)
that counts a press on the file in view (`climaxes`, one row per press, migration 0021; `POST /api/climax {file}`), with
Undo in the notice (`POST /api/climax/undo {id}`) and the file's count on the button. Counts can be taken back: the details panel shows
"I came: N times" with two icon buttons, Remove one (the latest press) and Reset (`POST /api/climax/remove {file, all}`), and the
I came page has Clear all at the bottom (asks first; `POST /api/climax/clear`). The index keeps a count per file,
so `system:came>0` (`came`, `climax`, `climaxes`) filters and `sort:came` ("Times came" in the sort menus, only while
the setting is on) sorts like any number. The rankings page (**I came** in the menu, with the drop icon, while it's on; `/rankings`, `GET
/api/rankings?period=week|month|year|all&tz=`): a period switch (last 7 / 30 / 365 days / all time, remembered on the
device), the total, days in a row (ending today, or yesterday when today has none yet) and the first press; a bar per
day (all time: the last year); the 30 most counted files (thumbnails with place and count; "See all" opens the library
with `system:came>0 sort:came`); the top creators and tags (presses on files with the tag, `meta:` left out), each
opening its search. Code and database use neutral names ("climax"); the words are the UI's.

## As built (web client, from 2026-10-14 on)
- **Rankings: characters and series (2026-10-20)**: the "I came" page lists Creators, then Characters, then Series
  (each its namespace, top 20), then the other tags. The menu's order: Library, Downloads, I came, Duplicates, Tags,
  Settings.

## "I came" in the comic reader (2026-10-23)
The user: "There should also be I came support for comics, that shows the exact page you pressed it on on the i came
page". The reader has the button too (C; Settings > Viewing, not in Safe for work), counted on the page showing
(a spread: its first page): `POST /api/climax {comic, page}` (0-based) stores the page's file plus `comic_id` and
`page` (migration 0037), and the count on the button is that page's. The I-came page lists **Comic pages** (the most
counted, the comic's title and "Page N" over the page's picture) apart from Files (the viewer's presses only); a tap
opens the comic on that page (`/comics/<id>?page=N`, 1-based). The total, days and streak count both kinds; the comic's
tags count in the creator/character/series/tag lists like a file's. Undo and Clear work as for files. Tests:
`TestComicClimaxes` (Go), "I came in the comic reader" (e2e).
