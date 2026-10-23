# The Downloads page

New download form, the queue and its stacks per site, subscriptions and groups, finished downloads, failed files and retries.

## Tidied: tabs, summary, site banners, failed by why (2026-10-21, user: "do it all")
- **Stopped sites first**: a site paused by itself (it kept failing to answer) is a banner at the top of the page, under
  the title - its address, "isn't answering - its downloads wait. Trying again at …", the last error, Try now. (The
  Paused chip stays by its name in the lists.)
- **Summary** under New download: pill links, zeros left out - "N running", "N waiting", "N failed files" (red),
  "N subscriptions to look at" (yellow), the next check, "N posts to refresh"; each opens its tab and scrolls there
  ("to look at" also turns that filter on). Nothing at all: "Nothing going on".
- **Tabs** Queue · Subscriptions · History (segmented, counts; the last one remembered: `vitrine.downloadsTab`). Queue =
  the queue stacks + the tag refresh; History = failed files + finished. "Found by > Open" switches to Subscriptions.
  Motion (user: "immediate, doesn't fit"): one pill glides to the lit tab (measured; 0.32 s, the standard ease), tabs
  as wide as their name and count (equal widths overflowed "Subscriptions 128" on phones; 14 px text under 480 px);
  panels slide like Sort and filters' pages - the leaving one out in 130 ms, the new one in from its tab's side (28 px,
  240 ms after 60 ms), both in one grid cell. No lighter hover layer on the tabs (the pill is the only highlight).
  Reduced motion: at once.
- **Failed files by why**: the error's words pick a reason (not on the site any more, refused by the site, connection
  problems, damaged or unreadable, other - `reasonOf`), each a block with a hint, Retry all and Dismiss all, its posts
  a stack (the site now on each card); a card has Retry and Dismiss.
- **Subscriptions that need a look**: a failing check (error), or nothing new for 90 days on one older than that and
  not paused (`last_found_at`): a "Needs a look" toggle beside the filter (only those, groups unfolded) and a muted
  "Nothing new in 3 months" chip on quiet ones.
- **Finished**: "Today" by site, then "Earlier" folded (remembered: `vitrine.finishedEarlier`); Retry failed / Clear
  finished are small buttons.
- **Empty parts** are a quiet line (no dashed box); the Tag refresh part is left out until something came from a site,
  and is one line per site (site, to refresh, posts, state).
- The stacks stay as below (queue per site, finished per site, failed per reason).

## Downloads stacks and the bell (2026-10-21)
- **Stacks** (`components/Stack.svelte`): each site's queue, finished downloads and failed files fold into a stack - the
  first card whole (the queue: the one running, then retries, then the waiting ones), the next two peeking out behind
  it (smaller, their content faded), a "+N" on the edges that unfold it; the site's heading unfolds it too. Unfolded,
  every card glides from its place in the stack to its place in the list (absolutely placed, transforms and heights
  only, 0.42 s), back on "Show less"; cards coming and going fade and the rest glide. Reduced motion: at once. In the
  queue only the waiting downloads have handles (they reorder among themselves; the running one stays in front).
- **Retries in the queue**: Retry (or Retry all) on a failed file runs in the background (`StartRetry`) - the card
  shows in its site's queue ("Retrying", the file's bytes and speed from the live stream: `FileProgress.key`), then its
  result for 6 s (Downloaded, or back in Failed files with why); a retry started here gets a notice.
- **The bell** (`components/Bell.svelte`, top bar, right): every notice goes to it (`lib/notices.svelte.ts`, the last
  100 on this device) and so does what the downloader did - new files from a subscription, a download finished or
  failed, a blacklisted tag (the server keeps its last 100 events: `GET /api/notices`, live as `notice` on
  `/api/events`; worded in the page's language with the push texts; a press opens Downloads). A new one rings it (a
  swing) and its counter pops (mauve; red with an error unread). A press opens the menu: newest first, the unread lit,
  sliding in; a broom clears them; opening reads them. Errors and notices with an action (Undo) still pop up at the
  bottom (`Toasts`, one under another); the rest are read out for screen readers (a hidden live region). The first
  time on a device, only the last minute's server events come in (no flood).

## As built (web client, from 2026-10-14 on)
- **Downloads** (add a search: site, query, max posts, repeat = subscribe; Queue, Subscriptions and Finished cards - see `docs/downloader/downloads-and-subscriptions.md`), **Tags** (parents grouped by parent
  with add/remove, siblings), **Settings** (start muted, default search, stats, log out; kept in the browser for now).
