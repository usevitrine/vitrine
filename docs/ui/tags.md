# Tags in the UI

The tag menu, tag actions in the details, the tag browser and Tags screens, the selection's tag editor, tag rules, picking several tags.

## As built (web client, from 2026-10-14 on)
- **Tag menu (2026-10-15, replaced the row of buttons)**: tapping a tag in the details opens a menu (`TagMenu.svelte`) -
  at the tag with a mouse, an action sheet along the bottom on phones. Header: the tag and its namespace. Search: Add
  to search / Remove from search, Exclude / Include again, New search (just this tag). New download: Downloads with
  the form filled in (the plain tag without namespace - sites search by it - and the file's site when the tag came
  from one; `router.downloadDraft`). Copy: the full tag, and for namespaced tags "Copy without namespace" (the text
  shown on each item; clipboard API, execCommand fallback; a toast). Remove from this file (red). Escape / tap
  outside closes only the menu; arrow keys move through it. Ctrl/Cmd-click (add) and Alt-click (exclude) stay.
- **Tag actions in the details panel**: tapping a tag opens a row under its group: **Add**
  (to the current search), **Exclude** (adds `-tag`), **Only this** (searches just the tag). If the search already has
  the tag, Add becomes **Remove** (from the search); if it already excludes it, Exclude becomes **Include**. The tag's
  own chip is replaced (an exclusion becomes an inclusion and back); OR groups are left alone. Tapping the tag again
  closes the row. Desktop shortcuts: Ctrl/Cmd-click adds, Alt-click excludes. The viewer closes and the new search
  runs, as before.
- **Tag browser (built 2026-10-15)** (`TagBrowser.svelte`, top of the Tags screen): every tag with live files and how
  many files have it, in its namespace color; filter (substring, any case), namespace chips (All, Creator, Series,
  Character, ..., General, Meta), Most used / A-Z; 100 at a time with "Show more". Tap a tag: Search (opens the
  library), Rename (only tags the user added - into an existing tag merges; refused for tags with parents/siblings),
  Alias (= make it a sibling of another tag; the only option for tags from sites, because a tag refresh would bring
  a renamed site tag back). API: `GET /api/tags?q=&ns=&sort=count|name&offset=&limit=` (index `TagList`, `ns=-` =
  no namespace; rows say `from_sites`), `POST /api/tags/rename {from, to}`.
