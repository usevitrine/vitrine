# Downloads and subscriptions

How the downloader behaves: searches and the queue per site, resuming, the blacklist, managing searches, subscriptions and their groups, retries, parallel downloads, download rules, a user's favorites, check depth.

## Downloader behavior
Lessons from self-hosted download setups built on gallery-dl and companion tools (problem in brackets).
- **Downloads go straight into the library**, file by file - no import queue, no double imports, no staging buildup.
  (A separate import step imported files twice, and one big search filled most of the staging space.)
- **Exact resume**: each search/subscription stores its cursor (page + post) after every file; pause, reorder, crash
  and power cut all carry on exactly. (gallery-dl restarts from page 1; an approximate resume needs patches.)
- **"Seen" records written per file**, never only at the end of a run. (gallery-dl's archive-mode "memory" left
  nothing after a killed run -> re-downloads.)
- **Stops only between files** - never a half-written file.
- **Ask the library before downloading**: both first sites give the md5 up front; skip files the library has or has
  tombstoned.
- **A worker per site**: sites download in parallel; queue order top-to-bottom per site, switching only at file
  boundaries (reordering above the running item takes effect at the next file). (One worker for everything never
  preempted.)
- **Per-site limits from the plugin** (requests/s, connections); ETA from measured speed.
- **Parallel downloads** (discussed 2026-10-14): sites always run in parallel (a worker each); within a site, up to
  the plugin's `connections` files download at once (a fast CDN: 3-4; a site that caps each IP: 1 - see the
  plugins repo's site notes). Optional
  segmented downloads (several connections per file) only where a plugin allows it. Rules that still hold: the resume
  cursor is a **watermark** - it only advances past a post once every earlier post is done (a crash can re-fetch an
  in-flight file, never skip one); pausing stops new starts and lets in-flight files finish; files still get their
  real name only when complete; searches within a site still start in list order (parallelism speeds up the current
  one, it doesn't jump the queue); tag refresh runs at lower priority. A global bandwidth limit setting.
  **Across pages** (2026-10-20): once a page's posts have all started and a connection is free, the next page is
  fetched (below the page's oldest post) and its files start - before, a file that stalled at the end of a page (a
  rule34 CDN server going quiet: 1 min until given up, then a retry) held every connection idle until it came.
  **A connection per file** (HTTP/1.1, 2026-10-20): over HTTP/2 every file from a host shared one TCP connection - rule34's
  CDN throttled it and all files crawled at a few KB/s together (a retry came back on the same connection; a restart
  helped for a while). **Trickles are given up too**: less than 1 MB in a minute (not only no data at all), then
  retried on a fresh connection. A failed post's Retry carries on if the request goes (its result is recorded).
  A Retry looks the post up on its own: on a gelbooru site whose answer for one post has no upload time (rule34's JSON)
  it asks the search for `id:N` (it has `created_at`); the tag refresh does the same for a file still without one. A
  retried file is "found by" the search that failed it (migration 0030 added that for files retried before).
  At start, files without an upload time on gelbooru sites go first in the tag refresh (seen_at 0; one the site still
  can't date goes back to its usual turn).
  **Resuming** (2026-10-21): a file that breaks off (a stall, a trickle, a cut connection) carries on where it stopped
  with a Range request when the site says `Accept-Ranges: bytes` and answers 206 from exactly that byte (up to 20
  times, each bringing something), on the same slot - the store gets one stream (its hashes cover the whole file).
  A site that ignores Range: given up as before. **A Retry doesn't queue** behind a running search's files: a free
  slot, or one kept for it (`plugin.Urgent`: at most one connection over the site's limit). One Retry per post at a time
  (pressed again: it waits for the running one). A stall is given up after 20 s (less than 256 KB in 20 s; it carries on
  from there, so giving up early is cheap). A file that stops at the same byte even when carried on is **cut short**
  (`ErrCutShort`, "the site's copy of this file looks broken - it always stops at X of Y MB"): not tried again from the start (it would stop there
  too). rule34's file servers (nginx) answer Range with 206 (checked 2026-10-21). **Retries run in the background**
  (`StartRetry`, 2026-10-21): the API answers at once (202); `Retries()` lists the running ones and those finished in the
  last 5 min with their result (`/api/downloads` `retries`); a retry's file is in the live progress under its site.
  **Mastodon: a status's files newest first** (2026-10-21): post IDs are status*10 + the file's number, and a status's
  files came in that order (IDs going up) - the resume point ("below the last done") then skipped a status's later
  files for good when a search was interrupted part-way through it (several deploys during a first download lost
  some fourth pictures). Now its last file first (IDs always going down), and resuming below a status's later file
  asks for that status again (max_id = status+1).
  **Counts after a resume** (2026-10-21): the posts finished past a resumed search's point come again; one this
  search recorded before this run of it began isn't counted again (a failed one that now succeeds moves to its new
  count) - before, every restart inflated "handled" and "already".
- **Blacklist checked against the post's tags before download**; file rules (skip/tag images over N megapixels or
  videos over N MB; mp4 vs webm preference per site).
- **Staging budget with backpressure** - if anything stages, wait instead of shutting down when space runs low.
  (Some downloaders stop their workers until restarted.)
- **Start-up tolerant** of dependencies not being up yet (VPN container, network).
- **Searches -> subscriptions without overlap**: a subscription created from a search starts at the search's cursor.
  Subscription groups with shared settings; moving a subscription between groups keeps its history; a changed query
  counts as a new search; "Check now" never undoes a reset; accurate counts across runs (counts that only
  cover the last run mislead).
- **Tag refresh** of existing posts on a schedule (see `docs/downloader/tag-refresh.md`).
- One-off clean-ups: delete by tag combination; purge files with newly blacklisted tags.
- Push notifications (Web Push) for finished/failed searches and problems; live progress from engine events (not log
  parsing).
- Optional egress/VPN check before downloading.
- Clean metadata: tags + post/source URLs only, no bookkeeping tags.

## Checks first, pages ahead (2026-10-21)
- **Subscription checks go first** (user: checks piled up behind a big download): with the shared setting
  `checksFirst` (Settings > Sites > Queue order, on unless turned off) a site's queue picks its checks before other
  searches (`next`), and a running download (not a check) looks every 3 s for a check waiting: it steps aside after the
  posts in flight (`stop`), goes back to `queued` at its place and carries on from `low_id` once the checks are done.
  The Downloads page lists waiting checks first too (and reorders in that order).
- **A first check is a bulk download** (2026-10-23, the user: "subscriptions still arent prioritized over bulk
  downloads (bulk downloads includes initial downloads that i put in a sub group)"): only a check for what's new
  (`stop_id > 0`: it stops at the newest post the last one saw; `checkSQL`, `Search.isCheck`) goes first and never steps
  aside; a subscription's first check (it takes everything) waits its turn and steps aside for checks like any download
  (before, any search with a subscription counted - a big first check held up every check behind it). `/api/downloads`
  marks it `first_check` (the page's order follows).
- **Post pages opened ahead** (`prefetch.go`): engines whose list pages give only IDs (HTML gelbooru, Shimmie2,
  favorites pages) open each post's page before its file; while a file downloads, the next post that will need its page
  (not seen, not a known deleted file) has it opened, at the same request pace; the post takes it (or asks again if it
  failed). Measured on realbooru: a page alongside a download doesn't slow it; extra file connections are throttled
  (2 connections ~ half the total, 4: three at ~0.09 MB/s), so files stay one at a time. Gains: images ~a third faster;
  videos ~1-2%.

## A user's favorites (2026-10-20)

- **One search on every engine**: `fav:<user>` (`fav:me` = the account in the site's credentials), or a pasted
  Gelbooru favorites address (`...page=favorites&s=view&id=N`). `plugin.Favorites` reads it; each engine's Search
  turns to its favorites path:
  - **gelbooru-v02 / gelbooru-v02-html**: the user's ID number (`fav:me` = the `user_id` credential). The favorites
    pages (`<site>/index.php?page=favorites&s=view&id=N&pid=<offset>`, on the site's own web host - from post_url
    when the plugin may fetch it, else base) are read with `Client.WebPage`: never with the API key in it, same rate
    limit, through FlareSolverr behind Cloudflare. Posts come back partial (ID + md5 from the thumbnail name);
    Lookup fills in the ones needed.
  - **shimmie2**: its own `favorited_by=<name>` search (`fav:me` = the `username` credential).
  - **mastodon**: only your own (`fav:me`, with an access token): `/api/v1/favourites`, paged by its Link header
    (`Client.APIHeader`).
- **Order**: favorites come in the order they were added, so the list is read whole (kept 30 minutes; a fresh start
  reads it again) and handed out newest post first, like any search - resuming by post ID works.
- **Subscriptions**: a check of a favorites search goes through the whole list (`plugin.IsFavorites`: the StopID
  stop is skipped) - a post favorited since can be older than the newest one the last check saw. Posts seen before
  are skipped without a request, so a check costs about one request per 50 favorites.
- **The form**: a muted heart at the end of Downloads > New download's search box fills in `fav:me` where the
  credentials say who you are (`fav_me` in /api/sites; `fav_by` says how the site names a user: id, name, only yours).
- **Into Vitrine's favorites**: a download or subscription can add the files it brings in (downloaded, or already in
  the library) to the library's favorites - `favorite` on searches and subscriptions (migration 0022; a check copies
  its subscription's), read once per run. The form offers it ("Add to my favorites", on) for `fav:me`; an existing
  favorite keeps the time it became one.
- **Its own group**: a subscription to anyone's favorites goes in its site's "Favorites" group (made the first time;
  also when a favorites search becomes a subscription); the form has no group to pick for it.
- **Removed on the site**: a favorites subscription keeps which of its posts are in the library (`fav_members`,
  migration 0023). A complete check (from the newest post, not resumed, not cut short by a limit) marks the ones it
  no longer found as gone - unless it found no posts at all (more likely the site failing). The subscription then says
  "N no longer in your favorites" with Keep (not asked again; favorited again, followed again) and Delete (a confirm
  with their pictures; the library's undo window). `GET /api/subscriptions/{id}/fav-gone`, `POST .../fav-gone-delete`,
  `.../fav-gone-keep`.
- Not tested live yet (2026-10-20): needs checking against real sites from the app.

## Check depth (2026-10-20)
- A subscription's check normally stops at the first post at or below the newest one the last check saw (`stop_id`).
  A post tagged after it was checked (an artist tag added a day later) sits further down the search and is missed.
  **Check depth** N lets a check go on for N posts past that point (posts it has are skipped as usual, so it costs
  API pages, not downloads).
- Set per subscription (`subscriptions.check_depth`), else its group (`sub_groups.check_depth`), else its site
  (setting `siteCheckDepths`: plugin ID -> posts), else for all (setting `checkDepth`); default 0 = today's
  behavior. NULL / missing = not set at that level. Range 0-100,000.
- A check copies the depth it starts with into `searches.stop_depth`; the posts past `stop_id` are counted in memory,
  so a check resumed after a restart counts again (a few posts more, never fewer). Favorites checks skip the stop
  altogether (see above).
- API: `check_depth` on `POST /api/subscriptions/{id}` and `POST /api/sub-groups/{id}` (-1 = not set); both list
  their own `check_depth` and the effective `depth`. UI: "Check depth" in a subscription's and a group's ⋯ menu
  (Default (what applies otherwise), None, 25 ... 1,000 posts), in a site's ⋯ menu in Settings > Sites, and a number
  in Settings > Download rules.

## As built (from step 1, 2026-10-14 on)
- **Downloader** (`internal/download`, migration `0003_downloader`): `searches` (site, query, state queued/running/
  paused/done/failed, queue position, `max_posts`, `low_id` resume point, `high_id`, counters) and `seen_posts`
  (site, post, file, outcome 1 downloaded / 2 already / 3 deleted before / 4 failed / 5 skipped). One worker per site
  works its queue in order (an interrupted "running" search first); sites run in parallel. Per post: seen before ->
  already; md5 in the library -> attach this site's tags/links to that file (cross-site dedupe), md5 tombstoned ->
  skip; else download straight into the store, set site tags + post/source/file URLs + rating - and on to the next
  post: downloads never wait for processing (2026-10-20). Probing and the thumbnail are made by the media worker
  (`library.RunMedia`, half the cores; woken by each new file, newest first in batches of 32, restart-safe as it
  reads what's missing from the database); preview frames, duplicate hashing and screen-sized copies were already
  background work. Until its thumbnail is ready a new tile shows the small "making the thumbnail" mark.
  The post's outcome and the new `low_id` are written in one transaction after each post. A canceled download
  leaves nothing (the store's temp file is removed) and is fetched again on resume. Failed posts are recorded (with
  the error and the search that met it - migration 0020) and passed; a later search that meets one tries it again,
  and Downloads lists them under **Failed files** (post linked to the site, site, search, why, when) with Retry per
  post and Retry all (`GET /api/failed-posts`, `POST /api/failed-posts/retry {site, post}`: the post looked up on
  its site and handled like any post; its new outcome replaces the failure, a new failure keeps it listed); Dismiss
  (`POST /api/failed-posts/dismiss {posts: [{site, post}]}`, 2026-10-21) deletes their `seen_posts` rows - forgotten, so a
  later search that meets one tries it again. Subscriptions carry `last_found_at` (newest downloaded `file_sources` row,
  index from migration 0034) for the Downloads page's "Nothing new in 3 months". a failing site request marks the search failed with the reason.
- Tests run against a fake gelbooru site: full run, resume after a stop at post 12 with no file fetched twice, a stop
  mid-file, `max_posts` and queue order, md5 skips (known + deleted), site down. Live: 3 Safebooru posts, then the
  same search again -> 3 "have", nothing downloaded.
- CLI: `vitrine download [-max N] <site> <search>` (carries on an unfinished identical search), `vitrine sites`,
  `vitrine credentials <site>`. `vitrine serve` runs every site's queue in the background; API `GET /api/downloads`,
  `POST /api/downloads {site, query, max_posts}`.
- **Not done yet**: file rules, subscription groups, parallel file downloads within a site (watermark rule), retries with backoff,
  progress events (SSE), Web Push, egress/VPN check, sites without an API (see the gelbooru-v02-html engine below).
- **Blacklist (2026-10-14)**: tags whose posts are never downloaded (table `blacklist`, migration `0005`). An entry is a
  tag or a `*` pattern (`ai_*`), matched against a post's whole site tag ignoring case; entries are normalized (lower
  case, spaces as underscores). A blacklisted post is recorded as seen (outcome 6, counter `blacklisted`), so a rerun
  doesn't page through it again - a gallery-dl problem where filtered posts didn't count as seen - but it is
  checked again on every run, so removing an entry lets it download. The downloader reads the blacklist at the start
  of each search. Edited on the Tags screen (moved from Downloads 2026-10-14; paste a whole list) or `vitrine blacklist list|add|remove|matches`.
  API: `GET/POST /api/blacklist`, `GET /api/blacklist/matches` (per-tag counts, total, size),
  `POST /api/blacklist/delete` (deletes matching library files the normal way - undoable, tombstoned; returns the
  IDs for Undo). Matching library files are found with a search per entry, so tag parents/siblings count too.
- **Managing searches (decided 2026-10-14)**: pause/resume (pausing stops new starts, an
  in-flight file finishes; resume continues from `low_id`), remove (finished, failed or running - a running one stops
  at the next file boundary), drag to reorder (takes effect at the next file). **Removing a search only takes it off
  the list; its downloaded files stay in the library** (deleting files is a library action). A **"Clear finished"**
  button removes all done searches at once, besides a remove button on each; **"Retry failed"** (2026-10-19) resumes
  every failed search at once, each from where it stopped.
- **Managing searches - as built (2026-10-14)**: `internal/download/manage.go` - `Pause` (queued/running ->
  paused; a running search sees a stop flag before its next post, so the file in flight finishes), `Resume`
  (paused/failed -> queued at its place; continues from `low_id`), `Remove` (stops a running one the same way, deletes
  the row; `seen_posts` and files stay), `ClearFinished` (deletes `done` searches; failed ones stay for Retry),
  `Reorder(site, ids)` (positions). API: `POST /api/downloads/{id}/pause|resume|remove`, `POST /api/downloads/clear`,
  `POST /api/downloads/reorder {site, ids}`; searches carry `position`. Web: Queue card (per site, in order; drag
  handle - also arrow keys - pause/resume, remove) and Finished card (newest first; Retry for failed, remove, "Clear
  finished"). E2E uses two offline test plugins (`test-down` refuses, `test-slow` never answers) so no real site is
  contacted.
- **Subscriptions (built 2026-10-14)**: a search checked again on a schedule
  (`internal/download/subscribe.go`, migration `0006_subscriptions`). Table `subscriptions` (site, query - unique per
  site -, `interval_s` 1 hour to 1 year, `paused`, `high_id`, `initial_max`, counters `checks`/`downloaded`/`last_new`,
  `error`, `last_check_at`, `next_check_at`). **A check is an ordinary search** in the site's queue
  (`searches.subscription_id`, `stop_id` = the subscription's `high_id`), so it shows progress, pauses, resumes exactly
  and survives restarts like any search; it is done on reaching a post at or below `stop_id`. Done -> its counts move
  to the subscription (`high_id` = max), the search row is deleted, the next check is due `interval` later. Failed ->
  the check stays (in Finished, with Retry) and is retried automatically after `min(interval, 1 h)`. The first check
  goes through the whole search (or `initial_max` posts) and is queued at the end; so are later due checks (since
  2026-10-19: downloads you added go first - before, checks jumped to the front); **Check now** puts a later check
  at the front (after the running search). A scheduler goroutine starts
  due checks every 30 s and when kicked. **Search -> subscription without overlap**: the bell on a search creates a
  subscription with the search's `high_id` (the search carries on or stays finished; older posts a `max_posts` search
  never reached aren't fetched); a search that hasn't started is replaced by the first check (its `max_posts` becomes
  `initial_max`). Pausing a subscription pauses its check in progress; removing a check skips it (next one a full
  interval later); "Check now" ignores the schedule (also when paused); Unsubscribe deletes it and its check, files
  stay. A post retagged on the site to match an old subscription isn't found (it's below `high_id`) - tag refresh
  is a separate feature. API: `POST /api/subscriptions {site, query, interval, initial_max}` or `{search_id,
  interval}`, `POST /api/subscriptions/{id} {interval?, paused?}`, `POST /api/subscriptions/{id}/check|remove`;
  listed in `GET /api/downloads` (`subscriptions`, each with its `check`). Web: a "Repeat" dropdown in the New
  download form (Once / every hour ... every week; max posts = the first check's limit), a Subscriptions card (state,
  interval, last/next check, files, Check now, interval menu, pause, unsubscribe), a "Check" badge on check rows, a
  bell on searches. Default interval from the bell: 12 hours. Not yet: subscription groups, per-subscription
  options, tag refresh of a subscription's posts, notifications.
- **Automatic retries (built 2026-10-15)**: a failed search (not a subscription check - those keep their hourly retry)
  is queued again by itself after 1 min, 5 min, 30 min, 2 h and 6 h (`searches.attempts` = failures in a row, reset by
  any handled post; `retry_at`; migration `0011`; the scheduler loop requeues due ones); after that it waits for
  Retry. The Finished card says "Trying again in 4 min (attempt 2)" or "Stopped trying after 5 attempts". A file
  download that fails in a way that may pass (network error, 429, 5xx) is tried twice more after 2 s and 10 s; a 404,
  a web page instead of the file (`plugin.ErrNotAFile`) or an unsupported type isn't.
- **Parallel downloads (built 2026-10-15)**: a site downloads up to its plugin's `limits.connections` files at once,
  posts started in list order; each post is recorded when done, and the resume point (`low_id`) only moves past a
  post once every newer post on the page is done (a crash re-fetches at most the posts in flight, never skips one;
  their counters may count them twice). Pausing lets in-flight files finish. Measured on one site's CDN:
  one at a time ~18 MB/s, three at once ~50 MB/s (that plugin uses 3); sites that throttle extra connections stay at 1.
- **Download rules (built 2026-10-15)**: Settings > Download rules - "Skip videos larger than N MB" (checked from the
  file response's Content-Length before reading the body) and "Skip images larger than N megapixels" (from the post's
  width x height, when the site gives them). Shared settings `skipVideoMB` / `skipImageMP` (0 = no limit); read at the
  start of each search. A post over a limit is outcome 7 (TooBig), counted as skipped (now shown in the download's
  numbers) and checked again on later runs, so raising a limit lets it download. Video = the file URL's extension.
- **Spread-out checks (2026-10-15)**: a subscription's next check (after a check, a skipped check or a failed one) is
  scheduled up to a tenth of its interval late at random, at most 30 min (`Downloader.jitter`, 0..1; tests set it to
  0), so subscriptions added together drift apart instead of all hitting the site at once.
- **Subscription groups (built 2026-10-15)**: groups the user names, one site each (`sub_groups`, migration `0013`;
  unique name per site, case-insensitive; `subscriptions.group_id`, NULL = no group; `interval_s` = the group's usual
  schedule). Group actions apply to every member: check now, pause, resume, the check interval (also stored as the
  group's schedule); rename; delete (members stay, ungrouped). `Downloader` `Groups`, `CreateGroup` (an existing name
  is reused), `RenameGroup`, `DeleteGroup`, `SetGroup` (same site only), `GroupAction`, `SetGroupInterval`. API:
  `GET /api/downloads` has `groups`; `POST /api/sub-groups {site, name}`, `POST /api/sub-groups/{id} {name?,
  interval?}`, `POST /api/sub-groups/{id}/{check|pause|resume|remove}`; subscribing takes `group_id` or `group_name`,
  `POST /api/subscriptions/{id} {group_id}` moves one (0 = none).
- **The post limit counts files (2026-10-15, user request)**: a search's `max_posts` (the form's "Files", a
  subscription's first check) now stops once that many files are in the library from it - downloaded or already
  there; posts skipped as blacklisted, over a download rule, deleted before or failed don't use it up. With parallel
  downloads no new post starts while the ones in flight could reach the limit; if some of them don't count, the search
  carries on. The progress bar shows files / limit; rows say "(up to N files)"; the form explains it under the box.
- **The limit counts posts gone through (2026-10-24)**: `max_posts` stops a search once its posts gone through
  (`handled`: new, had, blacklisted, skipped, deleted before, failed) reach it - what the Downloads page counts against
  it. Before, it counted the files that ended up in the library (new + had), so blacklisted posts didn't use it up
  (the user changed it: the page's count and the limit should be the same thing). `TestMaxPostsCountsPosts`.
