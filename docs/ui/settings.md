# Settings

Settings sections and cards: Sites, Library, Download rules, Backups, Stats and the problem report, settings shared through the server.

## As built (web client, from 2026-10-14 on)
- **Settings > Sites (built 2026-10-15)** (`SitesCard.svelte`): each site plugin with its host, version, built in /
  installed, tag types; a status (Needs an account / Credentials saved / Connected / Not connected). Sites with an
  account get a credentials form from the plugin's `auth` (labels like "API key", "User ID", the plugin's help text,
  password fields with an eye toggle; a pasted value shows in a code font). Saving writes `secrets.toml` and hands
  the credentials to the running downloader at once (no restart), then tests them; empty boxes keep what's saved;
  "Remove credentials" deletes them. "Test connection" makes one small request. **Credentials go in, never out**: the
  API returns only which fields are set; the request log has paths only. API: `GET /api/sites`,
  `POST /api/sites/{id}/credentials {values}`, `POST /api/sites/{id}/test`. Not yet: installing/updating plugins
  from the app (needs the public plugin index - step 3).
- **Settings on the server (built 2026-10-15)**: default search, default sort, start muted, loop and speed are shared by
  every device (`settings` table, migration `0009`; `GET/POST /api/settings`, an allow-list with a checker per key,
  all-or-nothing). Each device keeps a copy in localStorage (instant start, offline); after logging in the app reads
  the server's values (waiting at most 1.5 s before showing the library, so the first search uses the right default),
  and the first device ever to connect uploads its own values. Changes are sent (only the changed keys, 600 ms after
  the last one); coming back to the app pulls changes made elsewhere. Top spacing stays per device.
- **Settings > Backups (2026-10-16)** (`BackupsCard.svelte`): "Back up every day" (on by default) and Keep 3/7/14/30/60;
  Back up now; one row per backup (when, size, "newest") with Download, Restore and Delete; Restore asks first ("Changes
  since then are lost"), then a yellow banner "A restore is ready" with **Restart now** (the page waits for the server
  to answer again, then reloads) or Cancel restore; "Restore from a file…" stages an uploaded backup; the folder is
  shown at the bottom.
- **Settings in sections (2026-10-16)** (`lib/settingsSections.ts`): Library, Sites, Download rules, Notifications,
  Viewing (videos, hover previews, top spacing), This device (saved thumbnails, screen details), Backups, Account -
  each with a colored icon tile and a one-line summary, and its own address `/settings/<id>` (the navigation menu
  lists them under Settings). Phones: `/settings` is the list; a section opens on its own with a "‹ Settings" back
  link (slides in). Wide (>= 900 px): the list stays on the left (sticky, the current one highlighted) and the
  section shows beside it (`/settings` = Library). The welcome screen's "Open Sites" goes to `/settings/sites`.
- **Settings > Sites redesigned (2026-10-16, user: buttons placed badly on phones)**: each site card = name + details,
  status chip (Testing… while testing) and a ⋯ menu (Test connection, Update to x, Change / Remove credentials, Remove
  site / Use the built-in version). Credentials: the form only while needed (Save full width on phones); once saved a
  "Credentials saved · Change" line. "No account needed" dropped (the chip says Ready). Add site: a full-width dashed
  button under the list, its panel below it. An available update keeps its banner with an Update button.
- **Sites: no duplicate "saved" signals (2026-10-16, user)**: the status chip says Ready once credentials are saved
  (the card's "✓ Credentials saved" line says the rest); that line has no Change button - Change credentials is in
  the site's ⋯ menu.
- **Settings > Library and a slimmer top (2026-10-18)**: the default search is the library's own search bar
  (`SearchBar plain`: chips, autocomplete, the filter menu without Sort, no saved-searches star; a word left in the
  box becomes a chip when you leave it). Default sort and new files order use the action menu (`ActionMenu current`:
  a field showing the choice; sort = field, then direction). The top bar is the menu button's height (44 px on
  phones, 40 px with a mouse) and the search bar just fits a chip (44 / 38 px); "Nothing matches this search" is as
  wide as the search bar.