- **Sites shown by their address**: everywhere a site appears - Downloads (picker, queue
  headings, rows, subscriptions, tag refresh), the details panel (Post link, Found by), Settings > Sites (the card's
  heading; the plugin's own name in the small print), error messages - it is its web address ("safebooru.org"), not the
  plugin ID. Manifests may set `site:`; otherwise it is post_url's host (else base's) without `www.` / `api.`.
  `lib/sites.svelte.ts` maps IDs to addresses (from `/api/sites`, loaded at login). Download rows on phones put their
  buttons on their own line under the text (as subscriptions already did).
- **New download form redesign (2026-10-15: too much on one line; a long site name overlapped the
  search box)**: phone - site, search, then "Posts" (or "First check" when repeating) and "Repeat" side by side with
  small labels, then a full-width Download/Subscribe button; wider screens - the site picker (as wide as the site's
  name) and the search on one line, the options with inline labels and the button on the next. The search's hint
  names the site; when repeating, a note under the form says what will happen ("Checked every day for new posts. The
  first check gets the newest 200 from safebooru.org."). An E2E test checks the controls share one height and never
  overlap.
- **Subscriptions list tools (2026-10-15)**: from 6 subscriptions, a filter box (matches the search or the site's
  address; "Showing 3 of 24" / "No subscriptions match ...") and an order dropdown (Next check - paused last -, Most new
  last check, Most files, Last checked, Name; remembered per device in `vitrine.subSort`). "Found by > Open" clears
  the filter so the subscription it jumps to is shown.
- **Subscription groups in the UI (2026-10-15)**: the Subscriptions card shows site (a heading when there are
  several) > its groups (a framed block: fold arrow, name, "N subscriptions · files · new last checks · paused";
  buttons Check all, interval for all, Pause/Resume all, ⋯ Rename / Delete group with an inline confirmation; folding
  is remembered per device; on phones the buttons sit under the name) > "Not in a group". Each subscription has a
  folder button: move to one of its site's groups, no group, or "New group…" (an inline name box). The New download
  form shows a **Group** dropdown when Repeat isn't Once: No group, the site's groups (picking one suggests its usual
  schedule), New group… (a name box). The filter and order apply inside groups; while filtering, only groups with
  matches show, unfolded.
- **Steady queue rows (2026-10-15, user: rows jumping as files start and finish)**: a running search shows its files
  in flight as **one line of fixed height** - a thin bar of their combined progress and "3 files · 34 of 120 MB ·
  10 MB/s" (an unknown size slides) - or "Looking for the next post…" between files, instead of a bar per big file
  appearing and disappearing. E2E checks the row height stays the same with files and without.
- **Downloads page redesign (2026-10-15)**: every section is one framed list with rows split by hairlines (no box per
  row; subscriptions in a group sit inside the group's frame, not framed again). A row: title + state pill, progress
  bar, then **stats** - each number bold with its meaning beside it, spaced apart instead of joined by dots
  ("**612** of ~1,840 posts **540** new **60** had", new in green, failed in red); subscriptions show icon items
  (clock "Every 12 hours", retry "Next in 9 h" / "Checked 30 h ago", files, new last check). The drag handle is a
  slim 20 px strip. Actions: icon buttons at the right on wider screens; on phones one **⋯** per row opens a strip of
  labeled pill buttons under it (Pause/Resume, Subscribe, Remove; Check now, the interval, the group, Unsubscribe;
  for a group Check all, Schedule, Pause all, Rename/delete). Group head: fold arrow, name, a count badge, a Paused
  pill, files / new last checks / schedule. Card descriptions shortened to one line or two.
- **Downloads buttons on phones (2026-10-16, replaced the ⋯ button)**: every row has its labeled buttons on a strip
  of their own along the bottom (a lighter band, edge to edge, hairline above), one line that scrolls sideways.
  Subscription rows and group heads put timing (every / next check) and files (files / new) on two separate lines.
  Dropdowns in the strip use Dropdown's `floating` mode: the menu is position: fixed from the trigger's place, below
  it if it fits, else on the side with more room with a max-height (it scrolls); scrolling behind it closes it.
- **New download search box autocompletes (2026-10-16)**: the same TagInput as the library search (suggestions from
  the library's tags with counts, `-` kept); with `bare` a pick goes in without its namespace (sites search by the
  plain tag). Enter submits the form.
- **Subscription and group actions behind ⋯ (2026-10-16, user request)**: each subscription row and group header
  has one ⋯ button (components/ActionMenu.svelte: a menu at the button, an action sheet on phones < 640 px) with
  Check now, Pause/Resume, Schedule (a page of intervals, current one checked), Move to group (a page, incl. New
  group…), Unsubscribe (red); groups: Check all now, Pause/Resume all, Schedule for all, Rename, Delete group. The
  queue's download rows keep their button strip (asked for earlier).
- **Queue progress (2026-10-16, user)**: a running search with no estimate (no site total, no file limit - e.g.
  Mastodon) shows a moving striped bar instead of an empty one (also a subscription's check). The per-file line
  (thin bar, file count, bytes) is gone; the download speed is a stat on the stats line, kept 4 s between files so it
  doesn't flicker.
- **Subscription groups start folded (2026-10-16, user)**: the device remembers the groups you open
  (`vitrine.expandedGroups`); a filter shows every match; jumping to a subscription opens its group.
- **Sites fold on the Downloads page (2026-10-19)**: with more than one site (or a switched-off one), each site's
  heading in Queue and in Subscriptions is a fold button with the count (chevron like a group's); folded sites are
  remembered on the device (`vitrine.foldedSites`, keys `queue:<site>` / `subs:<site>`); a subscription filter
  looks inside folded sites.
- **Site picker (2026-10-19)**: the new download's site is an action menu (`ActionMenu current`). Up to 8 sites (or
  all on one engine): a plain list. More, on several engines: the engines (`engineLabel`: "Gelbooru 0.2",
  "Shimmie2", ...), each a page of its sites, the current one's engine lit with the site beside it; an engine with
  one site lists that site (engine as its note). **Menus reopened while fading out**: Svelte brings the same element
  back, so the intro clears the `inert`/`aria-hidden` the outro set (it stayed hidden and unpressable), and the
  menu's elements are tracked with an attachment that only clears its own reference.
- **Queue rows tidied (2026-10-19)**: a queued or finished search's actions are a ⋯ menu ("More for the download
  <query>" / "More for the check of <query>": Pause/Resume or Retry, Subscribe, Remove / Skip this check) - no
  button strip on phones. The query stays on one line and scrolls sideways (soft edges, `edgeFades`); the status
  chip never wraps; the counts are one line that scrolls sideways, the speed pinned at the end. New download's
  "Manage sites" cog sits muted in the card's title row. **`--overlay1` (#7f849c) was missing from the palette**:
  ten places that meant to be muted (Off chips, "Not started", subscription row icons, Settings chevrons...) showed
  in full text color.
- **The downloads menu in the top bar (2026-10-23)** (the user: "the download button on media should be converted
  into a current downloads dropdown similar to the notifications with a button to add a new download and a list of
  current downloads with a counter on the icon"): the Library's download icon (`DownloadsMenu`, left of the bell)
  has a peach counter - downloads running or waiting (not paused) - and its arrow bobs while one downloads. A press
  opens a panel like the bell's (grows out of it; press outside / Escape / Back closes): "Downloads" + "All >" (the
  Downloads page) and "+ New" beside it (label: New download; small links in the heading - the user: "i hate the massive new download
  button"; the dialog below, with the search in the bar), then what is going on
  now - running, waiting, paused (a subscription's check named "the check of x") - each with its site icon, "Downloading
  · 12 files" / Waiting / Paused and the same progress bar as the Downloads page (`lib/downloadProgress`, shared;
  striped without an estimate); six at most, then "N more"; a row opens the Downloads page. None: a quiet check like the bell's (the user; "Nothing
  downloading" only for screen readers). Data: `lib/activeDownloads` (`GET /api/downloads`, read again when the live stream says the queue changed;
  polled every 5 s only without it). Phones: the panel spans the top bar with 8 px each side (the bell's too, since).
  The tour's Downloads hint points at it.
- **New download from the Library (2026-10-22)** (the user: "a download button on the library page that opens a new
  download popup, a multi page form"): a download icon in the Library's top bar opens `NewDownloadDialog` - pages
  like the other menus (`lib/pages`, Back in the header): (1) only when the search has tags: "This search" (its
  chips in their own words, `searchWords`) or "Something else"; (2) "Which site?" - for the search, where its tags
  came from first (`tagOrigins`: files, or "2 of 3 tags"), then the others; a new search: the site used last first;
  (3) "Search <site>": the same box as the Downloads form (suggestions, written the site's way), filled with the
  library search on that site (`lib/siteSearch`: tags via `tagQuery`, an OR group via the site's OR search - a site
  without one: **split**, a search for each combination (2026-10-22, the user: "separating the or's into separate
  queries to get all combinations"; `(a ~ b) (c ~ d) x` -> 4 searches, at most 24 - more, or an OR group with an
  excluded tag, left out), said under the box and listed on the next page, all started at once, as subscriptions
  together in a new group named after the search to start with; the
  rest - filters, wildcards - left out and listed), Next/Enter; (4) "Download once
  or subscribe?": a Once/Subscribe switch, how often + group (new group by name) for a subscription, "Files"/"First
  check" limit, the same note as the form, then Download / Subscribe (the same API calls and toasts; fav:me's files
  become favorites; the site remembered as the last one). A card in the middle on wider screens, a sheet on phones -
  both above the on-screen keyboard. Intervals and their words moved to `lib/schedule` (shared with the page).
- **Every tag's New download uses the dialog (2026-10-22)** (the user: "also use the new download form when
  interacting with tags in the details panel"): `newDownload(tags, {any, fallback})` (a tag's menu in the details or
  the viewer, the details' "Download all/any of them", a search chip's New download / an OR chip's "Download any of
  them") opens the New download dialog (shown by App: `newDownloadDialog` in lib/newDownload) at "Which site?" with
  the tags as its search (any: an OR group - the site's OR search, or split where it has none), what goes shown above the sites, the
  file's own site first among the others. The old "Download from which site?" picker (SitePicker) and the Downloads
  form's tag draft (`router.downloadDraft`) are gone. Escape closes the dialog from its search box too (an open
  suggestion list first).
- **Subscriptions as cards (2026-10-22)** (the user: "ideas to beautify the subscriptions" - "do them all"): each
  subscription a card - a health dot (green working, blue beating while it checks, gray paused, yellow nothing new
  for months, red failing; the old "Quiet" badge gone), its search as chips (an excluded term struck through in red,
  a namespace the library knows in its color with the name alone; a gallery: its title; an OR search one chip -
  its tags with a small OR between, from "( a ~ b )" or "~a ~b" - not a chip per bracket and ~), a ring filling up to the
  next check (turning while it checks), "+N" for files found since this device last looked (localStorage
  `vitrine.subsSeen`, set when the tab opens) - it opens them in the library (`system:found_by=<id>
  system:imported<Nh`; a gallery: its comic), its 4 newest posts in the library by upload time - also ones it found there already (2026-10-23: a subscription whose posts
  another download had got first showed none) (a gallery: its comic's cover, opening it), one
  line of facts ("Every day · 1,204 files · found 2 days ago"; zeros left out, "Not checked yet"). A group: a
  mosaic of its subscriptions' newest finds. Site headings with the site's icon. Wide screens (≥1100 px): the page
  1200 px wide on this tab, the cards in columns (`auto-fill, minmax(360px, 1fr)`). Phones: a card swiped to the
  left shows Check now and Pause/Resume behind it (a press elsewhere closes it). The pictures come from
  `GET /api/subscriptions/looks?since=` (asked when the tab opens and when a subscription's file count changes).
- **Cards, touched up (2026-10-22)** (the user): the search chips stay on one line and scroll sideways (soft edges;
  on phones a sideways swipe there scrolls them, not the card); the next-check ring goes round the health dot (the
  dot seen through its middle); the subscriptions' site sections start folded like their groups (opened ones
  remembered on the device: `vitrine.openSubSites`; a file's "Found by" opens its site and group).
- **Touch-ups (2026-10-22)**: the dot exactly in the ring's middle (even sizes: a 20 px ring, an 8 px dot - 22/9
  left it half a pixel off); groups folded again (what's open is kept under a new key, `vitrine.openGroups`: every
  group starts folded once more); a row's ⋮ in its top right corner.
- **Tabs that don't move (2026-10-22)** (the user: "different tabs have different widths shifting the content", "a
  count added to a tab shifts its title", "on desktop the tabs don't need to be full width"): the page is one width
  on every tab (1200 px on wide screens: the subscription cards' columns), the three tabs the same size (a 480 px bar
  on screens ≥ 640 px, full width on phones), each count beside its title taking no room (absolutely placed; over
  99: "99+"). **Then** (the user: "the chip not being centered with the text... animate the centering", "switching
  between tabs should be smooth"): the name and count centered together again; a count coming or going slides open
  (its width and gap, 220 ms), a new number glides to its width (`easeWidth`), so the name moves smoothly; a phone
  too narrow for a name and its count lets that tab take what it needs (`min-width: max-content`). Panels: the old
  one fades out in 160 ms, the new one in from its tab's side (24 px, 300 ms after 90 ms), its parts rising in one
  after another (8 px, 50 ms apart); subscription card pictures fade in as they load. Reduced motion: none of it.
- **The pill covers its count (2026-10-23)** (the user's screenshot: "99+" sticking out of the Subscriptions pill on a
  phone): the pill is measured again whenever a tab changes size (each tab watched, not only the bar) - a count arriving
  or growing widens its tab after the bar was measured. Test: "the lit pill covers its tab's count".