- **Tags sub-pages**: the tag browser took too much room above the rules, so the Tags screen
  has two sub-pages: **All tags** (`/tags`, the browser) and **Tag rules** (`/tags/rules`: parents, siblings,
  blacklist). A two-way switch under the heading (`.segmented` in `app.css`, full width on phones) and indented
  entries under Tags in the navigation drawer (the current one highlighted) lead to them; the URL keeps the sub-page,
  so reload and Back work. The router has a `sub` (the path after the screen's own).
  **Since 2026-10-18 Tags is laid out like Settings** (the shared `components/Sections.svelte`): **Tag rules**
  (`/tags/rules`) first and **All tags** (`/tags/all`); phones get a list of the two (each opens as a page with
  "‹ Tags"), wide screens the list on the left and the section beside it. The menu's Tags entry opens Tag rules.
- **Tags in the details (2026-10-16)**: the Tags header has two icon buttons - refresh (read the post's tags from the
  site again; only for files with a post page; it moved there from "Where it's from") and a pencil. Tags show no ×
  until the pencil is pressed: edit mode shows the add box and × on every tag (× marks a tag removed - struck through,
  × again keeps it; new tags show dashed), a note "1 to add, 1 to remove", and the pencil becomes a mauve check that
  saves it all at once ("Tags saved (added 1, removed 1)" with Undo). Swiping to another file or closing the details
  while editing saves the changes. The tag menu's "Remove from this file" still removes at once.
- **Selection tag editor (2026-10-16, replaced "+ Tags" / "− Tags")**: the selection bar's **Tags** button opens a panel
  above the bar ("Tags of 30 files"; full width on phones, 520 px at the right on desktop) listing every tag of the
  selected files (`POST /api/tags/of {ids}` -> each tag with how many of them have it, hidden site tags left out),
  grouped by namespace. A tag on only some files is a dimmer pill with "12/30 +": the + gives it to all (shows
  "all ✓", dashed). × removes a tag from all (struck through; × again keeps it). The add box adds new tags (one every
  file already has is skipped). "1 to add, 1 to remove", Cancel / Save - one `POST /api/tags` for everything.
- **Tag rules boxes (2026-10-19)**: each parent's "add children" box sits under its name, above its children, with an
  Add button (`.add-row` in app.css; it used to trail the children and move as they grew); the blacklist's box has
  an Add button too. (The box binds through a getter/setter: binding a not-yet-set record entry to a `$bindable`
  with a fallback throws `props_invalid_value` and the parents list never rendered.)
- **Several tags at once (2026-10-20)**: the details' Tags header has a "Pick several tags" button (two checks; with two
  tags or more). Picking: a tap on a tag picks it (filled with its namespace's color) instead of opening its menu, and
  a bar under the tags (a header like the panels': "N picked" in bold, × to stop picking, a line under it) offers two rows of equal buttons in three columns, each under a small heading: SEARCH - All
  of them (a new search), Any of them (one OR group), Add to search (each added to the current search; one already a
  chip of its own is replaced); DOWNLOAD - All of them (the form filled with all of them, each written the site's way)
  and Any of them (the site's OR search - only shown where the site the download would use has one: the picked
  tags' site, else the form's last one; `router.downloadDraft` {tags, any}). The button again ends picking.
  **Since 2026-10-23** (the user: the little dialogue "feels inconsistent with the rest of the site"): the bar is one
  row like the selection bar - "N picked", a **Use** button (`ActionMenu`: the usual menu, a sheet on phones - Search
  all of them, Search any of them, Add them to the search, Download all of them, Download any of them, Add them to the
  blacklist) and × - instead of the grey box of button grids. **Picking starts by pressing and holding a tag** (450
  ms, a tick where the phone can; right-click with a mouse, the context-menu key on a keyboard - `lib/longPress`), the
  user: "instead of the multi select button ... press and hold on a tag": the header's pick button is gone; the held
  tag is the first picked, taps pick more, unpicking the last one ends picking (as does ×). The hold isn't also a tap
  (no tag menu), and the phone's own long-press menu stays away.
- **The download blacklist from the details (2026-10-23, the user: "quickly add tags to the blacklist in the details
  panel")**: a tag's menu has Add to blacklist (Remove from blacklist when it's on it); picked tags have Add them to
  the blacklist. A toast says so ("x won't be downloaded any more") with Undo; the list is read the first time a menu
  needs it (`GET/POST /api/blacklist`, the same as Tags > Tag rules).
- **Which site for a new download from tags (2026-10-20)**: a tag's New download (the details' tag menu, a search
  chip) and the picked tags' Download all/any go through `newDownload` (lib/newDownload.svelte.ts): it asks the server
  which sites gave the tags (`GET /api/tags/sites?t=...` -> per tag `[{site, count}]`, live files, most first) and
  ranks the sites that can take them (installed, not off; for Any only sites with an OR search for those tags) by how
  many of the tags they gave, then files. More than one: "Download from which site?" (`SitePicker`, a card / a phone
  sheet): those sites with their file counts ("2 of 3 tags" for a site that gave only some), then "Other sites";
  Escape or a press outside cancels. One: straight to the form on it; none: the file's site, else the form's own.
- **Blacklisted tags marked in the details (2026-10-23, the user)**: a tag on the download blacklist has a red
  crossed-out eye before its name (its tooltip names the entry; its label says "x, on the download blacklist"),
  matched as the server matches posts - the whole tag or its bare name, `*` patterns included (`lib/blacklist`, the
  list shared with Tag rules, so a change in either shows in the other at once). Its menu: Remove from blacklist for
  its own entry; one of a pattern is named ("On the blacklist by ai_*", not removable there - taking it off would let
  other tags through too).