- **Settings saved when leaving (2026-10-19)**: shared settings are sent 0.6 s after the last change; a change still
  waiting when the page is hidden or closed goes at once (`fetch` with `keepalive`), so a toggle right before
  switching apps or reloading isn't lost.
- **Adult sites behind a switch (2026-10-20)**: Settings > Sites > Add site (and the first steps) list plugins marked
  nsfw only after "Show adult sites (18+)" (since the overhaul below: the "Adult (18+)" chip).
- **Logins in a dialog (2026-10-23, user: "the login inputs shouldnt be in the main site card")**: a site card no
  longer holds the boxes. It shows one line: "✓ Login saved", or "Login available" / "Login needed" (peach, sites
  that require one) with "Log in" - the line opens the login dialog, as does the ⋯ item "Log in" / "Change login"
  ("Remove login" stays there). The dialog (`Modal`): the plugin's help, the boxes (empty; saved ones marked
  "saved"), the eye, and **Test / Cancel / Save** (the three share the row on phones). Test (user: "there should be a
  test button in the dialogue") tries what's typed over what's saved without saving it - `POST /api/sites/{id}/test
  {values}`, a throwaway copy of the site's client (`Client.WithCredentials`: same connections, fresh state, its own
  login session); with nothing typed it tests the saved login. A result goes when a box changes. Save closes the
  dialog and tests at once, as before.
- **Add site overhauled (2026-10-23, user: not animated, and 100 sites would be a massive list)**: the sites not
  installed, A to Z, in one bordered list (like Tags' list): the site's icon, address (one line, adult badge after it),
  name · version, the description clamped to two lines, Install. Past 8 sites: only the first 8 and "Show all N sites",
  plus a search (address, name, description; matches all shown) with the Adult (18+) chip beside it; without the
  search the chip ends the kinds row. Kind chips (All / Image boards / Galleries / Social, from the index's `kind`)
  only when two or more kinds are offered. "No sites match." (+ "Some adult sites do" when hidden ones would). Motion:
  the panel slides open/closed; a kind or a search picked: the rows going fade at once and close, the new ones open at
  the same time, the rows staying move with the list's height (2026-10-24 - gliding rows held the ones going outside
  the list's flow, drawn below it while the new ones came in: the user saw the old cards "remain on the screen";
  `reveal` with `quick`), an installed site slides out (none
  with reduced motion). Phones: the panel loses its dashed frame so the rows get the width. Same panel in the first
  steps (`SitesCard welcome`).
- **Stats (2026-10-19)**: Settings > Stats (`StatsCard`, `GET /api/stats?days=&tz=`; read on opening and every
  30 s while on screen). Library: files, total size, tags in use as three tiles; a bar of images / GIFs / videos
  (by count) with counts and sizes; tag links (and how many are the user's), parents and siblings, untagged files,
  deleted (undo window) and deleted for good. Imports: a bar per day for the last 30 days (the caller's time zone);
  pointing at a day reads it out above the chart. Downloads: per site the files it gave (still in the library) and
  posts seen; subscriptions (paused, failing). Storage: each disk's free space (originals and data on one line when
  they share a disk; red under 10 % free), then originals, database (+WAL), thumbnails, previews, backups. Server:
  version, uptime, memory, OS/arch and CPUs (Docker or not), ffmpeg and libvips versions, Go. Counted by SQL on
  meta.db (milliseconds; the time is shown) plus the disks (`internal/diskfree`: statfs / GetDiskFreeSpaceEx).
  **2026-10-28: the live files' numbers from the index** (`index.Census`: kinds and sizes, first import, imports per
  day, untagged, tag links - each tag once per live file; `index.SiteFiles`: files per site, as the From filter
  counts them) - the SQL took 4-6 s on a library of 330k files and 16M tag links (measured per query: counting
  file_tags 1.5 s + the user's 1.3 s, files per site 0.8 s, untagged 0.6 s, four scans of files ~0.8 s). The user's
  links and the trash use partial indexes (migration 0043); posts per site group seen_posts along its key.
- **Problem report (2026-10-19)**: the last card of Settings > Stats - Download (a .txt), Copy, Show (the text in
  the card, wrapped). Made by the server (`GET /api/report`, internal/server/report.go): the setup (version, OS,
  CPUs, Docker, memory, uptime, ffmpeg/libvips), the counts, storage and disks, per site its engine, version,
  login (how many fields set, never values), on/off and counts; the queue by state, the last failed searches and
  failing subscriptions with their errors; the settings (numbers and booleans as they are, text and lists only
  "set"/"empty"); then the last 5,000 log records. The server writes every log record to a small log file too
  (DataDir/logs/vitrine.log, JSON lines with the attributes apart, rotated at 4 MB with one older file kept -
  `internal/logring`, a tee in front of the text handler; HTTP server errors and handler panics go there too), so
  the report spans restarts and crashes (a deploy restarts the server: memory alone held only minutes) and
  private values are hidden by key: searches, tags, names, links, hosts, paths become placeholders (search-1,
  tag-2 - the same value, the same placeholder, so the log still reads); site IDs and addresses become site-1...
  wherever they appear; free text (errors, messages) loses links, quoted text, e-mail addresses, key=value secrets
  and long key-like strings. A request's path stays readable (hashes and site IDs inside it are hidden); versions
  and listen addresses are kept. Errors in the web app (uncaught errors, failed promises; `lib/errorlog.ts`, ten
  per page load, each once, the page's path without its search) are sent to `POST /api/log` and logged as
  "web app error" (20 a minute at most), so the report has them too.
- **Safe for work (2026-10-23)** (the user, picked from the options: hide the "I came" button, hide adult site plugins,
  neutral words everywhere - not a filter on the files themselves): Settings > Viewing > Content > "Safe for work"
  (shared setting `sfwMode`, off by default). On: `settings.came` is false whatever `cameButton` says - no "I came"
  button, Rankings tab or page, "Times came" sort, count in the details, notice kind - and the "I came" setting itself
  is hidden; adding a site (Settings > Sites, the first steps) never lists plugins marked `nsfw` in the plugin index
  and shows no "Show adult sites (18+)" switch or "adult" badge (installed ones keep working). Vitrine's own texts
  have no adult words otherwise. Test: "SFW mode" (e2e).
- **Stats on phones (2026-10-23)** (the user's screenshots: "overflowing text, data not displaying but ... instead"): a
  disk's free space goes to a line of its own when it doesn't fit beside its name and hint; the three tiles size their
  number to the tile (`min(22px, 18cqi)`, a container query) - "495.2 GB" whole, no "...". (The old rules were for
  screens up to 420 px only; the user's phone is wider.)
- **Media and Comics can be switched off (2026-10-23)** (the user: "an option to disable library and comics ... one HAS
  to be enabled ... prompt the user if they want to delete all the data ... where they need to actively type
  something"; and "in the initial setup the user should pick what they want to enable"): Settings > Viewing > Content
  has a switch each (shared settings `mediaOff`, `comicsOff`; the server refuses both off - `SetSettings`); the last
  one on can't be switched off. Switching one off asks first ("Its N subscriptions and N downloads are removed"), then
  "Also delete everything in Media/Comics?" with N files/comics and their size - the action waits for "delete all"
  typed; Cancel keeps everything (hidden until switched on again). `POST /api/sections/{media|comics}/off {delete}`:
  the setting, the section's subscriptions and downloads removed (the user's choice: removed, not paused; gallery
  sites = Comics, the others = Media), and with delete everything deleted for good in the background
  (`DeleteAllMedia`: into the trash in batches of 2,000, then the trash emptied; `DeleteAllComics`), a notice when
  done. Off, a section's menu entries and pages go (Media: Media, Duplicates, its Trash page; Comics: Comics, Browse,
  its Trash page) and their addresses open the other section; its sites aren't offered for new downloads (`siteOn`).
  Switching on again asks nothing. The first steps start with "What will you use?" (Media, Comics; one at least).
  Tests: `TestSectionOff` (Go), "sections: switching Media off", "first steps" (e2e).
- **What's new (2026-10-23, the user: "add a changelog somewhere in the app starting with 1.0" ... "it should be its
  own page")**: Settings > What's new (the last section, a star), also linked from the About card. It shows the
  repository's CHANGELOG.md, imported raw at build time (lazily: only when the page opens; the Dockerfile copies it
  into the web build) - a card per version, "Not released yet" for Unreleased, bold lead-ins kept. Keeping it
  current: docs/project/rules.md.
- **Comics sort (2026-10-25)** (the user: "the comic library needs its own default sort setting"): Settings > Library (then Media) has "Comics sort" under Favorites sort (not shown while Comics is off) - the shared setting `comicsSort` (default newest added; the server checks it like the other orders), offering the Comics page's own orders (last read, title, pages...). The Comics page opens in it as the library opens in its default order (its first search waits for the shared settings); the order last picked is no longer kept per device (`vitrine.comicSort`). A direction inside a sentence keeps a leading letter or name as written (`midSentence`: "Title - A to Z first", was "a to z"). Test: "comics: its own default order".
- **Library, and Browse search (2026-10-25)** (the user: "the comics browse page needs a default search setting", "the media page in settings needs renaming as it implies that these settings are for the media page only"): the section is "Library" (hint: default searches and orders, new files, the trash) - Media and Comics alike; its fields are named for their page: Media search, Media sort, Favorites sort, Comics sort, Browse search (the last two only while Comics is on). Browse search is the shared setting `browseSearch` (the gallery site's tags as Browse writes them; the server checks only its length): Browse opens with it (after the shared settings are read), else the site's newest galleries. Stats keeps "Media" for its files card (`menu.library`). Test: "Browse opens with its default search".
  - **Media and Comics apart (2026-10-25)** (the user: "there should be a separate media and comics section in there"): Library is three cards - Media (default search and sort, Favorites sort, new files order, bulk downloads as new, the files and tags count), Comics (its default sort - "Comics default sort" to screen readers - and Browse search; only while Comics is on), Trash (when it empties: both pages' trash). The fields' names are plain again inside their card (Default search, Default sort).
  - **Browse search in Browse's own bar (2026-10-25)** (the user: "the filter menu for browse comics in the default setting should be the same one thats on the page"): the field is `BrowseSearchSetting` - it provides Browse's scope, so its filter menu is Browse's (category, language, pages, rating, expunged, torrent) and its suggestions the gallery site's own tags (the first site to browse); no site row or order (the setting is for every site). Test: "settings: Browse search has Browse's own filter menu".
  - **... and its orders (2026-10-25)** (the user: "so should the sort menu"): the field's menu is Browse's "Sort and filters" (SearchBar `sortable`: plain, yet with the order) - newest or one of the sites' own lists (popular, the toplists; all the sites' together). Picking a list clears the search and a search goes back to newest, as on the page (`browseSort`, a shared setting). Browse opens in that list when its site has it (no search then), else with the search, newest first. Test: "settings: Browse search has Browse's orders too". In Settings the order picked is the default being set: no "not the default order" dot on the filter button, and its menu counts that order as the default (no Reset) - the user: "the not default shows which makes no sense".
  - **A default search and sort for each (2026-10-25)** (the user: "there needs to be a default search and sort for both the all comics page and the browse page"): Library's cards are Media, Comics, Browse (the last two while Comics is on) and Trash. Comics: Default search (`comicsSearch`, in the Comics page's own bar - `ComicsSearchSetting` provides its scope: its filters and the comics' tags; checked by the server as a library search) - the page opens with it (not Favorites, the trash or a collection; a search in the address wins) - and Default sort. Browse (`BrowseSettings`): Default search, and Default sort as a field too (newest or a site's list; the same setting as its bar's menu). Screen readers tell them apart: Comics default search/sort, Browse default search/sort. Test: "settings: Comics and Browse each have a default search and sort". The Browse page counts that order as its default too (the list set when the site has it, else newest): opened in it, no dot (the user: "i still see the not default dot on the browse page itself").
