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
  a (12) · b (9) ..."; one subscription: "from blue_sky on safebooru.org"; a subscription checked twice in the hour
  counts once). A new subscription's first check (its initial download) and one-off downloads notify within
  seconds when they finish ("Subscription ready" / "Download finished", with how many new files; several at once:
  "3 subscriptions ready"). Failures and blacklisted tags as before. Stopping Vitrine sends what's waiting
  (`Notifier.Flush`). The downloader marks a first check's event `done` with `First`.

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
