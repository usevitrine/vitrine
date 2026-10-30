# Notifications

Push notifications (Web Push), the toasts and their kinds, the bell and its notices.

## As built (web client, from 2026-10-14 on)
- **Push notifications (built 2026-10-15)**: Settings > Notifications. "Turn on notifications" (a tap: iOS requires
  one) asks for permission, registers `public/sw.js` and subscribes with the server's VAPID key; the device appears
  as e.g. "Home-screen app on iPhone" / "Brave on Windows" (other devices listed with a remove button). On iPhone
  Safari (not the home-screen app) the card explains Add to Home Screen; blocked permission explains where to allow
  it; Brave's "push service error" points to "Use Google services for push messaging". "Send a test" sends one to
  this device. Shared settings `notifyNew` / `notifyDone` / `notifyFailed` (missing = on). The service worker caches
  nothing; it shows the notification and, when tapped, focuses the open app and routes to its URL
  (`router.openPath` via postMessage) or opens a window. It's only registered on devices whose permission is granted
  (Playwright's Windows WebKit freezes when a service worker registers; other devices don't need one).
  Server side: package `internal/push` - RFC 8291 encryption (checked against the RFC's test vector), VAPID ES256
  JWT (sub = the project URL, exp 12 h; checked live: Apple and Google accept it), TTL 1 day; the private key is made
  on first start and kept in `meta.db` (`meta.vapid_key`); devices in `push_subscriptions` (migration 0012), removed
  when the push service answers 404/410. Events from the downloader (`Downloader.OnEvent`): a subscription check
  with new files; a subscription check failing (only the first failure in a row - it retries hourly); a search
  finishing; a search giving up its automatic retries. A `push.Notifier` gathers events for 20 s and sends one
  notification per kind ("36 new files" - "b (12) · d (8) · ..."); new files open the library, the rest Downloads.
- **Notices by kind (2026-10-19)**: every toast has a kind (`lib/toast.svelte.ts`), shown as a round icon badge
  in its color - done (green check, the default), info (blue i: something started, or nothing happened), removed
  (peach bin: deleted or taken off a list, usually with Undo) and error (red alert, red-tinted border). The text
  stays the normal color. The opening zoom (lib/hero) runs inside `#app` just under the viewer, so the viewer's
  controls fade in above it; sidebar items change hover at once (a crossfade between neighbors flickered).
- **Notifications: hourly new files, finished downloads (2026-10-19)**: subscriptions' regular checks no longer
  notify one by one - their new files are summed up at the next full hour ("38 new files", "across 5 subscriptions:
  a (12) · b (9) ..."; one subscription too: "across 1 subscription: blue_sky" (2026-10-23, the user: "1 new file - across N subscription(s)"); a subscription checked twice in the hour
  counts once). A new subscription's first check (its initial download) and one-off downloads notify within
  seconds when they finish ("Subscription ready" / "Download finished", with how many new files; several at once:
  "3 subscriptions ready"). Failures and blacklisted tags as before. Stopping Vitrine sends what's waiting
  (`Notifier.Flush`). The downloader marks a first check's event `done` with `First`.
- **A subscription check that found nothing is never told (2026-10-30, the user: "make sure subs that find 0 files
  dont notify the user")**: regular checks with no new files never were; now a first check that found no new files
  (all already in the library, or nothing matched) isn't either - no push, no bell notice (`finishCheck`, `TestEvents`).
  One-off downloads still say when they finish, 0 new files included.

## Notices kept over restarts (2026-10-21)
- The bell's notices (the downloader's events) are kept in meta.db (`notices`, migration 0032; the last 100,
  `library.AddNotice` / `Notices`), loaded when the server starts and numbered on from the last. Their boot (the
  name a page tells notices it has seen by, with the ID) is made once and kept (`meta` `notices_boot`), so a page
  doesn't take old notices for new ones after a restart.
- Clear (the broom) in the bell's menu also closes it (2026-10-21).
- **A cleared notice stays gone (2026-10-21)**: a page catches up on the server's notices at every start and each
  time its live stream opens again (on an iPhone: each return to the app), from the newest one it has seen *on* - so
  that newest one, cleared from the bell, came back each time (worse once the server kept notices over restarts).
  Each device now remembers the keys of the server notices it has had (`vitrine.noticesSeen`, the last 300) and
  never shows one again.

## What goes where; one bell for every device (2026-10-21)
- **Kinds of notice** (`web/src/lib/noticeKinds.ts`): 28 - the downloader's events (new files, done, failed,
  blacklisted, paused, back) and what's done in the app (deleted, tags, favorites, "I came", copied, refreshed, tag
  edits, comics read, comic edits, collections, downloads started/controlled, groups, retrying, a retry's result,
  other downloads, backups, sites, saved searches, this device, a new version, errors). Every `toasts.show` names
  its kind (`cat`). **Settings > Notifications > What goes where** (shared setting `noticeModes`: only the kinds
  changed from their default): **Bell** (it rings), **On screen** (a moment, then gone) or **Off** (screen readers
  still hear it). Defaults: the downloader's six, the ones with Undo (deleted, tags, favorites, "I came", comic
  edits, saved searches) and a new version to the bell; copied, refreshed, tag edits, collections, downloads
  started, a retry's result, backups, sites and this device on screen; the rest off. A notice with a button (Undo,
  Reload) shows on screen whatever its setting, and so does an error (errors can't be off). Push notifications are
  separate (set per device, as before).
- **One bell for every device**: the server keeps it - the downloader's events and the app's notices set to the bell
  (`POST /api/notices {text, tone, category}`; meta.db `notices` gains text, tone and category, migration 0033).
  Read and cleared are the same everywhere (`POST /api/notices/read|clear {up_to}`, meta `notices_read`,
  `notices_cleared`); `GET /api/notices` leaves out the cleared and says how far they're read; open pages hear changes
  (event `notices-state`) and new ones (event `notice`). A kind not set to the bell isn't listed (its notices are
  kept: a setting can change back); a downloader event set to on screen shows there as it comes. Each device's own
  copy of the bell (localStorage) is gone.
