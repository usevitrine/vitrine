# App shell, PWA and iOS

The web client's setup (Svelte, build, embedding), the shell (top bar, menu, navigation, back), first run and first steps, the service worker and offline thumbnails, live updates, start-up and new versions, the home-screen app on iOS.

## As built (web client, from 2026-10-14 on)
- `web/`: Svelte 5.57 + TypeScript 6 (strict, `svelte-check` clean) + Vite 8, no UI framework or router library;
  ~33 KB gzipped JS. `npm run build` writes into `internal/server/web/app/` (git-ignored), which the Go binary
  embeds; without it the server shows a "web app not built" page and the API still works. The Dockerfile has a Node
  stage; CI type-checks and builds it. Dev: `vitrine serve` + `npm run dev` (Vite on :5173 proxies `/api`, `/files`,
  `/thumbs` to `VITRINE_BACKEND`, default `http://127.0.0.1:8080`).
- **Shell**: login / first-run account setup; phone = tabs at the bottom (Library, Downloads, Tags, Settings),
  desktop (>= 900 px) = a sidebar. The Library stays mounted when switching tabs (keeps scroll and results).
  **Screens on demand (2026-10-23)**: Downloads, Browse, Tags, Duplicates, Rankings, Settings and the first steps load
  when first opened (`App.svelte` `loaders`; the main script went from 683 to 346 KB, 213 to 112 KB gzipped), and all of
  them 3 s after the start, so opening one later is instant - offline too (the service worker keeps `/assets/`). The
  Library and Comics stay in the main script.
  Catppuccin Mocha tokens, density by `pointer: coarse|fine`, safe areas, PWA manifest + icon.
- **iOS hardening (2026-10-14)**: resized copies (renditions) for images over 4096 px on touch screens, no tile
  long-press callout, `touch-action: manipulation`, `100dvh`, the details sheet above the keyboard (`--kb` from
  `visualViewport`), landscape safe areas, signed media links (see `docs/architecture/server.md`). Home-screen app: like hydrus-web, no
  `viewport-fit=cover` and no translucent status bar - iOS gives the status bar its own strip (theme color) and the
  page the full area below it. Drawing under the status bar (the first approach) left a ~50 pt gap at the bottom of
  the home-screen app and put content under iOS 27's blurred status bar. Even
  below the status bar, iOS 27 blurs a strip of the page, so in the home-screen app `--safe-top` adds `--top-extra`
  (default 0, changed from 26 pt on 2026-10-20; a "Top spacing" slider in Settings > Display, per device). Settings > Display shows window/screen/visible/app sizes and safe areas for diagnosing. E2E tests: Playwright 1.63 +
  WebKit, iPhone 15 and desktop projects (`npm run e2e`; builds a throwaway library with ffmpeg; tests log in from
  inside the page like the app).
