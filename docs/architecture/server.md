# The server

Implementation decisions of the Go server, the HTTP server and its API conventions, signed file links, screen-sized copies, the Docker image.

## Implementation decisions (build step 1, 2026-10-14)
Made while building items 1-3 of step 1 (repo skeleton, file store, metadata DB); review and change freely.
- **Config**: TOML file (`github.com/BurntSushi/toml`; unknown keys are an error, so typos show up) + `VITRINE_*`
  environment variables + command-line flags, in that order of priority. File location: `VITRINE_CONFIG` / `-config`,
  else a `config.toml` next to the program (portable, 2026-10-25 - the user: "set up the config so all the data is
  portable"), else the OS config dir (`~/.config/vitrine/config.toml` etc.). A config file's relative paths
  (data/files/backup dirs, socket, a local plugin index, tools given as a path) are from its folder, not the working
  directory; tools not set are taken from beside it if there (ffmpeg, ffprobe, vipsthumbnail); environment and flags
  as given. Portable and started without a command (a double-click) it serves. Settings so far: `data_dir` (default: the
  OS's app-data dir), `files_dir` (default `<data_dir>/files`), `listen` (default `:8080`). Example:
  `config.example.toml`.
- **Layout**: `cmd/vitrine` (one binary with subcommands: `import`, `config`, `version`; `serve` comes with the HTTP
  server), `internal/config`, `internal/media` (file types), `internal/store` (originals), `internal/db` (meta.db +
  migrations), `internal/library` (import = store + DB). The spike stays a separate module in `spike/`.
- **File types** are a small integer (`media.Type`) stored in the DB and later the index; codes are fixed forever:
  1 JPEG, 2 PNG, 3 GIF, 4 WebP, 5 AVIF, 6 MP4, 7 QuickTime, 8 WebM, 9 Matroska. Detected from the first 64 bytes only,
  never from the file name. HEIC is refused (browsers can't show it). Animated vs static, dimensions, duration and
  audio come later from libvips/ffprobe at import (one consistent probe step for all types).
- **Store**: temp files in `<files_dir>/tmp` (same filesystem, so the rename is atomic), fsync, rename, then fsync the
  directory (skipped on Windows, which can't sync directories). An existing file with the same hash is never touched.
  Leftover temp files are deleted at start-up. Measured: ~450 MB/s per write including fsync.
- **meta.db**: schema in `internal/db/migrations/NNNN_name.sql`, applied in order, version in `PRAGMA user_version`;
  a database from a newer Vitrine is refused. All tables `STRICT`; times are Unix milliseconds (UTC). Two connection
  pools: one writer connection (`BEGIN IMMEDIATE`) and up to 8 read-only connections. `files.id` uses
  `AUTOINCREMENT` so IDs are never reused even if a row is deleted. Tag/URL origin is a `sites` row; site 0 =
  `user`. Namespace 0 = no namespace. URL kinds: 1 post, 2 source, 3 file. A `meta` table holds a `generation`
  counter (bumped on every library change; later checked against the index snapshot). Downloader tables come in a
  later migration with the downloader.
- **Import**: store first, then one transaction that checks tombstones and inserts the row (`ON CONFLICT DO NOTHING`,
  so the same file twice keeps one ID). A tombstoned file is refused and a fresh copy removed from the store - also
  for manual imports (an override can come later if wanted). Measured: ~2.5 ms per small file (mostly fsync).
- **Not done yet**: the start-up sweep for originals without a DB row (crash between store and DB) - needs care
  with ~90k files on an HDD; probably compare against a list kept by the importer rather than walking every folder.
- **CI** (GitHub Actions): tests + vet + a short benchmark run on Linux (amd64, arm64), Windows and macOS; gofmt,
  `go mod tidy` and race-detector checks; pure-Go builds for all six OS/arch targets.

## HTTP server as built (step 1 item 6, 2026-10-14)
- `vitrine serve` (`internal/server`, net/http): JSON API, originals at `/files/<sha256>.<ext>` (range requests via
  `http.ServeContent`, `Cache-Control: private, max-age=1y, immutable`), thumbnails at `/thumbs/<id>.webp?v=<recipe>`
  (immutable when the version matches), the embedded web page at `/`. Graceful shutdown on SIGINT/SIGTERM; no write
  timeout (videos stream as long as they play); thumbnails for pending files are made in the background at start.
- **Login** (`internal/auth`, migration `0002_auth`): one account today, tables keyed by user. First visit shows
  "create your account" (refused once an account exists - set it up before exposing Vitrine); `vitrine passwd <name>`
  sets/resets a password and ends all sessions. Passwords: argon2id (64 MiB, 3 passes, 4 lanes, PHC string).
  Sessions: random 32-byte token in an HttpOnly, SameSite=Lax cookie (Secure when the request came over HTTPS,
  directly or per `X-Forwarded-Proto`); only its SHA-256 is stored; expires after 180 days unused. Failed logins are
  rate-limited globally (10 per minute) - no trust in proxy headers needed with one account.
- **Cross-site protection**: changing requests must send JSON (plain forms can't, cross-site) and, if the browser sends
  an `Origin`, it must match the host. Plus `X-Content-Type-Options`, `X-Frame-Options: DENY`, `Referrer-Policy`, and a
  strict CSP on the web page (no inline scripts or styles; since 2026-10-23 also `object-src 'none'` and
  `form-action 'self'`), and `Strict-Transport-Security` (a year) when the request came over HTTPS (directly or a proxy's
  `X-Forwarded-Proto`) - the user approved it knowing a later plain-HTTP visit to the same address would be refused.
  `TestSecurityHeaders`.
- **Licenses (2026-10-23)**: `GET /licenses` (open, also in anonymous mode - outside `unlessAnonymous`): plain text -
  Vitrine's version, copyright ("The Vitrine authors"), the AGPL notice and `SourceURL` (section 13: users over a
  network can get the source), then the web app's bundled packages (Vite `build.license` -> `web/app/licenses.md`:
  svelte, clsx) and the Go side (`internal/server/licenses/go.txt`, generated by `web/scripts/go-notices.mjs` =
  `npm run notices`: Go's own license and every module `go list -deps ./cmd/vitrine` names, with all their
  LICENSE*/NOTICE files, plus Feather's MIT notice - some icons are adapted from it). `TestLicenseNoticesCurrent`
  fails when go.mod's direct requirements (path + version) aren't in it. Settings > Account > About shows the version,
  the license, Source code and Third-party licenses. All dependencies are permissive (MIT, BSD-3, Apache-2.0):
  compatible with AGPL-3.0.
- **Final review (2026-10-23, before v1)**: govulncheck (0 vulnerabilities in reached code once built with Go 1.27.3 -
  `toolchain go1.27.3` in go.mod, fetched by the go command itself; 11 were in 1.27.0's net/http, crypto/tls,
  crypto/x509), staticcheck, deadcode, knip, jscpd, `npm audit` (no runtime dependencies at all). Found sound and left
  as is: argon2id passwords, hashed session tokens, the dummy hash against name probing, the global login limit, the
  JSON + Origin check, owner-only secrets and local socket, the favicon fetcher's public-addresses-only dialer, plugins
  as data only. The web app's built files are gzipped once (best compression) and kept (`assetCache`: the 680 KB script
  was compressed again on every first load); a server error's cause goes in the request's log line (`statusWriter.err`
  - a bare "500" said nothing); a request the page called off is no "session check" error.
- **API** (all JSON; `{error}` on failure): `GET /api/health` (open), `GET /api/session`, `POST /api/setup|login|logout`,
  `GET /api/search?q=&limit=` (keeps the whole result server-side and returns `result` ID + first slice + `took_ms`),
  `GET /api/results/{id}?offset=&limit=` (slices; 404 when evicted from the 32-entry LRU -> search again),
  `GET /api/files?ids=` (up to 1000: type, size, dims, duration, audio, times, placeholder, url, thumb),
  `GET /api/files/{id}/tags` (with origins), `POST /api/tags {ids, add, remove}`, `POST /api/delete {ids, reason}`,
  `POST /api/undelete {ids}`, `GET /api/autocomplete?q=&limit=`, `GET|POST /api/relations` (`{kind: parent|sibling,
  action: add|remove, from, to}`; loops -> 400), `GET /api/stats`.
- **Test page** (`internal/server/web`, vanilla JS, ~300 lines, Catppuccin Mocha): login/setup, search with
  autocomplete on the word being typed, an endless square-tile grid loading 200 results at a time with the 4x4
  placeholder behind each thumbnail, a viewer (image/video, tags clickable). A stand-in until the Svelte client.
- Checked in Chrome against a 34-file library: first page in ~35 ms in all; no console errors.

## Local socket and the command line (2026-10-23)
The user (to-do 100): "so you can fully interact with it and debug it through the command line - not only for you,
but for users, so make sure it's all documented" - the guide is [docs/cli.md](../cli.md).
- `vitrine serve` also serves the API on a Unix socket, config `socket` (`VITRINE_SOCKET`; default
  `<data_dir>/vitrine.sock`, `off` turns it off; Go's AF_UNIX works on Windows 10+ too). `Server.Local()` marks its
  requests; `requireLogin` lets them act as the account (`auth.Owner`: the first user; none yet -> 401 "no account
  yet"). Trust = file access: owner-only (0600) on Linux/macOS, the data folder's permissions, same machine only (in
  Docker: inside the container). A socket a running server answers on isn't taken over (two servers on one library);
  a stale one (a crash) is replaced; it goes when the server stops.
- The CLI (`cmd/vitrine/live.go`, `cmd_live.go`, `live_lib.go`) is a plain HTTP client over the socket: the same API
  and JSON as the web app (the server's own Go types decode it). Live-only: `status`, `queue`, `subs`, `failed`,
  `notices`, `logs`, `api`. `search`, `tag`, `blacklist`, `setting`, `sites`, `credentials` go through the
  server when it runs (before, they wrote meta.db behind its back: its index and settings were stale until a restart)
  and open the library themselves when it doesn't. Flags may follow the words (`parseMixed`: only the command's own
  flags; a search's `-cloud` stays a word). Sites are plugin IDs passed to the server - none is built into the CLI.
- `GET /api/logs?n=&after=&level=` - the log ring's records as JSON (not anonymized, unlike the problem report);
  `after` lets `vitrine logs -f` follow along. With `level`, the newest n at that level out of the whole log
  (`logring.RecordsAt`; warn and error from the warnings' log, which reaches back weeks) - before 2026-10-28 they were
  picked out of the newest 5,000 records only, minutes on a busy downloader.
- Tested by `TestLiveCommands` (a real `serve` in the test, the commands through its socket, then offline again).

## Windows installer and notification-area program (2026-10-25)
The user: "the ready to run version should be an installer where the user can pick the data folders, adds vitrine
to path, has a launch at startup option, that adds a shortcut to start menu, packages all the libraries, has a
system tray icon etc." - for this user only ("Just me"), phones allowed by default, Update from the app.
- **Two programs from one source**: `bin\vitrine.exe` (the command line) and `Vitrine.exe`,
  built with `-ldflags "-X main.gui=1 -H windowsgui"` - no console; `main` then runs `runTray`
  (`cmd/vitrine/tray_windows.go`, fyne.io/systray - pure Go on Windows, no cgo). Two names because Windows file
  names ignore case. Both carry the icon and version (go-winres in the release workflow; `cmd/vitrine/vitrine.ico`,
  made from web/public/icon.svg).
- **Vitrine.exe** loads the config like `vitrine serve`, holds the mutex `Local\Vitrine` (a second start opens the
  browser and leaves) and runs `cmdServe` in-process (logs only to the log file). A click on the icon opens the
  browser; the menu: Open Vitrine, Copy the address for phones (listening on every network: this computer's private
  IPv4 - the default route's, else the first interface's), Open the files folder, Open the log, Start with Windows
  (HKCU Run, `"...Vitrine.exe" --background`, as the installer writes it), Restart, Quit. It opens the browser once
  the port answers, unless started with --background (sign-in, after an update). Restart - the menu's or the web
  app's (after a restore) - quits and starts the program again (fresh state, like a container restart). `--quit`
  sets the event `Local\VitrineQuit` and waits for the mutex to go (up to 30 s): the running one stops cleanly.
  Failing to start (a port in use) says so in a message box. Menu texts: `tray.*` in the locale files
  (i18n-check reads cmd/ too).
- **Child processes**: a program without a console would flash a console window for every ffmpeg/ffprobe/vips
  run - `internal/proc.Hide` sets CREATE_NO_WINDOW on Windows (every exec of a tool goes through it).
- **Bundled tools**: an empty ffmpeg/ffprobe/vipsthumbnail setting takes `tools\ffmpeg\ffmpeg.exe`,
  `tools\vips\vipsthumbnail.exe` beside the program or beside its folder (`config.Bundled`), before PATH. Pinned
  in `packaging/windows/build.sh` by URL and SHA-256: BtbN's LGPL shared build of FFmpeg 8.1.3 (a month-end
  autobuild - those are kept) and libvips 8.18.7's "web" static build (vipsthumbnail.exe + libvips-42.dll); their
  licenses, source links (THIRD-PARTY.txt) and libvips' versions.json go in the `licenses` folder. LGPL only - Vitrine
  needs ffmpeg's WebP encoding and decoding, no GPL parts.
- **The installer** (`packaging/windows/vitrine.iss`, Inno Setup 6; `build.sh` lays out the files and runs ISCC;
  the release workflow's `installer` job on windows-latest): per user (`PrivilegesRequired=lowest`) into
  %LOCALAPPDATA%\Programs\Vitrine. Two of them since 1.0.6 (the user: "i want a native arm64 installer and remove the
  windows zip releases"): `-x64` (also allowed on ARM64 Windows, emulated) and `-arm64` (ARM64 only; the tray and
  command line built for arm64, BtbN's winarm64 ffmpeg and libvips' arm64 build, pinned beside the x64 ones) -
  `vitrine.iss`'s `Arch`, `build.sh`'s fifth argument. The release has no Windows zips any more (the installers hold
  the same programs). Pages: the folder, "Your library" (library,
  original files, backups - the latter two follow the first while inside it; default %LOCALAPPDATA%\Vitrine), "Network"
  (port; phones on the Wi-Fi: `listen = ':PORT'`, else `'127.0.0.1:PORT'`), options (start at sign-in, PATH - HKCU
  Environment, `bin`; desktop shortcut), ready (lists the choices). It writes %APPDATA%\vitrine\config.toml - where
  both programs look by default - only when there's none, and remembers the folders in HKCU\Software\Vitrine (for the
  uninstaller). Phones on: a firewall rule for Vitrine.exe on private networks (netsh, asks for administrator
  permission once; declined: Windows asks when Vitrine first listens). An update (installed already, or a config
  exists) skips the library and network pages and doesn't touch Start with Windows (the menu's). Unattended:
  `/DATADIR= /FILESDIR= /BACKUPDIR= /PORT= /PHONES=0` with Inno's `/VERYSILENT /DIR= /TASKS=` (from PowerShell or
  cmd - Git Bash turns /SWITCHES into paths). A running Vitrine is stopped with --quit first (CloseApplications off).
  Uninstalling stops it, removes the PATH entry, the sign-in entry, the firewall rule, and asks (default No) whether
  to delete the library folder - folders outside it (the originals, backups) are asked about one by one. Not
  code-signed: SmartScreen warns (unknown publisher).
- **The two programs in folders of their own in the release workflow (2026-10-26, the user: "it doesnt open")**:
  1.0.3's `windows` artifact held `Vitrine.exe` and `vitrine.exe` side by side - two files on Linux, one on the
  Windows runner (names there ignore case), so the installer shipped the command line as Vitrine.exe (console
  subsystem; run without a command it printed its usage and shut). Now `windows/tray/` and `windows/cli/`, and
  `build.sh` refuses the same file twice. (The local test install had been built from two separate paths, so it
  never showed.) Released as **v1.0.3-hotfix** (the user: "give it a release tag that indiciates that its a hotfix"):
  `deploy.local.sh release hotfix` tags `v<last>-hotfix`; the installer's file version is numbers only
  (`NumVersion`, 1.0.3.1). Docker's `:1` and `:1.0` don't move to a hotfix (metadata-action treats the suffix as a
  pre-release) - fine while hotfixes fix the Windows programs only.
- Tested on Windows 11 (2026-10-25): silent install into scratch folders, the server up, the command line finding
  the bundled tools, --quit, the in-app update, a clean uninstall; the wizard's pages checked by screenshot.
  Note: the local socket's path must stay under ~108 characters (Windows' AF_UNIX limit) - a very deep data folder
  turns the socket off.

## Docker image as built (2026-10-14)
- `Dockerfile`: build stage `golang:1.27-trixie` (pure Go, `CGO_ENABLED=0`, version via `--build-arg VERSION`), runtime
  `debian:trixie-slim` + `ffmpeg` + `libvips-tools` (~630 MB; a slimmer image - e.g. static ffmpeg, vips without
  the full toolset - is a later optimization). Runs as uid/gid 1000, `VITRINE_DATA_DIR=/data` (volume), listens on
  `:8080`, `HEALTHCHECK` = `vitrine healthcheck` (GET /api/health on the local port), clean stop on SIGTERM.
- `.dockerignore` keeps `.git`, personal notes, docs, the spike and local data out of the build.
- `compose.example.yaml`: data in `./data`, originals in `./files` (`VITRINE_FILES_DIR=/files`), `user: 1000:1000`, an
  optional VPN-container note. The multi-arch image on `ghcr.io/usevitrine/vitrine` comes with the GitHub setup.

## Signed file links (2026-10-14)
- File and thumbnail URLs from the API carry `?k=` = HMAC-SHA256(server media key, "file:" + SHA-256), first 18
  bytes, base64url. `/files/...` and `/thumbs/...` accept a valid signature **or** a session cookie. Reason: a video
  element's media loader may not send cookies (seen in Playwright's WebKit: `<video>` requests arrived without the
  session cookie -> 401 -> "media error 4", while `fetch` and `<img>` sent it). iOS Safari plays video through its
  own media stack, and iOS is first-class, so media must not depend on cookies (hydrus-web, which plays on
  iPhones, also puts its key in the URL).
- The media key is 32 random bytes in `meta` (`media_key`), made on first start. A link never expires (URLs are
  cached for good), opens only its own file, and only the logged-in API hands links out. Changing the key (delete the
  row, restart) invalidates every link.
- Debug logging: `VITRINE_LOG_LEVEL=debug` logs every request (method, path, range, status, time).

### Screen-sized copies made ahead (2026-10-16)
The viewer asks for `?size=<screen>&for=screen`; the server remembers those sizes (renditions dir,
`screen-sizes.json`, forgotten after 30 days unused) and every 10 minutes makes the missing copies at them for
still images bigger than that, newest first, one at a time (`RunRenditionsAhead`). It stops at 80% of
`renditions_max_gb`, so copies made ahead never push out viewed ones. Zoom requests (no `for=screen`) don't count.
About 0.6 MB per copy (WebP).

## Updates: the check and the Update button (2026-10-24)
The user: "will my personal vitrine get a notification that there's an update" -> "A notification and an update
button that uses watchtower would be cool" -> "a silky smooth experience ... a full screen client side updater that
waits before the server comes back up and fades the site back into view". `internal/server/updates.go`.
- **The check**: once a day (3 min after the start, then hourly checks for a day-old answer) the server asks GitHub's
  API for the latest release (`releases_url`, default `api.github.com/repos/usevitrine/vitrine/releases/latest`;
  **refused (403/429), GitHub's website instead** (2026-10-26, the user's main instance: "GitHub answered 403
  Forbidden" - the API allows 60 requests an hour per address, and the VPN's shared exit had used them up): the tag
  from where `github.com/usevitrine/vitrine/releases/latest` redirects, the files and their sums from the release's
  `SHA256SUMS` (`fetchLatestPage`, `ReleasePagesURL`);
  "off" never asks - the end-to-end test servers) - only with the shared setting `updateCheck` (missing = on;
  Settings > Updates). A newer `vX.Y.Z` than this program (or `vX.Y.Z-hotfix`: a fix of X.Y.Z, newer than it and older
  than X.Y.Z+1 - the user chose the name, 2026-10-26; a pre-release isn't one): `GET /api/update` says so (`newer`, `latest`,
  `page`, and `changelog`: that release's CHANGELOG.md, read from `changelog_url` - default the repository's on GitHub
  at the tag, raw.githubusercontent.com; a failure there still offers the update), and the bell gets a notice, once per version (kind `release`, the notice kind "A new Vitrine release",
  leading to Settings > Updates). Development builds (`dev`, the dev server's `test-...`) are never behind.
  `POST /api/update/check` is "Check now".
- **The Update button** (only with `watchtower_url` + `watchtower_token`: Watchtower's HTTP API, the
  `nickfedor/watchtower` fork - containrrr's is archived - with `WATCHTOWER_HTTP_API_ENDPOINTS=update` and that
  token; with only the update endpoint it never polls on its own): `POST /api/update` answers at once and runs in
  the background - a backup of meta.db (failed: nothing is updated), then `POST /v1/update?image=ghcr.io/usevitrine/vitrine`
  (synchronous, bounded at 15 min). On success Watchtower recreates this container before it answers (the program
  is stopped); any answer that comes back is a failure, kept as `update_error` with the reason (a refused token, a
  failed pull, no newer image, another container updated). `GET /api/update` gives the step (`updating`:
  backup, download). Watchtower reports nothing about the pull's progress (its events: started/completed/failed only).
- **The full-screen updater** (`lib/updater.svelte.ts`, `components/Updater.svelte`, mounted by App): the splash's
  background and logo, the steps under it, the two together in the middle of the screen (the user) - backing up, downloading, restarting (requests
  fail: the container is being recreated), starting the new version (back on another version) - each ticked when it
  really happens; the bar eases toward the current step's end without reaching it, never goes back. The end: the
  server's page is put in the service worker's copy (`lib/appShell.ts`, shared with main.ts's version check - no old
  version on the way), the bar fills, the steps fade while the group moves down by half the steps' height and the gap
  - the logo lands exactly where the splash draws it (measured: within 0.1 px) - and the library opens (`/`: its
  default search and sort, the user: "send the user to their main library page with their defaults"); `sessionStorage vitrine.updatedTo` makes
  splash.js show the splash for that load (also on Android, where it's normally off), so the splash takes over at
  the same place and fades the app in, then "Updated to vX" (App, `updater.announce`). A failure: the reason, "still
  on the version it was", Close; the overlay scrolls on a short screen (the group centered with margin: auto). Tests: `TestNewerVersion`, `TestUpdateCheck`, `TestUpdateStart` (Go), "updates: ..."
  (e2e, the API mocked).
- **Installed with the Windows installer** (2026-10-25, the user: "Yes" to updating from the app): Vitrine.exe sets
  `UpdateConfig.SetupName` (`Vitrine-Setup-%s-x64.exe`, or `-arm64.exe` on ARM64 Windows - since 1.0.6 an x64 Vitrine
  running there emulated moves to the native installer at its next update: `setupArch`, `IsWow64Process2`) and `RunSetup`, which makes `can_update` true. Update then
  backs up, downloads that release asset (its URL and SHA-256 come with the latest-release answer: GitHub's `digest`;
  no digest or a mismatch: not run) to a temp folder and hands it to RunSetup: it starts the installer with
  `/VERYSILENT /SUPPRESSMSGBOXES /NORESTART` and quits Vitrine a second later; the installer waits for it (--quit),
  updates in place keeping config and library, and starts `Vitrine.exe --background` - the page sees the server go
  and come back on the new version (the same steps as Watchtower's). Tested end to end on Windows 2026-10-25 with a
  local stand-in for GitHub (1.0.3 to 1.0.4: ~5 s). Test: `TestUpdateWithInstaller`.
- compose.example.yaml has the Watchtower service (optional) and the two Vitrine variables; config.example.toml
  documents `watchtower_url`, `watchtower_token`, `releases_url`.
- **Settings > Updates** (2026-10-24, the user: "The update section should be on its own update page with the change
  log and if possible it gets the changes for the new version"): in place of What's new - the version installed and
  UpdateStatus (up to date / the newer one, Release notes, Update, Check now, the daily check), then the newer
  release's versions after this one (`lib/changelog.ts` reads both changelogs; `newerThan`), each card marked New,
  then the installed changelog (ChangelogCard). About keeps the version and a link here.
- **What's new after an update** (2026-10-26, the user: "when a user updates and it goes to their media, i want a
  'what's new' popup to show by default that shows the changes for the new version, with a 'don't show again'
  checkbox and the same option in the settings" - "only once per instance (not once per device) and on mobile it
  should be a sheet"): `lib/whatsNew.svelte.ts` + `WhatsNew.svelte`. The installed version is the newest released one
  in the built-in CHANGELOG.md; shared settings `whatsNew` (missing = on) and `whatsNewSeen` (the last version
  shown - the server's, so once per server). On the library (Comics with Media off), nothing else open, once the
  settings are read: a newer installed version opens the window with the versions since `whatsNewSeen` (up to five,
  each under its heading; never shown before: this version's only) - ChangelogCard `bare`. A card in the middle,
  a sheet on phones (drag down); Got it, Escape, Back or a press outside mark it seen; "Don't show again" sets
  `whatsNew` false, switched back in Settings > Updates ("Show what's new after an update"). Off: marked seen quietly.
  A new server's first steps mark the version seen (no window after setting up); the demo never shows it. The e2e
  login switches it off on its device (the test libraries never saw it). Test: "what's new after an update" (e2e).