- **Pop-ups reworked (2026-10-24)** (the user: "some appear high up the screen, they animate in and out buggily,
  notifications of the same type dont stack"): `components/Toasts.svelte` places each pop-up by the ones below it
  (measured heights, `translate`, gliding), not in a column - one going leaves its place at once (it fades where it
  was) and the others glide down; a new one rises in at the bottom. They sit 16 px above the bottom of the screen
  (clear of the iPhone's home bar, `--bottom-room`), above the selection bar only while it shows (it says its height
  as `--bottom-bar`) - before, always 72 px up, and higher still while one faded. Notices of the same kind (their kind of notice and tone;
  unnamed: their words) form a stack like the Downloads page's (Stack, building upwards, `hover`, unframed - the
  user: "the same stack style as on the downloads page"): the newest in front, two peeking out behind with "+N";
  pointing at it or tapping its edges fans it out, each card with its own Undo. The stacks sit one above another, the
  newest at the bottom (placed by measure as above). At most three stacks (a fourth sends the oldest away), five
  notices in one. Downloader pop-ups pass their kind
  too (`notices.onScreen`).
- **The bell stacks too (2026-10-24)** (the user: "they should be stacked in the notification bell too"): notices of
  the same kind (kind of notice and tone) are one Stack at the place of the newest - the newest in front, two peeking
  out with "+N"; tapping its edges fans it out, "Show less" folds it; one of a kind stays a plain row. Opened again,
  the bell (and the Downloads menu) shows its stacks folded (the user: they should collapse when it closes). The kind's
  color and the unread tint are on each row (`.row`).
- **Wording, 2026-10-24** (the user: in-app notices "a little more verbose ... if i delete 2 or more files, just say 2
  ... it should say the file/comic id"; push: nothing "that could be explicit such as site names and tag names"):
  - In-app (pop-ups, the bell): a notice about one item names it by its ID - `itemName(id, comic)` in lib/i18n:
    "file #1234" / "comic #56", "page 12 of comic #56" for the comic reader's "I came" - e.g. "Deleted file #1234",
    "Restored comic #56", "Added file #1234 to favorites", "You Came to file #1234 for the first time" (Came capitalized, the feature's name - the user) / "... 3 times"
    (the user's words), "Saved the tags of file #1234 (added 1)", "Removed landscape from file #1234", "Refreshed
    the tags of file #1234 from its site", "Renamed comic #56", "Added comic #56 to "Sunsets"". Two or more: the
    count only ("Deleted 2 files"). The blacklist clean-up's Undo says "Restored N files". TagsSection knows whose
    tags (`comic` prop). Site and tag names stay in-app.
  - Push (internal/push/notify.go `Compose`): what happened and how many - "12 new files" / "from 3 subscriptions",
    "A download stopped" / "A subscription check failed" / "3 downloads failed" with "Open Vitrine to see why", "A
    site isn't answering" / "A site answers again", "2 files got blacklisted tags" / "They were in the library
    already - see Tag rules", "2 downloads finished" / "52 new files", "Subscription ready" / "340 new files". No
    site, tag, search or error text (a push goes through the browser maker's push service and shows on a locked
    screen); `TestCompose` checks none slips in. Tapping one opens the page with the names.
- **The downloads menu stacks too (2026-10-24)** (the user): a site's downloads are one Stack (running first, in front);
  each card: its search as the header (scrolling to its end and back when it's long - `marquee`; its soft edges only on the side where text is out of view, animated with it - `marquee-edges` in app.css, also a comic title's on hover; 2026-10-26, the user: fixed at both they covered the first and last letters), a chip "Sub" (a
  subscription's check) or "Initial" (a first download: a new subscription's first check, or a download once), the
  posts gone through out of the goal ("12/40", `goalOf` as on the Downloads page; none known: the count and a striped
  bar) and " · Waiting" / " · Paused". One on its own (here and in the bell) is a card like a stack's (`.solo`).
- **A gallery counts by its pages (2026-10-25)** (the user: "the downloader and the notifications still only show 1 file
  when downloading a multi page gallery"): a gallery site's events carry `Pages` (the search's pages; `New` counts its
  galleries) - kept with the bell's notices (`notices.pages`, migration 0041). The bell: "40 new pages from ..." /
  "Download finished: ... (40 new pages)"; push: "40 new pages" (files and pages together: "5 new files, 75 new
  pages" - `push.newWhat`). The top bar's Downloads menu: one gallery by its pages ("12/40 pages" while it downloads,
  "40 new pages" after; nothing before it starts - was "0/1"), a search of many galleries by gallery with the page
  it's on ("2/5 · page 12 of 40"; `lib/downloadProgress`'s `oneGallery`, shared with the Downloads page). Tests:
  `TestComposeGalleryPages`, "the downloads menu counts a gallery by its pages".
- **Blacklisted tags lead to Delete them (2026-10-25)** (the user: "take the user to the tags page where the button is to delete files with blacklisted tags"): a bell notice of kind blacklisted leads to Tags > Rules (it led to Downloads), and the push one's address is `/tags/rules?focus=blacklist-delete`: the router takes `?focus=` (a tapped notification, or the app starting from one) into `router.focus` and tidies the address; BlacklistPanel brings Delete them into view, focuses it and lights it (a red glow pulsing twice; none with reduced motion) - also when the page was open already. Test: "the bell: files that got a blacklisted tag lead to Tags > Rules".