- **Not yet**: (the browser's ~33M px height limit for huge results on phones),

  drag a chip onto another to OR them,
  live updates (SSE), service worker / offline thumbnail cache, pause/delete
  downloads, subscriptions UI.
- **Top bar (2026-10-14)**: replaces the floating menu button. Menu button (opens the navigation drawer),
  the Vitrine logo (taps to the library) and the shown screen's controls on the right - the library's sort menu, put
  there through `lib/topbar.svelte.ts` (a snippet the library registers). It starts below the status bar (`--safe-top`,
  incl. the iOS 27 blur allowance); screens below it no longer pad for the safe area or the menu button.
- **Live updates (built 2026-10-15)**: `GET /api/events` is a Server-Sent Events stream (`internal/server/events.go`):
  `downloads` when the queue or subscriptions change (the downloader signals on queueing, a search starting, every
  handled post, finishing; queue/subscription POSTs signal too, so other devices see changes) - sent at most twice
  a second; `progress` with the files downloading now (`[{search, post, read, total}]`, bytes counted as they're
  read, twice a second while any are, then one empty list); a comment every 20 s. Server shutdown ends the streams
  (`BaseContext`). Client `lib/live.svelte.ts` (opened after login, reopened when the app comes back on screen):
  Downloads re-reads the queue on `downloads` (polling only as a fallback: 2.5 s without the stream, 20 s with it);
  running searches show their files of 1 MB and more as aligned thin bars ("34 MB of 46 MB"; an unknown size slides)
  and the speed on the counts line ("· 10 MB/s", smoothed); the library checks "N new files" 1.5 s after a change.
- **Offline thumbnails (built 2026-10-15)**: the service worker (`public/sw.js`) is now registered on every device
  (not under browser automation - `navigator.webdriver` - unless `localStorage['vitrine.sw'] = 'on'`, because
  Playwright's Windows WebKit freezes on registration) and asks for persistent storage. It keeps **thumbnails and
  scrub frames** cache first (`vitrine-thumbs-v1`, trimmed to the newest ~15,000; their URLs never change), the
  **app** (the page network first with the saved copy as fallback, `/assets/` and icons cache first;
  `vitrine-shell-v1`) and the **last answers of library API reads** (`/api/session`, search, results, files, tags,
  settings, sites, namespaces, saved; network first, `vitrine-api-v1`, ~400 kept) - used only when the server
  can't be reached and marked `X-Vitrine-Offline: 1`, which the app shows as "Offline · showing saved results" on the
  library's status line. Originals are never stored. Logging out deletes what's saved. Settings > Display > "Saved
  on this device" shows the thumbnail count and space used, with Clear. E2E (Chromium, against the built app on the
  backend's port): go offline, reload, the grid and thumbnails come back with the note.
- **First run (2026-10-15)**: the empty library (no search) shows a welcome (`components/Welcome.svelte`): "Download from
  a site" (Start a download: opens Downloads with the search box focused, via `router.focus = 'new-download'`), "Add
  more sites" (lists the installed sites; Open Sites scrolls Settings to the Sites card, `data-card="sites"`), "Bring
  in files you have" (`vitrine import /path/to/folder` with Copy command); on iPhone Safari (not the home-screen app)
  an Add to Home Screen tip; while a download waits or runs, a green "Your first download is running" note with See
  progress (re-checked on live updates). Account screens: a show/hide button on the password, the setup text says the
  account is for all devices, the login screen says how to reset a forgotten password (`vitrine passwd`), and the
  error line no longer leaves empty space.
- **Faster start (2026-10-16)**: (1) a splash (the icon on the theme color, style attributes - the CSP forbids inline
  style blocks; pointer-events: none) is in index.html itself, so nothing is black while the code loads; main.ts
  fades it out once the app draws its shell/login. (2) The service worker serves the saved page at once and fetches
  the server's copy for next time; if that is a newer version it tells the app, which reloads if it started < 4 s
  ago. (3) The app opens on the last known session (localStorage `vitrine.session`) and, after the first sync on this
  device, the saved settings - the shell shows at once while both are checked in the background (logged out: the
  login screen). The library's first search waits for the settings (`settings.whenSynced()`, max 1.5 s), so it never
  runs with stale ones. A server value never overwrites a local change not yet sent (`pending`). (4) The server
  gzips text answers (the app's code 280 -> ~90 KB, JSON); never files, thumbnails, previews, the event stream or
  backups.
- **New versions on the home-screen app (2026-10-16)**: iOS usually resumes the app instead of starting it, so the
  start-up check (service worker: saved page first, "updated" message, reload within 4 s) may never run. main.ts
  also checks when the app comes back on screen after >= 1 minute: it fetches `/` (no-store), compares the script
  name with its own and, when newer, puts that page into the service worker's cache and reloads. An "updated"
  message later than 4 s after start shows a "New version ready - Reload" notice instead.
- **Version check at every start (2026-10-16)**: the service worker's "updated" message can be lost (sent while
  the new page isn't a client yet), so a plain reload could stay on the old version, and a hard reload (which skips
  the service worker) left the old saved page in place. main.ts's `checkVersion()` now runs at every start and on
  coming back after >= 1 minute: fetch `/` (no-store), always store it as the saved page, reload if its script
  differs. Verified with two real builds (A open, server swapped to B, Ctrl+R -> B, again -> B).
- **Batch of fixes (2026-10-18, user list)**: the viewer's image has a transparent pixel as its src while the
  picture decodes (no broken-image border/box); the header's "N new" chip has a broom (clears, doesn't delete); top
  spacing is only offered on touch screens; New download has a Manage sites button (to Settings > Sites); the menu's
  sub-items sit on an indented rail (current one lit); a subscription group's whole header folds it; no "Check" chip on
  queue rows; library search placeholder "Tag search"; "Where it's from" lists only what downloaded the file, rows
  don't react - their open/copy/open-subscription buttons do; a tap beside the details closes them at once; a details
  sheet swiped away leaves from where it was let go. Tests: the test video's sound track is silent and Chromium runs
  with --mute-audio (nothing from the tests is heard).
- **More fixes (2026-10-18)**: a tapped "new files" notification with the app open goes to the library, clears the
  search, runs it fresh (new files in) and jumps to the top; GIF tiles show a "GIF" badge (not a video-like length)
  and their details no "silent"; a tile whose thumbnail is still being made shows a small spinner (re-checked every
  3 s); a press outside the filter menu, saved searches or a dropdown only closes it - `lib/outside.ts`
  `swallowPress` stops the press and its click reaching what's underneath; every open menu sub-item has the
  highlight background (the rule hiding it was meant for main items only).
- **Thumbnails through the service worker (2026-10-19)**: fetched first (the browser's HTTP cache answers seen ones
  at once - they're immutable), the saved copy only when the fetch fails (offline); saving and trimming run in the
  background. Cache-first made every thumbnail wait on the Cache API, which does one operation at a time per cache
  (and a trim deleted hundreds one by one): a screenful queued up and arrived together.
- **A slimmer top bar on phones (2026-10-23, user: "with the two buttons on the right, the files counter ... and the
  new files button ... it gets cramped")**: under 640 px the logo is just the mark (the V, no "itrine"), the library's
  file count is shorter from 10,000 on ("159K files", `short()` in lib/i18n: compact notation, 3 significant digits -
  the full number in its title and for screen readers); under 380 px the bar's gaps and sides are tighter. The
  new-files chip left the top bar (the user: "move the clear new button to the left of the search bar and have it
  animate in and out" ... "just the icon" ... "make sure the button is square"): a square accent button left of the
  library's search bar, the bar's one-line height (44 / 38 px, like Empty trash in the trash), no text - how many
  only in its label for screen readers; it and Empty trash slide in and out sideways with their gap. Measured with a
  159,212 file count and 3 downloads: the bar fits at 393, 360 and 320 px. Desktop: just the button moved. Its icon is an eye (marks them seen; a broom until 2026-10-24, the user: "should be an eye instead of a broom").
- **The top bar slid away on scroll (2026-10-19 to 2026-10-25 - removed: the user, "disable the header bar autohide site
  wide. its annoying"; it always stays now)**: scrolling the library grid or a screen's page down (past two
  bar heights) slides the top bar up out of sight - all but the strip under the status bar; scrolling up brings it
  back, and it's always there at the top, on another screen and with the menu open. Since 2026-10-20 the bar floats
  over the screens (`position: absolute`; `--bar-h`, `--bar-off` on the shell, `topbar.h`/`off` for the library)
  and slides by its `top` (a transform would make it the containing block of menus fixed to the screen), so nothing
  under it moves (it used to give the page its room, and the content jumped up as the user scrolled): pages and
  Duplicates keep `--bar-h` of padding at the top; the library's search bar floats too, under the bar, sliding with it,
  and the grid scrolls under both (`pad`: room at the top of the list; `inset`: how much the bars cover now - the
  hover card, `reveal`, `tileRect` and drag-select's auto-scroll stay below them; the fast scroller's track starts
  below them as shown and stays there - following their slide, the handle jumped up). The
  slide animates only while it slides (`.bar-sliding`), not when the heights are first measured. The grid moving itself (following the viewer, keeping arrivals still: `scrollSelf`, `data-quiet-until`)
  doesn't count, nor does the browser moving the scroll right after the bar moved (350 ms).
- **Launch screen on iPhones (2026-10-19)**: a home-screen app showed an empty screen from the tap until the page
  first drew (the splash in index.html only shows once the page loads). index.html now lists an
  `apple-touch-startup-image` per iPhone screen size (media queries; iOS uses one only at the exact size), and the
  server draws them (`GET /startup.png?w=&h=&top=&scale=`, open to everyone, internal/server/startup.go): the splash
  itself - the icon on the theme color, centered below the status bar - so the page takes over without a jump.
  iOS may read them only when the app is added to the home screen.
- **First steps (2026-10-20)**: after the first account is made (Login's setup), `FirstSteps.svelte` replaces the app
  until finished: Storage (the files and data folders with free space - little room: under 10 GB or 5 % - ffmpeg and
  libvips found or what to do; daily backups are on), Sites (`SitesCard welcome`: the plugin list open, no
  Cloudflare), Install the app (`InstallHelp`), Notifications (`NotificationsCard`; on iPhone after the Home Screen),
  Start your library (site + search, Download once / Subscribe daily; skippable), You're all set (daily backups,
  download rules, the blacklist - with links). Back / Continue / Skip and "Skip setup". Where it is is the shared
  setting `welcome` ('' never started - setups from before it never see it; a step; 'done' the last page;
  'finished'), so it carries on where it was left, on any device; Settings > Account > First steps shows it again.
  The language step waits for the language files (item 70). An empty `welcome` must stay a valid setting value:
  one refused value refuses the whole settings save (it broke saving every setting before it was allowed).
- **First steps, v1 (2026-10-23)** (the user, testing v1 from scratch: "the setup wizard isn't animated at all", "when
  you download/subscribe in the form you have no control over file limits", then chose more steps and a tour). Nine
  steps now: What will you use? (+ **Safe for work**), Storage, Sites, **How it looks** (square tiles or rows - this
  device's `gridLayout`, each with a little picture of it; videos start muted; play on hover with a mouse), Install the
  app, Notifications, **Before downloading** (`BlacklistPanel suggest` - common tags a tap away - and the size limits,
  `DownloadRules`, shared with Settings > Download rules), Start your library, You're all set. **Moving**: each step
  slides in from the side it's reached from (`{#key step}` in one grid cell; the old one fades in 120 ms, the new one
  waits for it - never text over text), the progress bar's segments fade to their colors; none of it with reduced
  motion. **The first download is the New download dialog itself** (`newDownloadDialog` with an `ondone`): the site,
  the search with tag suggestions, once - all or the newest so many - or a subscription with its interval and group;
  done, the steps go on; closed, the step stays. **The tour** (`lib/tour`, `Tour.svelte`): finishing (or skipping) the
  steps starts it on this device; on the library or Comics (nothing open over it) one hint at a time beside its thing,
  lit up by a frame whose shadow dims the rest (the page stays usable): search, New download, saved searches, viewing
  (touch: swipes; mouse: keys and hover), the menu - those not on screen left out (an empty library has no file to
  point at). Skip tour / Esc / Done end it for good on that device (`vitrine.tour` in localStorage: per device, as
  gestures differ). The rows' preview uses plain columns: CSS multi-column made WebKit (and so Playwright) count the
  card as never settling.
- **A newer Vitrine during the first steps (2026-10-28)** (the user, setting up a wiped instance: "if theres an update
  during initial setup the user should be able to update there and it resets the initial setup and they start again"):
  a banner above the steps (`/api/update`; asked once if never checked yet) - Update (with Watchtower or the Windows
  installer) sets `welcome` back to the first step and runs the full-screen updater, so the new version starts setup
  over; without it, how to update by hand. Disk sizes there (`bytes`) go up to TB (from 1000 GB on). Test: "first
  steps: a newer Vitrine is offered" (e2e).
- **Installing as an app, every platform (2026-10-20)**: `InstallHelp` (first steps, Settings > This device > App):
  installed - says so; Chromium's own install offer (`beforeinstallprompt`, kept from start-up by lib/install) - an
  Install Vitrine button; else steps for the platform and browser: iPhone/iPad (Safari's Share; Chrome's Share in
  the address bar; Firefox/Edge menu > Share; then Add to Home Screen), Android (Chrome/Brave/Edge menu > Install app;
  Samsung Internet menu > Add page to > Home screen; Firefox menu > Install; Opera menu > Add to > Home screen),
  desktop (Chrome/Brave install icon or menu; Edge Apps > Install this site as an app; Safari File > Add to Dock;
  Firefox can't). Android also got 192/512 px PNG icons, a maskable icon (the mark inside the safe zone,
  public/icon-maskable.svg) and a one-color notification badge (public/badge.svg -> badge-96.png); the manifest has
  id, scope and description. PNGs made with `vips copy "x.svg[scale=N]" x.png` (to remake with the new logo, item 54).
- **Back closes what's on top (2026-10-20)**: on touch screens (Android's back button and gesture, iOS's edge swipe),
  the menu, sheets, dialogs, menus and the viewer's details each add a history entry while open (`backClose`
  attachment, lib/backClose.ts): Back closes them and stays on the page; closed another way, the entry is popped as
  soon as it starts fading out (an outroing element turns inert) - unless the page moved on meanwhile, or its action
  changed the address (a new search: then the entry stays, else Back would undo it).
- **The menu folds (2026-10-20)**: items with sub-pages (Library, Tags, Settings) are folded to begin with; an arrow
  at the right of the row unfolds them (sliding), the row itself still opens the screen; what's unfolded is
  remembered on the device. "Logged in as" is gone from the menu (Settings > Account says it).
- **Splash to app without a gray flash (2026-10-20)**: the start-up splash stays until the library's thumbnails on
  screen are in (gray empty tiles flashed between the splash and the pictures), at most 1.5 s after the app drew,
  then fades out over 0.25 s (main.ts).
  Its icon is inline SVG (an <img> could miss the first frame: the icon blinked after the launch image), and
  public/splash.js (a plain script before the first paint; the CSP allows no inline script) moves it to exactly
  where the launch image drew it: that image centers it below a status bar of its `top` height, the page starts below
  the real one (screen.height - innerHeight), so it shifts by half the difference.
- **No reload loop (2026-10-20)**: renaming the service worker's page cache (v2) left main.ts's version check writing
  the fresh page to the old name, so the worker kept serving the stale page and every start reloaded again (Ctrl+R
  looped; a hard reload skips the worker). It now writes to whichever vitrine-shell-* cache exists, and reloads into a
  new version at most once in 30 s (`reloadOnce`, sessionStorage). The menu's sub-page line ends at the last sub-page.
- **The menu scrolls (2026-10-20)**: taller than the window (a short window, a landscape phone), the menu scrolls
  (a thin scrollbar) instead of cutting off its last items.
- **Live updates survive restarts (2026-10-20)**: the browser gives up on the live stream (`/api/events`) after an
  error answer - the proxy's 502 while the server restarts (every deploy) - and the app used to reopen it only when it
  came back on screen, so the files' progress bars and speed stayed frozen and a download that had resumed within
  seconds looked stalled. `lib/live.svelte.ts` now opens it again itself (1, 2, 4 ... s, at most 15 s apart), clears the
  progress once it's given up or has been down 3 s (a quick reconnect keeps it) and makes pages catch up the moment
  it's back.
- **No welcome flash on start (2026-10-19)**: a device that has synced before starts on its saved settings and its
  first search waits up to 1.5 s for the server's copy; meanwhile the library had no results, so the empty-library
  welcome (with its download form - it looked like the Downloads screen) and "0 files" flashed. Both now wait for
  the first finished search (`results.searches`); until then the grid is empty and the count says "Searching…".
- **Quiet keyboard (2026-10-21)** (`lib/privacy.ts`): every text field gets `autocomplete`/`autocorrect`/
  `autocapitalize` off and `spellcheck` false as it appears (a MutationObserver; a field that says otherwise keeps it -
  the login's), so the phone's keyboard doesn't suggest or (iOS) largely learn what's typed. A web page can't turn on a
  keyboard's own incognito mode. (Blurring the app in the app switcher was tried and dropped the same day: iOS takes
  the switcher's picture before a web app hears it's being left; Android's way to keep an app out of Recents is a flag
  web apps can't set.)
- **Anonymous sign-in page (2026-10-23)** (the user: "no info anywhere on the login page that indicates what the
  login page is for so if someone finds it on someone's network, they can't look up what the software is"): Settings >
  Account > "Anonymous sign-in page" (shared setting `anonymousLogin`, off by default). Signed out, the server
  (`internal/server/signin.go`) answers every page path with a plain form from `internal/server/signin/` - title "Sign
  in", Name, Password, a neutral gray look (light/dark by the device), no name, logo, favicon, theme color, manifest
  or app script; `no-store`, `noindex`. Every other file of the app (scripts, icons, manifest, service worker,
  launch images) answers 404 until signed in; the API and media check the login as always. `/api/session` says
  `anonymous` (nothing more), so a device that has the app saved (service worker) goes to `/signin` instead of the
  app's own login screen - also after Log out or when a session ends. The service worker leaves `/signin` alone and
  never keeps a `no-store` page as the app. Tests: `TestAnonymousSignIn` (Go).
- **"Media", not "Library" (2026-10-23)** (the user: "rename the library section as comics is technically also part of
  the library"; picked "Media"): the menu's first section (Media: Everything, Favorites, Trash), its Settings section and
  notice group, the first steps' "Open Media", the Comics empty page. "The library" in other texts still means all of
  it (files and comics); code and URLs keep `library` (`/`, `/favorites`, `/trash`).
- **Android home-screen app (2026-10-24)** (the user, testing in Brave on Android): (1) the round launcher icon's mark
  was too big - a launcher shows only the middle two thirds of the maskable icon, where the mark (scale 0.72) filled
  70%; now 0.56 (public/icon-maskable.svg -> icon-maskable-512.png with `vips copy "icon-maskable.svg[scale=8]"`; the
  manifest's `v` raised). An installed app keeps its old icon until it's installed again. (2) The system's splash and
  the app's differed in size and place, so on Android the app has **no splash of its own** (public/splash.js hides
  it before the first paint; the user: "why do we even need a splashscreen?"). Chromium (Chrome, Brave, Edge) shows
  its own splash for an installed web app until the page first draws something (a background color doesn't count), so
  it simply stays until the app appears. Lining ours up was tried and dropped the same day: Chromium's covers the whole
  screen, bars included (the maskable icon drawn flat at 128dp, 16dp above the middle, the name 48dp from the bottom -
  its `webapp_splash_screen_large` layout), while the page gets only the part between the bars, and no API says how
  tall each bar is (measured on the user's phone: screen 915, page 838 or 863 depending on the launch; avail, outer,
  screenY, env() and lvh/svh no help; keeping a touch's screenY - clientY as the status bar didn't fix it in practice either). The iPhone keeps its splash
  (there it lines up exactly with the launch image). (3) The comic reader's swipes started and snapped back: touch-action stops
  at the nearest box that clips, and the slides and pages clip, so the stage's `touch-action: none` didn't reach the
  page - the browser took the swipe as a scroll and cancelled it (pointercancel). Now on everything in the stage
  (the viewer was fine). Found with real touches (CDP `Input.dispatchTouchEvent` on a Pixel 7 profile); synthetic
  pointer events never show it. (4) A 1px line between the top bar and the search bar while scrolling (fractional
  pixels: the two edges rounded apart): the library's, comics' and browse headers reach 2px up under the top bar
  (a box-shadow in their background color).
- **Trash counts in the menu (2026-10-24, the user)**: Trash shows the deleted files and comics together (a grey
  count, left of its fold arrow - not the accent of Duplicates', which waits for you), and Media and Comics under it
  each their own, lined up with it. Read from `lib/trash` each time the menu opens (deletes elsewhere don't update
  it); a part that's off (Media or Comics) isn't counted.
- **Taps the browser dropped (2026-10-25, the user: after swiping a picture away, the top bar's buttons took two taps -
  "check the entire site"; then: the first fix waited for a click that might not come - "a significant delay", "hacky")**:
  where the app handles touches itself (`touch-action: none`: the viewer's and the reader's swipes, sheets dragged
  down, sliders, the crop box, the fast scroller) the browser scrolls nothing but still watched the touch - a quick
  swipe it took for a fling, and the next tap, anywhere and however much later, only "stopped" it: no click (Chrome and
  Safari; measured with real touches - a slow drag was fine, a quick swipe lost the next tap's click even 2.5 s later,
  and nothing scrolled). The cause, fixed: `lib/ownTouches` (main.ts) marks a touch's moves handled (preventDefault) when
  it starts on such a surface (touch-action none on it or an ancestor before anything scrollable; not in text fields or
  native controls). The browser runs no gesture there; pointer events - what the app listens to - are as before.
  (Before it: a click made for the dropped tap - one site-wide, one on the viewer's bars - removed.)
