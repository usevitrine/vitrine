# Project overview

What Vitrine is, the key decisions behind it (details in the topic docs), the agreed build order and where it stands.

## What it is
A fast, self-hosted **media library with a built-in downloader and its own web client** - a lean alternative to
[Hydrus](https://github.com/hydrusnetwork/hydrus) for people who use their library through a browser. One Go
program, no desktop GUI: it stores files, keeps tags (with parents/siblings), searches, makes thumbnails, downloads
from booru sites via plugins, and serves the web app (phone first, desktop with power-user tools). Far fewer features
than Hydrus on purpose, but Hydrus-level **advanced search**, **lightning fast**, and **5-star UI/UX**.

Name: **Vitrine** ("vi-TREEN", a display cabinet for a collection; history in `docs/project/background.md`).
Accounts under **`usevitrine`** (a GitHub user account; `ghcr.io/usevitrine/vitrine`, Go module
`github.com/usevitrine/vitrine`, website and email `vitrine.software`). Repos `usevitrine/vitrine` and
`usevitrine/vitrine-plugins`, **private for now**; the plugin index URL works once vitrine-plugins is public.

## Key decisions (details in the topic docs)
- **One project, one repo**; headless **Go** server with the built web client embedded (one binary).
- **Single user** (one login), but nothing that makes more users impossible later.
- **Docker first-class and native equally supported** (Linux/Windows/macOS, amd64+arm64); pure Go (no cgo);
  external tools **ffmpeg** (video) and **libvips CLI** (images); SQLite driver **modernc.org/sqlite**.
  (`docs/architecture/stack-and-storage.md`)
- **Storage**: originals content-addressed on bulk storage; SQLite metadata + thumbnail DB + previews on fast
  storage; search index in RAM (roaring bitmaps, column arrays); dense sequential uint32 file IDs.
- **Tags**: one tag set, each file-tag link marked by origin (a site or the user); site tags stored verbatim, with
  **namespaces from the site's tag types** (creator/series/character/meta, each its own color); **parents and
  siblings** core; **periodic tag refresh** of existing posts (a top priority). (`docs/library/model.md`,
  `docs/downloader/tag-refresh.md`)
- **Search**: booru-style typing and chips, **OR groups** (like hydrus-web), **every chip editable in place**.
  (`docs/library/search.md`)
- **Delete** with a short undo window (default 7 days) + tombstones (never re-download); same file from several sites
  = one file. No user ratings (but **favorites**, a heart), no inbox/archive, no PTR, **no Hydrus import** (starts
  fresh).
- **iOS is a first-class citizen** (Safari / the home-screen web app get the same care as desktop;
  `docs/ui/app-shell.md`).
- **Web client: Svelte 5** SPA (Vite). **Catppuccin Mocha only** (no light theme), accent **Mauve #cba6f7**;
  mobile-first but equally polished on desktop; compact controls with a mouse, touch-sized only on touch screens.
  (`docs/ui/design-language.md`)
- **Own video player** (never the iOS one), gestures, preview frames; **no page walls** when browsing (server-side
  result lists, slices by position, grid and viewer always in sync). (`docs/ui/viewer-and-video.md`,
  `docs/ui/grid-and-search.md`)
- **Site plugins**: declarative manifest per booru engine + sandboxed **JavaScript (goja)** for odd sites; plugins
  live in a **separate repo** (`usevitrine/vitrine-plugins`, also the default plugin index). (`docs/downloader/`)
- **Comics** have a page of their own (galleries, a reader, collections), never mixed into the library's grid.
  (`docs/comics.md`)
- **License**: AGPL-3.0-or-later (core), MIT (plugin SDK, example plugins, manifests).

## Build order (agreed)
0. **Speed experiment**: the in-memory search index alone on synthetic data (100k files, 85k tags, ~5M tag links;
   and a 1M-file run) - search < 20 ms and autocomplete < 10 ms before building more. Code in `spike/indexbench/`
   (throwaway). **Done 2026-10-14, targets met with a wide margin** (100k files: searches < 1 ms; 1M files: < 4 ms,
   except sorting a 292k-file result by size: 15-18 ms) - results in `docs/architecture/stack-and-storage.md`.
1. Server core + the first site downloader (store, DB, index, parents/siblings, thumbnails, gelbooru-v02 engine) -
   proven with a minimal test page. **Done 2026-10-14** (live download test passed).
2. Web client (search, viewer, tag/parent editing, downloader UI). **Done and growing** (`docs/ui/`).
3. A site without an API (the Go engine `gelbooru-v02-html`, 2026-10-15), plugin install/update in Settings
   (2026-10-15), notifications (Web Push, 2026-10-15). **Done.**
4. Good enough to replace a Hydrus setup (a library built by Vitrine's own subscriptions/searches).

## Where it stands (2026-10-21)
Library, search, thumbnails, duplicates, favorites, the downloader with five engines and subscriptions, the web
client (library, viewer, Downloads, Tags, Settings, Duplicates, Comics with collections), Web Push, languages, the
Docker image, backups. Measurements and choices are in each topic doc's "as built" sections. Open work is tracked
outside the repo.
