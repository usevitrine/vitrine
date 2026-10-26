# Vitrine docs

Split by topic: open only the files a task needs. Each file starts with what it covers.

## Project
- `project/overview.md` - what Vitrine is, the key decisions, the build order, where it stands.
- `project/rules.md` - public-project rules, spelling and texts, working rules, how the docs are kept.
- `project/features.md` - the planned feature list.
- `project/website.md` - the website (vitrine.software): its plan, the in-browser demo.
- `project/background.md` - prior art (Hydrus, Stash, monbooru, ...), libraries, name history, license.

## Architecture
- `architecture/stack-and-storage.md` - tech stack, speed targets and how they're met, storage design.
- `architecture/server.md` - server implementation decisions, HTTP server and API conventions, signed links,
  screen-sized copies, Docker image, the Windows installer and notification-area program.
- `architecture/search-index.md` - the in-memory search index as built.
- `architecture/media.md` - probing, thumbnails, previews (ffmpeg, libvips).
- `architecture/testing.md` - Go tests and the Playwright end-to-end tests.

## Command line
- `cli.md` - the `vitrine` command: every command, the local socket a running server listens on.

## Library
- `library/model.md` - files, tags, origins, namespaces, parents/siblings, delete/undo, upload times.
- `library/search.md` - search requirements and the search syntax as built.
- `library/duplicates.md` - the duplicate finder and the Duplicates screen.
- `library/favorites-and-rankings.md` - favorites, NEW pills, the "I came" button and Rankings.
- `comics.md` - comics: the library side, the Comics page and reader, collections.

## Downloader
- `downloader/downloads-and-subscriptions.md` - searches, the queue, blacklist, subscriptions and groups,
  retries, parallel downloads, download rules, favorites, check depth.
- `downloader/plugins.md` - the plugin system, manifests, HTTP client, credentials, tag namespaces, install/update.
- `downloader/engines.md` - gelbooru-v02 (+ html), Shimmie2, Mastodon, Danbooru, ehentai.
- `downloader/tag-refresh.md` - periodic tag refresh.
- `downloader/reliability.md` - Cloudflare/FlareSolverr, stalls, a site that stops answering pauses by itself.
- Per-site notes: the plugins repo's `docs/sites.md`.

## Web client
- `ui/design-language.md` - **read before any UI work**: colors, tokens, sizes, layout, gestures, building blocks
  (controls, cards, menus, sheets), motion, hover, hints, icons, the logo.
- `ui/app-shell.md` - setup and build, the shell (top bar, menu, back), first run, service worker, live updates,
  start-up, iOS home-screen app.
- `ui/grid-and-search.md` - search bar and chips, saved searches, sort and filters, the grid, fast scroller, new
  files, hover previews, the selection bar.
- `ui/viewer-and-video.md` - the viewer, details panel, video player and scrubbing.
- `ui/tags.md` - tag menu, tag browser, Tags screens, tag editing.
- `ui/downloads-page.md` - the Downloads page.
- `ui/settings.md` - Settings sections and cards, stats, problem report.
- `ui/notifications.md` - push notifications, toasts, the bell.
- `ui/languages.md` - UI texts and translations.

`brand/` holds the logo sources.
